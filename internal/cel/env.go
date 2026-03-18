// Package cel provides Keep's custom CEL environment for rule expressions.
package cel

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/majorcontext/keep/internal/rate"
)

// Env is Keep's configured CEL environment with custom functions.
type Env struct {
	env *cel.Env
}

// EnvOption configures the CEL environment.
type EnvOption func(*envConfig)

type envConfig struct {
	rateStore *rate.Store
}

// WithRateStore configures the CEL environment with a rate counter store,
// enabling the rateCount(key, window) function.
func WithRateStore(store *rate.Store) EnvOption {
	return func(cfg *envConfig) {
		cfg.rateStore = store
	}
}

// NewEnv creates a new CEL environment with Keep's input variables
// (params, context) and all custom functions registered.
func NewEnv(opts ...EnvOption) (*Env, error) {
	cfg := &envConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

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

		// containsAny(field, terms) bool — case-insensitive substring match against any term
		cel.Function("containsAny",
			cel.Overload(
				"containsAny_string_list",
				[]*cel.Type{cel.StringType, cel.ListType(cel.StringType)},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					field, ok := args[0].(types.String)
					if !ok {
						return types.Bool(false)
					}
					list, ok2 := args[1].(traits.Lister)
					if !ok2 {
						return types.Bool(false)
					}
					var terms []string
					it := list.Iterator()
					for it.HasNext() == types.True {
						term := string(it.Next().(types.String))
						terms = append(terms, term)
					}
					return types.Bool(ContainsAnyFunc(string(field), terms))
				}),
			),
		),

		// estimateTokens(field) int — rough token count (len/4)
		cel.Function("estimateTokens",
			cel.Overload(
				"estimateTokens_string",
				[]*cel.Type{cel.StringType},
				cel.IntType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					field, ok := val.(types.String)
					if !ok {
						return types.Int(0)
					}
					return types.Int(EstimateTokensFunc(string(field)))
				}),
			),
		),

		// rateCount(key, window) int — increment counter and return hit count within window.
		// window is a string like "1h", "30m", "30s". Max 24h, min 1s.
		// If no rate store is configured, returns an error at eval time.
		cel.Function("rateCount",
			cel.Overload(
				"rateCount_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(func(key, window ref.Val) ref.Val {
					k := string(key.(types.String))
					w := string(window.(types.String))
					count, err := rateCountFunc(cfg.rateStore, k, w)
					if err != nil {
						return types.WrapErr(err)
					}
					return types.Int(count)
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
		msg := err.Error()
		if strings.Contains(msg, "no such key") || strings.Contains(msg, "no such field") || strings.Contains(msg, "undefined field") {
			return false, nil
		}
		return false, err
	}

	bv, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("cel: expression returned %s, want bool", out.Type())
	}
	return bool(bv), nil
}
