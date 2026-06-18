// Package cel provides Keep's custom CEL environment for rule expressions.
package cel

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/majorcontext/keep/internal/rate"
	"github.com/majorcontext/keep/internal/secrets"
)

// Env is Keep's configured CEL environment with custom functions.
type Env struct {
	env *cel.Env
	cfg *envConfig
}

// EnvOption configures the CEL environment.
type EnvOption func(*envConfig)

type envConfig struct {
	rateStore      *rate.Store
	secretDetector *secrets.Detector
}

// WithSecretDetector configures the CEL environment with a secret detector,
// enabling the hasSecrets(field) function.
func WithSecretDetector(d *secrets.Detector) EnvOption {
	return func(cfg *envConfig) {
		cfg.secretDetector = d
	}
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

		// _originalParams holds pre-normalization params for functions like hasSecrets
		// that need original-case values. In case-sensitive mode, this equals params.
		cel.Variable("_originalParams", cel.DynType),

		// now is injected by Eval from ctx["timestamp"]; used by temporal functions.
		cel.Variable("now", cel.TimestampType),

		// inTimeWindow(now, start, end, tz) bool
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

		// lower(string) -> string
		cel.Function("lower",
			cel.Overload("lower_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					return types.String(LowerFunc(string(val.(types.String))))
				}),
			),
		),

		// upper(string) -> string
		cel.Function("upper",
			cel.Overload("upper_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					return types.String(UpperFunc(string(val.(types.String))))
				}),
			),
		),

		// matchesDomain(string, list(string)) -> bool
		cel.Function("matchesDomain",
			cel.Overload("matchesDomain_string_list",
				[]*cel.Type{cel.StringType, cel.ListType(cel.StringType)},
				cel.BoolType,
				cel.BinaryBinding(func(email, domains ref.Val) ref.Val {
					e := string(email.(types.String))
					list := domains.(traits.Lister)
					var ds []string
					it := list.Iterator()
					for it.HasNext() == types.True {
						ds = append(ds, string(it.Next().(types.String)))
					}
					return types.Bool(MatchesDomainFunc(e, ds))
				}),
			),
		),

		// dayOfWeek(now) string — UTC weekday name
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
			// dayOfWeek(now, tz) string — timezone-aware weekday name
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

		// hasSecrets(string) bool — returns true if gitleaks detects secrets.
		// When case normalization is active, the string arg is lowered.
		// The two-arg overload hasSecrets(string, dyn) uses the original params
		// map to detect secrets with original casing.
		cel.Function("hasSecrets",
			cel.Overload("hasSecrets_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s, ok := val.(types.String)
					if !ok {
						return types.Bool(false)
					}
					if cfg.secretDetector == nil {
						return types.Bool(false)
					}
					findings := cfg.secretDetector.Detect(string(s))
					return types.Bool(len(findings) > 0)
				}),
			),
			// hasSecrets(loweredValue, originalFieldValue) — uses original-case field value for detection.
			// The engine rewrites hasSecrets(params.X) to hasSecrets(params.X, _originalParams.X)
			// when case normalization is active, so the second arg is the original-case string
			// for that specific field only.
			cel.Overload("hasSecrets_string_dyn",
				[]*cel.Type{cel.StringType, cel.DynType},
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if cfg.secretDetector == nil {
						return types.Bool(false)
					}
					// Prefer the second arg (original-case field value) for detection.
					if orig, ok := args[1].(types.String); ok {
						findings := cfg.secretDetector.Detect(string(orig))
						return types.Bool(len(findings) > 0)
					}
					// Fallback: use the first arg (lowered value) directly.
					s, ok := args[0].(types.String)
					if !ok {
						return types.Bool(false)
					}
					findings := cfg.secretDetector.Detect(string(s))
					return types.Bool(len(findings) > 0)
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("cel: create env: %w", err)
	}
	return &Env{env: env, cfg: cfg}, nil
}

// Program is a compiled CEL expression ready for evaluation.
type Program struct {
	prog cel.Program
	// paramRefs is the set of top-level fields referenced off the params
	// input variable (e.g. "body", "headers"), computed once at compile time.
	paramRefs map[string]bool
	// paramsOpaque is set when the expression uses the params map in a way the
	// syntactic walker cannot resolve to specific fields (see collectParamRefs).
	// When true, ReferencesParam answers true for every field — failing safe.
	paramsOpaque bool
}

