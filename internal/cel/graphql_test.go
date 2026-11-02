package cel

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// gqlBody builds a GraphQL-over-HTTP request body.
func gqlBody(query string, variables map[string]any) map[string]any {
	b := map[string]any{"query": query}
	if variables != nil {
		b["variables"] = variables
	}
	return b
}

func fieldsOf(t *testing.T, res map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := res[key].([]any)
	if !ok {
		t.Fatalf("%s is %T, want []any", key, res[key])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, f := range raw {
		out = append(out, f.(map[string]any))
	}
	return out
}

func mustValid(t *testing.T, res map[string]any) {
	t.Helper()
	if res["valid"] != true {
		t.Fatalf("valid = %v, error = %q; want valid", res["valid"], res["error"])
	}
	if res["error"] != "" {
		t.Fatalf("error = %q, want empty for a valid request", res["error"])
	}
}

func TestParseGraphQLRequest_Query(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`query Viewer { viewer { login } rateLimit { remaining } }`, nil))
	mustValid(t, res)

	queries := fieldsOf(t, res, "queries")
	if len(queries) != 2 || queries[0]["name"] != "viewer" || queries[1]["name"] != "rateLimit" {
		t.Fatalf("queries = %v, want [viewer rateLimit]", queries)
	}
	// Companion: a pure query reports no mutations or subscriptions.
	if m := fieldsOf(t, res, "mutations"); len(m) != 0 {
		t.Errorf("mutations = %v, want none", m)
	}
	if s := fieldsOf(t, res, "subscriptions"); len(s) != 0 {
		t.Errorf("subscriptions = %v, want none", s)
	}

	ops := res["operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("operations = %v, want 1", ops)
	}
	op := ops[0].(map[string]any)
	if op["type"] != "query" || op["name"] != "Viewer" {
		t.Errorf("operation = %v, want type query name Viewer", op)
	}
}

func TestParseGraphQLRequest_ShorthandIsQuery(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`{ viewer { login } }`, nil))
	mustValid(t, res)
	op := res["operations"].([]any)[0].(map[string]any)
	if op["type"] != "query" || op["name"] != "" {
		t.Errorf("operation = %v, want anonymous query", op)
	}
}

func TestParseGraphQLRequest_MutationVariables(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(
		`mutation CreatePR($input: CreatePullRequestInput!) { createPullRequest(input: $input) { pullRequest { url } } }`,
		map[string]any{"input": map[string]any{"repositoryId": "R_kgDOAbCd", "title": "Fix"}},
	))
	mustValid(t, res)

	muts := fieldsOf(t, res, "mutations")
	if len(muts) != 1 {
		t.Fatalf("mutations = %v, want 1", muts)
	}
	m := muts[0]
	if m["name"] != "createPullRequest" || m["alias"] != "" {
		t.Errorf("field = %v, want createPullRequest without alias", m)
	}
	input := m["args"].(map[string]any)["input"].(map[string]any)
	if input["repositoryId"] != "R_kgDOAbCd" {
		t.Errorf("input.repositoryId = %v, want R_kgDOAbCd (original case)", input["repositoryId"])
	}
	if q := fieldsOf(t, res, "queries"); len(q) != 0 {
		t.Errorf("queries = %v, want none", q)
	}
}

func TestParseGraphQLRequest_InlineLiteralsAndNestedVariables(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(
		`mutation($repo: ID!, $n: Int) {
			createRef(input: {repositoryId: $repo, name: "refs/heads/x", oid: "abc", extra: [$n, $missing], flag: true, ratio: 1.5, kind: BRANCH, nothing: null, gone: $missing})
		}`,
		map[string]any{"repo": "R_1", "n": float64(7)},
	))
	mustValid(t, res)

	input := fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)["input"].(map[string]any)
	want := map[string]any{
		"repositoryId": "R_1",
		"name":         "refs/heads/x",
		"oid":          "abc",
		"extra":        []any{float64(7), nil}, // unset variable inside a list → null
		"flag":         true,
		"ratio":        1.5,
		"kind":         "BRANCH",
		"nothing":      nil,
		// "gone" is omitted: unset variable in an object field.
	}
	if !reflect.DeepEqual(input, want) {
		t.Errorf("input = %#v\nwant   %#v", input, want)
	}
}

