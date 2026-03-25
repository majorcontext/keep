package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/gateway/anthropic"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
	"github.com/majorcontext/keep/internal/sse"
)

// maxRequestBodySize is the maximum size of a request body we will read (4 MB).
const maxRequestBodySize = 4 * 1024 * 1024

// maxResponseBodySize is the maximum size of an upstream response body we will read (16 MB).
const maxResponseBodySize = 16 << 20 // 16 MB

// maxSSEEvents is the maximum number of SSE events buffered during streaming.
// A typical Anthropic response generates ~100-500 events. This cap prevents
// memory exhaustion from malformed or malicious streams.
const maxSSEEvents = 10000

// Proxy is the LLM gateway HTTP handler.
type Proxy struct {
	engine    *keep.Engine
	scope     string
	upstream  *url.URL
	decompose gwconfig.DecomposeConfig
	logger    *audit.Logger
	debug     *slog.Logger
	verbose   *VerboseWriter
	passthru  *httputil.ReverseProxy // for non-messages passthrough
	client    *http.Client           // for /v1/messages upstream requests
}

// NewProxy creates an LLM gateway proxy.
func NewProxy(engine *keep.Engine, cfg *gwconfig.GatewayConfig, logger *audit.Logger, opts ...ProxyOption) (*Proxy, error) {
	upstream, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, fmt.Errorf("gateway: invalid upstream URL: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(upstream)

	p := &Proxy{
		engine:    engine,
		scope:     cfg.Scope,
		upstream:  upstream,
		decompose: cfg.Decompose,
		logger:    logger,
		passthru:  rp,
		// 110s client timeout is less than the server's 120s WriteTimeout,
		// ensuring the upstream error response has time to be written back
		// before the server closes the connection.
		client: &http.Client{Timeout: 110 * time.Second},
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// ProxyOption configures optional Proxy behavior.
type ProxyOption func(*Proxy)

// WithDebugLogger enables debug logging to the given slog.Logger.
func WithDebugLogger(l *slog.Logger) ProxyOption {
	return func(p *Proxy) { p.debug = l }
}

// WithVerboseWriter enables verbose packet logging.
func WithVerboseWriter(v *VerboseWriter) ProxyOption {
	return func(p *Proxy) { p.verbose = v }
}

// logDebug emits a debug log if debug logging is enabled.
func (p *Proxy) logDebug(msg string, args ...any) {
	if p.debug != nil {
		p.debug.Debug(msg, args...)
	}
}

// ServeHTTP intercepts /v1/messages requests for policy evaluation.
// All other paths are reverse-proxied without evaluation.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/messages" {
		p.logDebug("passthrough", "method", r.Method, "path", r.URL.Path)
		p.passthru.ServeHTTP(w, r)
		return
	}
	p.handleMessages(w, r)
}

// policyError is the JSON error body returned when policy denies a request or response.
type policyError struct {
	Type  string      `json:"type"`
	Error policyInner `json:"error"`
}

type policyInner struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func writePolicyDeny(w http.ResponseWriter, rule, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(policyError{
		Type: "error",
		Error: policyInner{
			Type:    "policy_denied",
			Message: fmt.Sprintf("Policy denied: %s. %s", rule, message),
		},
	})
}

func writeInternalError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(policyError{
		Type: "error",
		Error: policyInner{
			Type:    "internal_error",
			Message: msg,
		},
	})
}

