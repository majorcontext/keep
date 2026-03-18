package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/gateway/anthropic"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
)

// maxRequestBodySize is the maximum size of a request body we will read (4 MB).
const maxRequestBodySize = 4 * 1024 * 1024

// Proxy is the LLM gateway HTTP handler.
type Proxy struct {
	engine    *keep.Engine
	scope     string
	upstream  *url.URL
	decompose gwconfig.DecomposeConfig
	logger    *audit.Logger
	passthru  *httputil.ReverseProxy // for non-messages passthrough
	client    *http.Client           // for /v1/messages upstream requests
}

// NewProxy creates an LLM gateway proxy.
func NewProxy(engine *keep.Engine, cfg *gwconfig.GatewayConfig, logger *audit.Logger) (*Proxy, error) {
	upstream, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, fmt.Errorf("gateway: invalid upstream URL: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(upstream)

	return &Proxy{
		engine:    engine,
		scope:     cfg.Scope,
		upstream:  upstream,
		decompose: cfg.Decompose,
		logger:    logger,
		passthru:  rp,
		client:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// ServeHTTP intercepts /v1/messages requests for policy evaluation.
// All other paths are reverse-proxied without evaluation.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/messages" {
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
	// 1. Read request body.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodySize))
	if err != nil {
		writeInternalError(w, "failed to read request body")
		return
	}

	// 2. Parse as MessagesRequest.
	var req anthropic.MessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeInternalError(w, "failed to parse request body")
		return
	}

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
				}
			}
		}
	}

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

	// 7. Forward to upstream.
	upstreamURL := p.upstream.String() + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(forwardBody))
	if err != nil {
		writeInternalError(w, "failed to create upstream request")
		return
	}

	// Copy relevant headers.
	for _, h := range []string{"Authorization", "Content-Type", "anthropic-version", "x-api-key"} {
		if v := r.Header.Get(h); v != "" {
			upstreamReq.Header.Set(h, v)
		}
	}

	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		writeInternalError(w, "upstream request failed")
		return
	}
	defer upstreamResp.Body.Close()

	// 8. Read response body.
	respBody, err := io.ReadAll(upstreamResp.Body)
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
			writePolicyDeny(w, result.Rule, result.Message)
			return
		}

		if i >= respSummaryOffset {
			if result.Decision == keep.Redact {
				respHasRedaction = true
			}
			respBlockResults = append(respBlockResults, anthropic.BlockResult{
				BlockIndex: i - respSummaryOffset,
				Result:     result,
			})
		}
	}

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
func buildRequestBlockMap(req *anthropic.MessagesRequest, cfg gwconfig.DecomposeConfig) []blockPosition {
	var positions []blockPosition

	for msgIdx, msg := range req.Messages {
		blocks := msg.ContentBlocks()
		for blockIdx, block := range blocks {
			switch block.Type {
			case "tool_result":
				if cfg.ToolResultEnabled() {
					positions = append(positions, blockPosition{
						MessageIndex: msgIdx,
						BlockIndex:   blockIdx,
					})
				}
			case "text":
				if cfg.TextEnabled() {
					positions = append(positions, blockPosition{
						MessageIndex: msgIdx,
						BlockIndex:   blockIdx,
					})
				}
			}
		}
	}

	return positions
}

// copyResponseHeaders copies relevant headers from an upstream response to the client.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{"Content-Type", "X-Request-Id"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}