func TestParseGraphQLRequest_VariableDefaults(t *testing.T) {
	q := `mutation($id: ID = "R_default") { createRef(input: {repositoryId: $id}) }`

	// Absent variable → default value.
	res := ParseGraphQLRequest(gqlBody(q, nil))
	mustValid(t, res)
	input := fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)["input"].(map[string]any)
	if input["repositoryId"] != "R_default" {
		t.Errorf("repositoryId = %v, want default R_default", input["repositoryId"])
	}

	// Companion: a supplied value wins over the default.
	res = ParseGraphQLRequest(gqlBody(q, map[string]any{"id": "R_given"}))
	input = fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)["input"].(map[string]any)
	if input["repositoryId"] != "R_given" {
		t.Errorf("repositoryId = %v, want supplied R_given", input["repositoryId"])
	}

	// An explicit null is a supplied value, not "absent".
	res = ParseGraphQLRequest(gqlBody(q, map[string]any{"id": nil}))
	input = fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)["input"].(map[string]any)
	if v, ok := input["repositoryId"]; !ok || v != nil {
		t.Errorf("repositoryId = %v (present=%v), want explicit null", v, ok)
	}
}

func TestParseGraphQLRequest_Alias(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`mutation { innocent: deleteRef(input: {refId: "REF_1"}) }`, nil))
	mustValid(t, res)
	m := fieldsOf(t, res, "mutations")[0]
	if m["name"] != "deleteRef" || m["alias"] != "innocent" {
		t.Errorf("field = %v, want name deleteRef alias innocent", m)
	}
}

func TestParseGraphQLRequest_FragmentsExpanded(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`
		mutation { ...Outer ... on Mutation { addComment(input: {subjectId: "PR_1", body: "hi"}) { clientMutationId } } }
		fragment Outer on Mutation { ...Inner }
		fragment Inner on Mutation { createRef(input: {repositoryId: "R_2"}) { ref { name } } }
	`, nil))
	mustValid(t, res)
	muts := fieldsOf(t, res, "mutations")
	var names []string
	for _, m := range muts {
		names = append(names, m["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"createRef", "addComment"}) {
		t.Errorf("mutation fields = %v, want [createRef addComment]", names)
	}
}

func TestParseGraphQLRequest_DirectivesIgnored(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`mutation { mergePullRequest(input: {pullRequestId: "PR_1"}) @skip(if: true) { clientMutationId } }`, nil))
	mustValid(t, res)
	if m := fieldsOf(t, res, "mutations"); len(m) != 1 || m[0]["name"] != "mergePullRequest" {
		t.Errorf("mutations = %v, want the @skip field still reported", m)
	}
}

func TestParseGraphQLRequest_AllOperationsReported(t *testing.T) {
	// operationName selects the query, but the mutation in the same document
	// is still reported — the policy sees everything the document contains.
	body := gqlBody(`query Q { viewer { login } } mutation M { deleteRef(input: {refId: "REF_1"}) { clientMutationId } }`, nil)
	body["operationName"] = "Q"
	res := ParseGraphQLRequest(body)
	mustValid(t, res)
	if m := fieldsOf(t, res, "mutations"); len(m) != 1 || m[0]["name"] != "deleteRef" {
		t.Errorf("mutations = %v, want deleteRef", m)
	}
	if q := fieldsOf(t, res, "queries"); len(q) != 1 || q[0]["name"] != "viewer" {
		t.Errorf("queries = %v, want viewer", q)
	}
	if ops := res["operations"].([]any); len(ops) != 2 {
		t.Errorf("operations = %d, want 2", len(ops))
	}
}

func TestParseGraphQLRequest_Subscription(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`subscription { issueUpdated { id } }`, nil))
	mustValid(t, res)
	if s := fieldsOf(t, res, "subscriptions"); len(s) != 1 || s[0]["name"] != "issueUpdated" {
		t.Errorf("subscriptions = %v, want issueUpdated", s)
	}
}

func TestParseGraphQLRequest_StringEscapesDecoded(t *testing.T) {
	// The server decodes escapes, so the policy must see the decoded value.
	res := ParseGraphQLRequest(gqlBody(`mutation { createRef(input: {repositoryId: "R_ABC"}) }`, nil))
	mustValid(t, res)
	input := fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)["input"].(map[string]any)
	if input["repositoryId"] != "R_ABC" {
		t.Errorf("repositoryId = %q, want decoded R_ABC", input["repositoryId"])
	}
}