// handleMessages processes /v1/messages requests with bidirectional policy evaluation.
func (p *Proxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	// 1. Read request body (with size limit to prevent memory exhaustion).
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodySize))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(policyError{
				Type: "error",
				Error: policyInner{
					Type:    "request_too_large",
					Message: fmt.Sprintf("Request body exceeds maximum allowed size of %d bytes", maxRequestBodySize),
				},
			})
			return
		}
		writeInternalError(w, "failed to read request body")
		return
	}

	// Verbose: log raw request.
	if p.verbose != nil {
		p.verbose.RequestRaw(body)
	}

	// 2. Parse as MessagesRequest.
	var req anthropic.MessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeInternalError(w, "failed to parse request body")
		return
	}

	mode := "non-streaming"
	if req.Stream {
		mode = "streaming"
	}
	p.logDebug("request",
		"model", req.Model,
		"mode", mode,
		"messages", len(req.Messages),
	)

	// 3. Decompose request into calls.
	calls := anthropic.DecomposeRequest(&req, p.scope, p.decompose)

	// 4. Evaluate each call. Track block results for reassembly.
	// DecomposeRequest emits: optional summary call, then one call per content block.
	// We need to map the content-block calls back to (MessageIndex, BlockIndex).
	summaryOffset := 0
	if p.decompose.RequestSummaryEnabled() && len(calls) > 0 {
		summaryOffset = 1
	}

	// Build the block index mapping by re-walking messages (same order as DecomposeRequest).
	blockMap := buildRequestBlockMap(&req, p.decompose)

	hasRedaction := false
	blockResults := make([]anthropic.BlockResult, len(blockMap))

	var redactedRules []string
	for i, call := range calls {
		result, evalErr := p.engine.Evaluate(call, p.scope)
		if evalErr != nil {
			writeInternalError(w, "policy evaluation error")
			return
		}

		// Log audit entry.
		if p.logger != nil {
			p.logger.Log(result.Audit)
		}

		// Check for deny.
		if result.Decision == keep.Deny {
			p.logDebug("request denied", "rule", result.Rule, "message", result.Message)
			if p.verbose != nil {
				p.verbose.RequestDenied(result.Rule, result.Message)
			}
			writePolicyDeny(w, result.Rule, result.Message)
			return
		}

		// Track redactions for content blocks (skip the summary call).
		if i >= summaryOffset {
			blockIdx := i - summaryOffset
			if blockIdx < len(blockResults) {
				blockResults[blockIdx].MessageIndex = blockMap[blockIdx].MessageIndex
				blockResults[blockIdx].BlockIndex = blockMap[blockIdx].BlockIndex
				blockResults[blockIdx].Result = result
				if result.Decision == keep.Redact {
					hasRedaction = true
					redactedRules = append(redactedRules, result.Rule)
				}
			}
		}
	}

	p.logDebug("request policy",
		"calls", len(calls),
		"redacted", hasRedaction,
		"redacted_rules", strings.Join(redactedRules, ","),
	)

	// 6. Reassemble if redacted.
	forwardBody := body
	if hasRedaction {
		patched := anthropic.ReassembleRequest(&req, blockResults)
		forwardBody, err = json.Marshal(patched)
		if err != nil {
			writeInternalError(w, "failed to marshal patched request")
			return
		}
	}

	// Verbose: log post-policy request.
	if p.verbose != nil {
		if hasRedaction {
			p.verbose.RequestAfterPolicy(forwardBody, strings.Join(redactedRules, ", "))
		} else {
			p.verbose.RequestAllowed()
		}
	}

	// 7. Streaming: buffer, evaluate, replay/synthesize.
	if req.Stream {
		p.handleStreamingResponse(w, r, forwardBody)
		return
	}

	// 8. Forward to upstream.
	upstreamBase := strings.TrimRight(p.upstream.String(), "/")
	upstreamURL := upstreamBase + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(forwardBody))
	if err != nil {
		writeInternalError(w, "failed to create upstream request")
		return
	}

	copyRequestHeaders(upstreamReq, r)

	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		writeInternalError(w, "upstream request failed")
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	// 8. Read response body.
	respBody, err := io.ReadAll(io.LimitReader(upstreamResp.Body, maxResponseBodySize))
	if err != nil {
		writeInternalError(w, "failed to read upstream response")
		return
	}

	// If upstream returned a non-2xx status, pass through as-is.
	if upstreamResp.StatusCode < 200 || upstreamResp.StatusCode >= 300 {
		copyResponseHeaders(w, upstreamResp)
		w.WriteHeader(upstreamResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	// Verbose: log raw response.
	if p.verbose != nil {
		p.verbose.ResponseRaw(respBody)
	}

	// 9. Parse as MessagesResponse.
	var resp anthropic.MessagesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		// Can't parse response; pass through as-is.
		copyResponseHeaders(w, upstreamResp)
		w.WriteHeader(upstreamResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	// 10. Decompose response.
	respCalls := anthropic.DecomposeResponse(&resp, p.scope, p.decompose)

	// 11. Evaluate response calls.
	respSummaryOffset := 0
	if p.decompose.ResponseSummaryEnabled() && len(respCalls) > 0 {
		respSummaryOffset = 1
	}

	// Build response block index mapping using WalkResponseBlocks (same order as DecomposeResponse).
	respBlockMap := anthropic.WalkResponseBlocks(&resp, p.decompose)

	var respBlockResults []anthropic.BlockResult
	respHasRedaction := false

	for i, call := range respCalls {
		result, evalErr := p.engine.Evaluate(call, p.scope)
		if evalErr != nil {
			writeInternalError(w, "response policy evaluation error")
			return
		}

		if p.logger != nil {
			p.logger.Log(result.Audit)
		}

		// 12. Check for deny.
		if result.Decision == keep.Deny {
			p.logDebug("response denied", "rule", result.Rule, "message", result.Message)
			if p.verbose != nil {
				p.verbose.ResponseDenied(result.Rule, result.Message)
			}
			writePolicyDeny(w, result.Rule, result.Message)
			return
		}

		if i >= respSummaryOffset {
			blockIdx := i - respSummaryOffset
			if blockIdx < len(respBlockMap) {
				if result.Decision == keep.Redact {
					respHasRedaction = true
				}
				respBlockResults = append(respBlockResults, anthropic.BlockResult{
					BlockIndex: respBlockMap[blockIdx].BlockIndex,
					Result:     result,
				})
			}
		}
	}

	p.logDebug("response policy", "status", upstreamResp.StatusCode, "calls", len(respCalls), "redacted", respHasRedaction)

	// 13. Reassemble if redacted.
	finalBody := respBody
	if respHasRedaction {
		patched := anthropic.ReassembleResponse(&resp, respBlockResults)
		finalBody, err = json.Marshal(patched)
		if err != nil {
			writeInternalError(w, "failed to marshal patched response")
			return
		}
	}

	// Verbose: log post-policy response.
	if p.verbose != nil {
		if respHasRedaction {
			p.verbose.ResponseAfterPolicy(finalBody, "")
		} else {
			p.verbose.ResponseAllowed()
		}
	}

	// 14. Return response to agent.
	copyResponseHeaders(w, upstreamResp)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(finalBody)
}

// blockPosition maps a decomposed call index to its message and block position.
type blockPosition struct {
	MessageIndex int
	BlockIndex   int
}

// buildRequestBlockMap walks the request messages in the same order as DecomposeRequest
// and returns the (MessageIndex, BlockIndex) for each content-block call.
// Uses the shared WalkRequestBlocks iterator to ensure consistent traversal.
func buildRequestBlockMap(req *anthropic.MessagesRequest, cfg gwconfig.DecomposeConfig) []blockPosition {
	walked := anthropic.WalkRequestBlocks(req, cfg)
	positions := make([]blockPosition, len(walked))
	for i, pos := range walked {
		positions[i] = blockPosition{
			MessageIndex: pos.MessageIndex,
			BlockIndex:   pos.BlockIndex,
		}
	}
	return positions
}

// handleStreamingResponse handles the upstream call and response for streaming requests.
// It buffers the full SSE stream, reassembles into a MessagesResponse, evaluates policy,
// then replays original events (if clean) or synthesizes new events (if redacted).
func (p *Proxy) handleStreamingResponse(w http.ResponseWriter, r *http.Request, forwardBody []byte) {
	// 1. Forward to upstream.
	upstreamBase := strings.TrimRight(p.upstream.String(), "/")
	upstreamURL := upstreamBase + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(forwardBody))
	if err != nil {
		writeInternalError(w, "failed to create upstream request")
		return
	}

	copyRequestHeaders(upstreamReq, r)

	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		writeInternalError(w, "upstream request failed")
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	// 2. If upstream returned non-2xx, pass through as-is.
	if upstreamResp.StatusCode < 200 || upstreamResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(upstreamResp.Body, maxResponseBodySize))
		copyResponseHeaders(w, upstreamResp)
		w.WriteHeader(upstreamResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	// 3. Buffer all SSE events from upstream.
	// Stop on message_stop (the terminal Anthropic event) rather than waiting
	// for EOF — the upstream may keep the connection open after the last event.
	reader := sse.NewReader(io.LimitReader(upstreamResp.Body, maxResponseBodySize))
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.logDebug("stream read error", "err", err, "events_so_far", len(events))
			writeInternalError(w, "failed to read upstream SSE stream")
			return
		}
		events = append(events, ev)
		if len(events) > maxSSEEvents {
			p.logDebug("SSE event limit exceeded", "limit", maxSSEEvents)
			writeInternalError(w, "upstream response too large")
			return
		}
		if ev.Type == "message_stop" {
			break
		}
	}

	p.logDebug("upstream stream", "status", upstreamResp.StatusCode, "events", len(events))

	// 4. Reassemble into MessagesResponse.
	resp, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		writeInternalError(w, "failed to reassemble streaming response")
		return
	}

	// Verbose: log reassembled response.
	if p.verbose != nil {
		assembled, _ := json.Marshal(resp)
		p.verbose.ResponseRaw(assembled)
	}

	// 5. Decompose and evaluate response policy.
	respCalls := anthropic.DecomposeResponse(resp, p.scope, p.decompose)

	respSummaryOffset := 0
	if p.decompose.ResponseSummaryEnabled() && len(respCalls) > 0 {
		respSummaryOffset = 1
	}

	// Build response block index mapping using WalkResponseBlocks (same order as DecomposeResponse).
	respBlockMap := anthropic.WalkResponseBlocks(resp, p.decompose)

	var respBlockResults []anthropic.BlockResult
	respHasRedaction := false

	for i, call := range respCalls {
		result, evalErr := p.engine.Evaluate(call, p.scope)
		if evalErr != nil {
			writeInternalError(w, "response policy evaluation error")
			return
		}

		if p.logger != nil {
			p.logger.Log(result.Audit)
		}

		if result.Decision == keep.Deny {
			p.logDebug("response denied", "rule", result.Rule, "message", result.Message)
			if p.verbose != nil {
				p.verbose.ResponseDenied(result.Rule, result.Message)
			}
			writePolicyDeny(w, result.Rule, result.Message)
			return
		}

		if i >= respSummaryOffset {
			blockIdx := i - respSummaryOffset
			if blockIdx < len(respBlockMap) {
				if result.Decision == keep.Redact {
					respHasRedaction = true
				}
				respBlockResults = append(respBlockResults, anthropic.BlockResult{
					BlockIndex: respBlockMap[blockIdx].BlockIndex,
					Result:     result,
				})
			}
		}
	}

	p.logDebug("response policy", "calls", len(respCalls), "redacted", respHasRedaction)

	// 6. Determine which events to send.
	var outEvents []sse.Event
	if respHasRedaction {
		patched := anthropic.ReassembleResponse(resp, respBlockResults)
		outEvents = anthropic.SynthesizeEvents(patched)
	} else {
		outEvents = events
	}

	// Verbose: log post-policy response.
	if p.verbose != nil {
		if respHasRedaction {
			assembled, _ := json.Marshal(anthropic.ReassembleResponse(resp, respBlockResults))
			p.verbose.ResponseAfterPolicy(assembled, "")
		} else {
			p.verbose.ResponseAllowed()
		}
	}

	// 7. Stream events to client.
	sseWriter, err := sse.NewWriter(w)
	if err != nil {
		writeInternalError(w, "streaming not supported by response writer")
		return
	}
	// Copy rate-limit headers from upstream, then set SSE headers (overrides Content-Type).
	copyResponseHeaders(w, upstreamResp)
	sseWriter.SetHeaders()
	w.WriteHeader(http.StatusOK)

	for _, ev := range outEvents {
		if err := sseWriter.WriteEvent(ev); err != nil {
			return
		}
	}
}

// allowedRequestHeaders lists headers that are forwarded to the upstream.
// This is an allowlist rather than a denylist to prevent accidentally
// forwarding sensitive headers (cookies, internal auth) to the upstream.
var allowedRequestHeaders = map[string]bool{
	"Authorization":     true,
	"Content-Type":      true,
	"Accept":            true,
	"User-Agent":        true,
	"X-Request-Id":      true,
	"Anthropic-Version": true,
	"Anthropic-Beta":    true,
	"X-Api-Key":         true,
}

// copyRequestHeaders copies allowed headers from the incoming request to the upstream request.
func copyRequestHeaders(dst *http.Request, src *http.Request) {
	for key, values := range src.Header {
		if !allowedRequestHeaders[http.CanonicalHeaderKey(key)] {
			continue
		}
		for _, v := range values {
			dst.Header.Add(key, v)
		}
	}
}

// copyResponseHeaders copies relevant headers from an upstream response to the client.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{
		"Content-Type",
		"X-Request-Id",
		"retry-after",
		"anthropic-ratelimit-requests-limit",
		"anthropic-ratelimit-requests-remaining",
		"anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-tokens-limit",
		"anthropic-ratelimit-tokens-remaining",
		"anthropic-ratelimit-tokens-reset",
	} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}
