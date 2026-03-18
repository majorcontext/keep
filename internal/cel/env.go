// Package cel provides Keep's custom CEL environment for rule expressions.
package cel

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
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
func (e *Env) Compile(expr string) (*Program, error) {
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

	out, _, err := p.prog.Eval(map[string]any{
		"params":  params,
		"context": ctx,
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
