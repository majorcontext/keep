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
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
	"github.com/majorcontext/keep/llm"
	"github.com/majorcontext/keep/llm/anthropic"
	"github.com/majorcontext/keep/sse"
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
	engine   *keep.Engine
	scope    string
	upstream *url.URL
	codec    llm.Codec
	llmCfg   llm.DecomposeConfig
	logger   *audit.Logger
	debug    *slog.Logger
	verbose  *VerboseWriter
	passthru *httputil.ReverseProxy // for non-messages passthrough
	client   *http.Client           // for /v1/messages upstream requests
}

// NewProxy creates an LLM gateway proxy.
func NewProxy(engine *keep.Engine, cfg *gwconfig.GatewayConfig, logger *audit.Logger, opts ...ProxyOption) (*Proxy, error) {
	upstream, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, fmt.Errorf("gateway: invalid upstream URL: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(upstream)

	p := &Proxy{
		engine:   engine,
		scope:    cfg.Scope,
		upstream: upstream,
		codec:    anthropic.NewCodec(),
		llmCfg:   cfg.Decompose.ToLLM(),
		logger:   logger,
		passthru: rp,
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
	// 1. Read and parse request.
	body, req, err := p.readRequest(w, r)
	if err != nil {
		return // error already written to w
	}

	// 2. Evaluate request policy.
	evalResult, err := p.evaluateRequestPolicy(w, body)
	if err != nil {
		return // error (or deny) already written to w
	}

	// 3. Streaming: buffer, evaluate, replay/synthesize.
	if req.Stream {
		p.handleStreamingResponse(w, r, evalResult.forwardBody)
		return
	}

	// 4. Non-streaming: forward, evaluate response, return.
	p.handleNonStreamingResponse(w, r, evalResult.forwardBody)
}

// readRequest reads the request body (with size limits) and parses it as a MessagesRequest.
// On error, it writes the appropriate error response to w and returns a non-nil error.
func (p *Proxy) readRequest(w http.ResponseWriter, r *http.Request) ([]byte, *anthropic.MessagesRequest, error) {
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
			return nil, nil, err
		}
		writeInternalError(w, "failed to read request body")
		return nil, nil, err
	}

	// Verbose: log raw request.
	if p.verbose != nil {
		p.verbose.RequestRaw(body)
	}

	var req anthropic.MessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeInternalError(w, "failed to parse request body")
		return nil, nil, err
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

	return body, &req, nil
}

// requestPolicyResult holds the output of request policy evaluation.
type requestPolicyResult struct {
	forwardBody []byte
}

// evaluateRequestPolicy decomposes the request, evaluates each call against policy,
// and reassembles the request body if any redactions were applied.
// On deny or error, it writes the appropriate response to w and returns a non-nil error.
func (p *Proxy) evaluateRequestPolicy(w http.ResponseWriter, body []byte) (*requestPolicyResult, error) {
	result, err := llm.EvaluateRequest(p.engine, p.codec, body, p.scope, p.llmCfg)
	if err != nil {
		writeInternalError(w, "policy evaluation error")
		return nil, err
	}

	// Log all audit entries.
	if p.logger != nil {
		for _, a := range result.Audits {
			p.logger.Log(a)
		}
	}

	if result.Decision == keep.Deny {
		p.logDebug("request denied", "rule", result.Rule, "message", result.Message)
		if p.verbose != nil {
			p.verbose.RequestDenied(result.Rule, result.Message)
		}
		writePolicyDeny(w, result.Rule, result.Message)
		return nil, fmt.Errorf("policy denied: %s", result.Rule)
	}

	if p.verbose != nil {
		if result.Decision == keep.Redact {
			p.verbose.RequestAfterPolicy(result.Body, result.Rule)
		} else {
			p.verbose.RequestAllowed()
		}
	}

	return &requestPolicyResult{forwardBody: result.Body}, nil
}

// handleNonStreamingResponse forwards the request to upstream, reads the response,
// evaluates response policy, and writes the (possibly redacted) response back to the client.
func (p *Proxy) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, forwardBody []byte) {
	// Forward to upstream.
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

	// Read response body.
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

	// Evaluate response policy via pipeline.
	result, err := llm.EvaluateResponse(p.engine, p.codec, respBody, p.scope, p.llmCfg)
	if err != nil {
		writeInternalError(w, "response policy evaluation error")
		return
	}
	if p.logger != nil {
		for _, a := range result.Audits {
			p.logger.Log(a)
		}
	}
	if result.Decision == keep.Deny {
		p.logDebug("response denied", "rule", result.Rule, "message", result.Message)
		if p.verbose != nil {
			p.verbose.ResponseDenied(result.Rule, result.Message)
		}
		writePolicyDeny(w, result.Rule, result.Message)
		return
	}
	finalBody := result.Body

	// Verbose: log post-policy response.
	if p.verbose != nil {
		if result.Decision == keep.Redact {
			p.verbose.ResponseAfterPolicy(finalBody, result.Rule)
		} else {
			p.verbose.ResponseAllowed()
		}
	}

	// Return response to agent.
	copyResponseHeaders(w, upstreamResp)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(finalBody)
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

	// 4. Evaluate stream via pipeline (reassemble, decompose, evaluate, reassemble/synthesize).
	// Verbose: log reassembled response before policy.
	if p.verbose != nil {
		// Reassemble just for logging — the pipeline will do it again internally.
		if assembled, logErr := p.codec.ReassembleStream(events); logErr == nil {
			p.verbose.ResponseRaw(assembled)
		}
	}

	streamResult, err := llm.EvaluateStream(p.engine, p.codec, events, p.scope, p.llmCfg)
	if err != nil {
		writeInternalError(w, "stream policy evaluation error")
		return
	}
	if p.logger != nil {
		for _, a := range streamResult.Audits {
			p.logger.Log(a)
		}
	}
	if streamResult.Decision == keep.Deny {
		p.logDebug("stream denied", "rule", streamResult.Rule, "message", streamResult.Message)
		if p.verbose != nil {
			p.verbose.ResponseDenied(streamResult.Rule, streamResult.Message)
		}
		writePolicyDeny(w, streamResult.Rule, streamResult.Message)
		return
	}
	if p.verbose != nil {
		if streamResult.Decision == keep.Redact {
			p.verbose.ResponseAfterPolicy(streamResult.Body, streamResult.Rule)
		} else {
			p.verbose.ResponseAllowed()
		}
	}
	outEvents := streamResult.Events

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
