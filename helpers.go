package keep

import (
	"fmt"
	"strings"
	"time"

	"github.com/majorcontext/keep/internal/engine"
)

// SafeEvaluate wraps Engine.Evaluate with panic recovery so the host process
// never crashes due to a policy evaluation bug. On panic it returns
// EvalResult{Decision: Deny} and an error describing the panic (fail-closed).
func SafeEvaluate(eng *Engine, call Call, scope string) (result EvalResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keep: panic during evaluation: %v", r)
			result = EvalResult{Decision: Deny}
		}
	}()
	return eng.Evaluate(call, scope)
}

// NewHTTPCall constructs a Call for HTTP request policy evaluation.
// The operation is formatted as "METHOD host/path" (e.g. "GET api.github.com/repos").
// Method is uppercased. Path is expected to include a leading slash.
// Context.Scope is not set — callers should assign it based on their deployment convention.
func NewHTTPCall(method, host, path string) Call {
	m := strings.ToUpper(method)
	return Call{
		Operation: m + " " + host + path,
		Params: map[string]any{
			"method": m,
			"host":   host,
			"path":   path,
		},
		Context: engine.CallContext{
			Timestamp: time.Now(),
		},
	}
}

// NewMCPCall constructs a Call for MCP tool-use policy evaluation.
// The operation is the tool name as-is. Params are passed through directly (may be nil).
// Context.Scope is not set — callers should assign it based on their deployment convention.
func NewMCPCall(tool string, params map[string]any) Call {
	return Call{
		Operation: tool,
		Params:    params,
		Context: engine.CallContext{
			Timestamp: time.Now(),
		},
	}
}
