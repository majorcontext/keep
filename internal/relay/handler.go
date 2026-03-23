package relay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// RelayHandler bridges MCP tool calls to Keep policy evaluation.
type RelayHandler struct {
	engine *keep.Engine
	router *Router
	logger *audit.Logger
}

// NewRelayHandler creates a handler that evaluates policy on every tool call.
func NewRelayHandler(engine *keep.Engine, router *Router, logger *audit.Logger) *RelayHandler {
	return &RelayHandler{engine: engine, router: router, logger: logger}
}

// HandleToolCall implements mcp.Handler.
func (h *RelayHandler) HandleToolCall(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	// 1. Lookup route
	route, err := h.router.Lookup(name)
	if err != nil {
		return nil, err
	}

	// 2. Build Keep call
	call := keep.Call{
		Operation: name,
		Params:    args,
		Context: keep.CallContext{
			AgentID:   "relay", // TODO(m1): extract agent identity from MCP initialize clientInfo or X-Agent-ID header
			Timestamp: time.Now(),
			Scope:     route.Scope,
			Direction: "request",
		},
	}

	// 3. Evaluate policy
	result, err := h.engine.Evaluate(call, route.Scope)
	if err != nil {
		return nil, fmt.Errorf("policy evaluation error: %w", err)
	}

	// 4. Log audit entry
	if h.logger != nil {
		h.logger.Log(result.Audit)
	}

	// 5. Handle decision
	switch result.Decision {
	case keep.Deny:
		msg := result.Message
		if msg == "" {
			msg = "Denied by policy"
		}
		return nil, fmt.Errorf("policy denied: %s (rule: %s)", msg, result.Rule)

	case keep.Redact:
		// Apply mutations to args before forwarding
		args = keep.ApplyMutations(args, result.Mutations)
	}

	// 6. Forward to upstream
	toolResult, err := route.Client.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}

	// 7. Evaluate response-side policy
	toolResult, err = h.evaluateResponse(name, route.Scope, toolResult)
	if err != nil {
		return nil, err
	}

	return toolResult, nil
}

// evaluateResponse runs policy evaluation on the upstream response.
// It extracts text content blocks, evaluates them against response-direction
// rules, and applies any deny or redact decisions.
func (h *RelayHandler) evaluateResponse(name, scope string, toolResult *mcp.ToolCallResult) (*mcp.ToolCallResult, error) {
	if toolResult == nil || len(toolResult.Content) == 0 {
		return toolResult, nil
	}

	// Collect text blocks and their indices.
	var textParts []string
	var textIndices []int
	for i, block := range toolResult.Content {
		if block.Type == "text" {
			textParts = append(textParts, block.Text)
			textIndices = append(textIndices, i)
		}
	}
	if len(textParts) == 0 {
		return toolResult, nil
	}

	joined := strings.Join(textParts, "\n")

	call := keep.Call{
		Operation: name,
		Params:    map[string]any{"content": joined},
		Context: keep.CallContext{
			AgentID:   "relay",
			Timestamp: time.Now(),
			Scope:     scope,
			Direction: "response",
		},
	}

	result, err := h.engine.Evaluate(call, scope)
	if err != nil {
		return nil, fmt.Errorf("response policy evaluation error: %w", err)
	}

	if h.logger != nil {
		h.logger.Log(result.Audit)
	}

	switch result.Decision {
	case keep.Deny:
		msg := result.Message
		if msg == "" {
			msg = "Denied by policy"
		}
		return nil, fmt.Errorf("response policy denied: %s (rule: %s)", msg, result.Rule)

	case keep.Redact:
		mutated := keep.ApplyMutations(map[string]any{"content": joined}, result.Mutations)
		redacted, _ := mutated["content"].(string)

		if len(textIndices) == 1 {
			toolResult.Content[textIndices[0]].Text = redacted
		} else {
			// Replace first text block with full redacted content, clear the rest.
			toolResult.Content[textIndices[0]].Text = redacted
			for _, idx := range textIndices[1:] {
				toolResult.Content[idx].Text = ""
			}
		}
	}

	return toolResult, nil
}
