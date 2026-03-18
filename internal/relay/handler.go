package relay

import (
	"context"
	"fmt"
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
	return route.Client.CallTool(ctx, name, args)
}
