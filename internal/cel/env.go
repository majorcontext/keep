// Package cel provides Keep's custom CEL environment for rule expressions.
package cel

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Env is Keep's configured CEL environment with custom functions.
type Env struct {
	env *cel.Env
}

// EnvOption configures the CEL environment.
type EnvOption func(*envConfig)

type envConfig struct {
	// will hold rate store etc. in future tasks
}

// NewEnv creates a new CEL environment with Keep's input variables
// (params, context) and all custom functions registered.
func NewEnv(opts ...EnvOption) (*Env, error) {
	_ = &envConfig{} // apply options in future tasks

	env, err := cel.NewEnv(
		// params and context are dynamic maps: any field access works at runtime.
		cel.Variable("params", cel.DynType),
		cel.Variable("context", cel.DynType),

		// _timestamp is injected by Eval from ctx["timestamp"]; used by temporal functions.
		cel.Variable("_timestamp", cel.TimestampType),

		// inTimeWindow(_timestamp, start, end, tz) bool
		cel.Function("inTimeWindow",
			cel.Overload(
				"inTimeWindow_timestamp_string_string_string",
				[]*cel.Type{cel.TimestampType, cel.StringType, cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					ts, ok := args[0].(types.Timestamp)
					if !ok {
						return types.Bool(false)
					}
					start, ok2 := args[1].(types.String)
					end, ok3 := args[2].(types.String)
					tz, ok4 := args[3].(types.String)
					if !ok2 || !ok3 || !ok4 {
						return types.Bool(false)
					}
					return types.Bool(InTimeWindow(string(start), string(end), string(tz), ts.Time))
				}),
			),
		),

		// dayOfWeek(_timestamp) string — UTC weekday name
		cel.Function("dayOfWeek",
			cel.Overload(
				"dayOfWeek_timestamp",
				[]*cel.Type{cel.TimestampType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.String("")
					}
					return types.String(DayOfWeek(ts.Time))
				}),
			),
			// dayOfWeek(_timestamp, tz) string — timezone-aware weekday name
			cel.Overload(
				"dayOfWeek_timestamp_string",
				[]*cel.Type{cel.TimestampType, cel.StringType},
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					ts, ok := args[0].(types.Timestamp)
					if !ok {
						return types.String("")
					}
					tz, ok2 := args[1].(types.String)
					if !ok2 {
						return types.String("")
					}
					return types.String(DayOfWeekTZ(string(tz), ts.Time))
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("cel: create env: %w", err)
	}
	return &Env{env: env}, nil
}

// Program is a compiled CEL expression ready for evaluation.
type Program struct {
	prog cel.Program
}

// Compile parses and type-checks a CEL expression string.
// Temporal sugar expressions (inTimeWindow, dayOfWeek) are rewritten to inject
// _timestamp as their first argument before compilation.
func (e *Env) Compile(expr string) (*Program, error) {
	expr = rewriteTemporalCalls(expr)

	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("cel: compile %q: %w", expr, iss.Err())
	}
	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel: program %q: %w", expr, err)
	}
	return &Program{prog: prog}, nil
}

// Eval evaluates a compiled program against the given params and context.
// Returns the boolean result. Returns an error if evaluation fails or
// the expression does not return a bool.
// Missing field accesses return false rather than an error.
func (p *Program) Eval(params map[string]any, ctx map[string]any) (bool, error) {
	if params == nil {
		params = map[string]any{}
	}
	if ctx == nil {
		ctx = map[string]any{}
	}

	// Extract timestamp from context for temporal functions.
	var ts time.Time
	if raw, ok := ctx["timestamp"]; ok {
		if t, ok := raw.(time.Time); ok {
			ts = t
		}
	}

	out, _, err := p.prog.Eval(map[string]any{
		"params":     params,
		"context":    ctx,
		"_timestamp": ts,
	})
	if err != nil {
		// Treat missing field / no such key errors as false so that expressions
		// like `params.missing == 'x'` are safe when the key is absent.
		return false, nil
	}

	bv, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("cel: expression returned %s, want bool", out.Type())
	}
	return bool(bv), nil
}
