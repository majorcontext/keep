package cel_test

import (
	"encoding/json"
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

// FuzzParseGraphQLRequest checks the GraphQL request parser never panics and
// always returns the documented shape, whatever the query or variables hold.
func FuzzParseGraphQLRequest(f *testing.F) {
	seeds := []string{
		`{ viewer { login } }`,
		`mutation($input: CreatePullRequestInput!) { createPullRequest(input: $input) { clientMutationId } }`,
		`mutation { ...A } fragment A on Mutation { ...B } fragment B on Mutation { createRef(input: {repositoryId: "R"}) }`,
		`mutation { ...A } fragment A on Mutation { ...A }`,
		`query Q { a } mutation M { b(x: [1, 2.5, "s", true, null, ENUM, {k: $v}]) }`,
		`subscription { s }`,
		`mutation {`,
		`"""desc""" query { a }`,
		"",
		"\x00\xff",
	}
	for _, s := range seeds {
		f.Add(s, `{"v": 1}`)
	}
	f.Fuzz(func(t *testing.T, query, varsJSON string) {
		var vars any
		_ = json.Unmarshal([]byte(varsJSON), &vars)
		res := keepcel.ParseGraphQLRequest(map[string]any{"query": query, "variables": vars})
		valid, ok := res["valid"].(bool)
		if !ok {
			t.Fatalf("valid is %T", res["valid"])
		}
		if msg, _ := res["error"].(string); valid == (msg != "") {
			t.Fatalf("valid=%v but error=%q", valid, msg)
		}
		for _, k := range []string{"operations", "queries", "mutations", "subscriptions"} {
			if _, ok := res[k].([]any); !ok {
				t.Fatalf("%s is %T, want []any", k, res[k])
			}
		}
	})
}