func TestParseGraphQLRequest_IntBounds(t *testing.T) {
	res := ParseGraphQLRequest(gqlBody(`mutation { f(a: 42, b: 99999999999999999999999) }`, nil))
	mustValid(t, res)
	args := fieldsOf(t, res, "mutations")[0]["args"].(map[string]any)
	if args["a"] != int64(42) {
		t.Errorf("a = %#v, want int64(42)", args["a"])
	}
	if args["b"] != "99999999999999999999999" {
		t.Errorf("b = %#v, want raw literal for out-of-range int", args["b"])
	}
}

func TestParseGraphQLRequest_Invalid(t *testing.T) {
	cycle := `mutation { ...A } fragment A on Mutation { ...B } fragment B on Mutation { ...A }`
	dup := `mutation { ...A } fragment A on Mutation { x } fragment A on Mutation { y }`

	// Exponential fan-out: each level spreads the next one twice.
	var sb strings.Builder
	sb.WriteString("mutation { ...F0 }\n")
	for i := 0; i < 20; i++ {
		sb.WriteString("fragment F")
		sb.WriteString(itoa(i))
		sb.WriteString(" on Mutation { ...F")
		sb.WriteString(itoa(i + 1))
		sb.WriteString(" ...F")
		sb.WriteString(itoa(i + 1))
		sb.WriteString(" }\n")
	}
	sb.WriteString("fragment F20 on Mutation { createRef(input: {}) }\n")

	tests := []struct {
		name    string
		body    any
		errPart string
	}{
		{"nil body", nil, "not a JSON object"},
		{"string body", "mutation { x }", "not a JSON object"},
		{"list body (batch)", []any{map[string]any{"query": "{ x }"}}, "not a JSON object"},
		{"no query", map[string]any{"variables": map[string]any{}}, `no "query"`},
		{"query not string", map[string]any{"query": 42.0}, "not a string"},
		{"variables not object", map[string]any{"query": "{ x }", "variables": "nope"}, `"variables"`},
		{"parse error", gqlBody("mutation {", nil), "parse"},
		{"no operations", gqlBody("fragment A on Query { x }", nil), "no operations"},
		{"unknown fragment", gqlBody("mutation { ...Missing }", nil), "unknown fragment"},
		{"fragment cycle", gqlBody(cycle, nil), "cycle"},
		{"duplicate fragment", gqlBody(dup, nil), "duplicate fragment"},
		{"fan-out limit", gqlBody(sb.String(), nil), "root fields"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseGraphQLRequest(tt.body)
			if res["valid"] != false {
				t.Fatalf("valid = %v, want false", res["valid"])
			}
			if msg, _ := res["error"].(string); !strings.Contains(msg, tt.errPart) {
				t.Errorf("error = %q, want it to mention %q", msg, tt.errPart)
			}
			// Lists are always present and empty so .exists() never errors.
			for _, k := range []string{"operations", "queries", "mutations", "subscriptions"} {
				if l, ok := res[k].([]any); !ok || len(l) != 0 {
					t.Errorf("%s = %#v, want empty list", k, res[k])
				}
			}
		})
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func refOf(v any) ref.Val { return types.DefaultTypeAdapter.NativeToValue(v) }

// TestGraphQL_CEL exercises the function through the CEL environment: the
// access patterns a policy author writes must work on the returned value.
func TestGraphQL_CEL(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	createPR := map[string]any{
		"query":     `mutation($input: CreatePullRequestInput!) { createPullRequest(input: $input) { clientMutationId } }`,
		"variables": map[string]any{"input": map[string]any{"repositoryId": "R_kgDOAbCd"}},
	}
	comment := map[string]any{
		"query": `mutation { addComment(input: {subjectId: "PR_1", body: "x"}) { clientMutationId } }`,
	}
	query := map[string]any{"query": `{ viewer { login } }`}

	tests := []struct {
		name string
		expr string
		body any
		want bool
	}{
		{"valid", "graphql(params.body).valid", createPR, true},
		{"invalid input", "graphql(params.body).valid", "nope", false},
		{"invalid has error", "graphql(params.body).error != ''", "nope", true},
		{"mutation by name", "graphql(params.body).mutations.exists(m, m.name == 'createPullRequest')", createPR, true},
		{"name in list", "graphql(params.body).mutations.exists(m, m.name in ['mergePullRequest', 'createPullRequest'])", createPR, true},
		{"query has no mutations", "graphql(params.body).mutations.exists(m, true)", query, false},
		{"has input", "graphql(params.body).mutations.all(m, has(m.args.input))", createPR, true},
		{"has repositoryId", "graphql(params.body).mutations.exists(m, has(m.args.input.repositoryId))", createPR, true},
		{"repositoryId absent", "graphql(params.body).mutations.exists(m, has(m.args.input.repositoryId))", comment, false},
		{"repositoryId in list", "graphql(params.body).mutations.exists(m, m.args.input.repositoryId in ['R_kgDOAbCd'])", createPR, true},
		{"repositoryId not in list", "graphql(params.body).mutations.exists(m, !(m.args.input.repositoryId in ['R_other']))", createPR, true},
		{"invalid exists is false", "graphql(params.body).mutations.exists(m, true)", "nope", false},
		{"operations type", "graphql(params.body).operations.exists(o, o.type == 'mutation')", comment, true},
		{"size", "size(graphql(params.body).mutations) == 1", comment, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := env.Compile(InjectOriginalParams(tt.expr))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := prog.Eval(map[string]any{"body": tt.body}, nil)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

// TestGraphQL_CEL_OriginalCase checks the two-argument form parses the
// original-case body when params have been lowered, and ignores a second
// argument that is not the original form of the first.
func TestGraphQL_CEL_OriginalCase(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]any{"query": `mutation { createRef(input: {repositoryId: "R_kgDOAbCd"}) { clientMutationId } }`}
	lowered := map[string]any{"query": strings.ToLower(original["query"].(string))}

	prog, err := env.Compile(InjectOriginalParams("graphql(params.body).mutations.exists(m, m.name == 'createRef' && m.args.input.repositoryId == 'R_kgDOAbCd')"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := prog.Eval(map[string]any{"body": lowered}, nil, map[string]any{"body": original})
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("expected the original-case body to be parsed (camelCase name, case-sensitive ID)")
	}

	// Companion: without the original, the lowered body is what gets parsed,
	// so the camelCase name no longer matches.
	got, err = prog.Eval(map[string]any{"body": lowered}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("lowered body alone should not match the camelCase field name")
	}

	// A second argument that is not the original of the first is ignored.
	mismatch := map[string]any{"query": `mutation { deleteRef(input: {refId: "X"}) { clientMutationId } }`}
	if v := originalGraphQLBody(refOf(lowered), refOf(mismatch)); !reflect.DeepEqual(refToNative(v), lowered) {
		t.Errorf("originalGraphQLBody picked %v, want the first argument", refToNative(v))
	}
}

func TestInjectOriginalParams_GraphQL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"graphql(params.body).valid", "graphql(params.body, _originalParams.body).valid"},
		{"graphql(params.body).mutations.exists(m, hasSecrets(m.name))", "graphql(params.body, _originalParams.body).mutations.exists(m, hasSecrets(m.name, _originalParams))"},
		// Not call sites of the global function:
		{"params.path == 'graphql(x)'", "params.path == 'graphql(x)'"},
		{`params.path == "/graphql(" && graphql(params.body).valid`, `params.path == "/graphql(" && graphql(params.body, _originalParams.body).valid`},
		{"mygraphql(params.body)", "mygraphql(params.body)"},
		{"params.graphql(params.body)", "params.graphql(params.body)"},
	}
	for _, tt := range tests {
		if got := InjectOriginalParams(tt.in); got != tt.want {
			t.Errorf("InjectOriginalParams(%q)\n  got  %q\n  want %q", tt.in, got, tt.want)
		}
	}
}

func TestGraphQL_ReferencesBody(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	prog, err := env.Compile(InjectOriginalParams("graphql(params.body).mutations.exists(m, true)"))
	if err != nil {
		t.Fatal(err)
	}
	if !prog.ReferencesParam("body") {
		t.Error("graphql(params.body) must count as a params.body reference")
	}
	// Companion: it is not mistaken for a reference to another field.
	if prog.ReferencesParam("path") {
		t.Error("graphql(params.body) must not count as a params.path reference")
	}
}