// Compile parses and type-checks a CEL expression string.
func (e *Env) Compile(expr string) (*Program, error) {

	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("cel: compile %q: %w", expr, iss.Err())
	}
	prog, err := e.env.Program(ast, cel.CostLimit(100000))
	if err != nil {
		return nil, fmt.Errorf("cel: program %q: %w", expr, err)
	}
	refs, opaque := collectParamRefs(ast.NativeRep())
	return &Program{prog: prog, paramRefs: refs, paramsOpaque: opaque}, nil
}

// ReferencesParam reports whether the compiled expression may read the given
// field off the params input variable. It returns true for idiomatic dot- or
// index-access (params.body, params["body"], including nested forms like
// params.body.foo), and — failing safe — for every field when the expression
// touches the params map opaquely (see collectParamRefs). The result is
// computed at compile time, so this is a cheap lookup.
func (p *Program) ReferencesParam(field string) bool {
	if p == nil {
		return false
	}
	return p.paramsOpaque || p.paramRefs[field]
}

// collectParamRefs walks a compiled AST and returns the set of top-level fields
// read off the params input variable, plus an "opaque" flag. It recognizes both
// dot-selection (params.body) and index access with a string literal
// (params["body"]), including those nested inside macros (has, exists, map,
// filter), the `in` operator, and ternaries — i.e. every idiomatic way a rule
// reads a field.
//
// Because it is a purely syntactic matcher, it cannot resolve a field read
// through a computed index key (params["bo"+"dy"]), the whole params map used as
// a value (size(params), dyn(params).body), or params rebound to a comprehension
// variable. Rather than silently report "no reference" for those — which would
// let a fail-safe trigger (see Engine.RequiresBody) skip a field the rule
// actually reads — it returns opaque=true whenever params is used in any form
// other than a recognized field access. Callers then treat the expression as
// potentially referencing any field.
//
// Detection: every occurrence of the bare params identifier is either consumed
// by a recognized field access (a select on params, or an index of params with a
// string-literal key) or it is not. If any occurrence is left unconsumed, params
// escaped into an unresolvable position and opaque is set.
//
// Coupled to cel-go/common/ast (PostOrderVisit, SelectKind/CallKind, the index
// operators); audit this walk when upgrading cel-go.
func collectParamRefs(a *celast.AST) (refs map[string]bool, opaque bool) {
	if a == nil {
		return nil, false
	}
	refs = map[string]bool{}
	var total, consumed int
	celast.PostOrderVisit(a.Expr(), celast.NewExprVisitor(func(e celast.Expr) {
		switch e.Kind() {
		case celast.IdentKind:
			if e.AsIdent() == "params" {
				total++
			}
		case celast.SelectKind:
			sel := e.AsSelect()
			if isParamsIdent(sel.Operand()) {
				refs[sel.FieldName()] = true
				consumed++
			}
		case celast.CallKind:
			call := e.AsCall()
			if call.FunctionName() != operators.Index && call.FunctionName() != operators.OptIndex {
				return
			}
			args := call.Args()
			if len(args) != 2 || !isParamsIdent(args[0]) || args[1].Kind() != celast.LiteralKind {
				return
			}
			if s, ok := args[1].AsLiteral().Value().(string); ok {
				refs[s] = true
				consumed++
			}
		}
	}))
	opaque = total > consumed
	if len(refs) == 0 {
		refs = nil
	}
	return refs, opaque
}

// isParamsIdent reports whether e is the bare params input identifier.
func isParamsIdent(e celast.Expr) bool {
	return e.Kind() == celast.IdentKind && e.AsIdent() == "params"
}

// Eval evaluates a compiled program against the given params and context.
// Returns the boolean result. Returns an error if evaluation fails or
// the expression does not return a bool.
// Missing field accesses return false rather than an error.
//
// originalParams is optional: when provided, it is passed as _originalParams
// for functions like hasSecrets that need pre-normalization values.
func (p *Program) Eval(params map[string]any, ctx map[string]any, originalParams ...map[string]any) (bool, error) {
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

	// _originalParams defaults to params if not provided.
	origParams := params
	if len(originalParams) > 0 && originalParams[0] != nil {
		origParams = originalParams[0]
	}

	out, _, err := p.prog.Eval(map[string]any{
		"params":          params,
		"context":         ctx,
		"now":             ts,
		"_originalParams": origParams,
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
