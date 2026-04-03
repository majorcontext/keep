package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
)

func FuzzCompile(f *testing.F) {
	// Seed with valid CEL expressions used in existing tests and rules.
	seeds := []string{
		"params.priority == 0",
		"params.text.contains('hello')",
		"params.a > 1 && params.b < 10",
		"params.to.exists(x, x.endsWith('@test.com'))",
		"params.to.all(x, x.endsWith('@test.com'))",
		"params.team in ['A', 'B']",
		"context.agent_id == 'test-agent'",
		"params.missing == 'x'",
		"params.text.contains('@here')",
		"!(context.operation in ['get_issue', 'list_issues'])",
		"estimateTokens(params.text) > 1000",
		"containsAny(params.text, ['secret', 'password'])",
		"lower(params.name) == 'test'",
		"upper(params.name) == 'TEST'",
		// Invalid expressions that should error but not panic.
		"params.priority ===",
		"invalid ++ expr",
		"",
		"{{{}}}",
		"func() {}",
		"\x00\xff\xfe",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// Create the CEL environment once — it is safe for concurrent use.
	env, err := keepcel.NewEnv()
	if err != nil {
		f.Fatalf("NewEnv() error: %v", err)
	}

	f.Fuzz(func(t *testing.T, expr string) {
		// Compile must never panic, regardless of input.
		// Errors are expected and acceptable.
		_, _ = env.Compile(expr)
	})
}
