package engine

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

func TestEvaluator_RequiresBody(t *testing.T) {
	tests := []struct {
		name  string
		rules []config.Rule
		want  bool
	}{
		{
			name:  "no rules",
			rules: nil,
			want:  false,
		},
		{
			name: "rule without when clause",
			rules: []config.Rule{
				{Name: "allow-all", Action: config.ActionLog, Match: config.Match{Operation: "*"}},
			},
			want: false,
		},
		{
			name: "when references other params only",
			rules: []config.Rule{
				{Name: "method", Action: config.ActionDeny, Match: config.Match{When: "params.method == 'DELETE'"}},
			},
			want: false,
		},
		{
			name: "single rule references body",
			rules: []config.Rule{
				{Name: "model", Action: config.ActionDeny, Match: config.Match{When: "params.body.model == 'gpt-4'"}},
			},
			want: true,
		},
		{
			name: "body reference in one of several rules",
			rules: []config.Rule{
				{Name: "method", Action: config.ActionDeny, Match: config.Match{When: "params.method == 'DELETE'"}},
				{Name: "no-when", Action: config.ActionLog, Match: config.Match{Operation: "GET *"}},
				{Name: "secrets", Action: config.ActionDeny, Match: config.Match{When: "hasSecrets(params.body.prompt)"}},
			},
			want: true,
		},
		{
			name: "index access counts",
			rules: []config.Rule{
				{Name: "idx", Action: config.ActionDeny, Match: config.Match{When: `params["body"] != ''`}},
			},
			want: true,
		},
		{
			name: "opaque params use fails safe",
			rules: []config.Rule{
				{Name: "method", Action: config.ActionDeny, Match: config.Match{When: "params.method == 'DELETE'"}},
				{Name: "whole-map", Action: config.ActionDeny, Match: config.Match{When: "size(params) > 20"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := makeEvaluator(t, tt.rules)
			if got := ev.RequiresBody(); got != tt.want {
				t.Errorf("RequiresBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEvaluator_RequiresBody_ViaAlias verifies body references hidden behind a
// rule alias are still detected, since detection runs on the resolved expression.
func TestEvaluator_RequiresBody_ViaAlias(t *testing.T) {
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{Name: "aliased", Action: config.ActionDeny, Match: config.Match{When: "bigModel"}},
	}
	aliases := map[string]string{"bigModel": "params.body.model == 'gpt-4'"}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, aliases, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ev.RequiresBody() {
		t.Error("RequiresBody() = false, want true (body reference via alias)")
	}
}

func TestEvaluator_RequiresBodyFor(t *testing.T) {
	const graphqlOp = "POST api.github.com/graphql"
	const pushOp = "POST github.com/o/r.git/git-receive-pack"

	pinned := []config.Rule{
		{Name: "path-only", Action: config.ActionDeny, Match: config.Match{When: "params.path.startsWith('/admin')"}},
		{Name: "gql", Action: config.ActionDeny, Match: config.Match{
			Operation: "post api.github.com/graphql",
			When:      "graphql(params.body).mutations.exists(m, m.name == 'deleteRef')",
		}},
	}
	unpinned := []config.Rule{
		{Name: "secrets", Action: config.ActionDeny, Match: config.Match{When: "hasSecrets(params.body)"}},
	}
	noBody := []config.Rule{
		{Name: "method", Action: config.ActionDeny, Match: config.Match{Operation: "post *", When: "params.method == 'POST'"}},
	}

	tests := []struct {
		name          string
		rules         []config.Rule
		caseSensitive bool
		op            string
		want          bool
	}{
		{"pinned rule matches its operation", pinned, false, graphqlOp, true},
		{"pinned rule matches mixed-case method and host", pinned, false, "post API.GitHub.com/graphql", true},
		{"pinned rule skips other operations", pinned, false, pushOp, false},
		{"pinned rule skips other paths on the same host", pinned, false, "POST api.github.com/repos/o/r/pulls", false},
		{"unpinned body rule applies to every call", unpinned, false, pushOp, true},
		{"unpinned body rule applies to graphql too", unpinned, false, graphqlOp, true},
		{"no body rules", noBody, false, graphqlOp, false},
		{"case-sensitive scope matches exact case", []config.Rule{{Name: "gql", Action: config.ActionDeny, Match: config.Match{
			Operation: "POST api.github.com/graphql", When: "graphql(params.body).valid == false"}}}, true, graphqlOp, true},
		{"case-sensitive scope does not fold case", []config.Rule{{Name: "gql", Action: config.ActionDeny, Match: config.Match{
			Operation: "POST api.github.com/graphql", When: "graphql(params.body).valid == false"}}}, true, "post api.github.com/graphql", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := keepcel.NewEnv()
			if err != nil {
				t.Fatal(err)
			}
			ev, err := NewEvaluator(env, "http", config.ModeEnforce, config.ErrorModeClosed, tt.rules, nil, nil, nil, tt.caseSensitive)
			if err != nil {
				t.Fatal(err)
			}
			if got := ev.RequiresBodyFor(tt.op); got != tt.want {
				t.Errorf("RequiresBodyFor(%q) = %v, want %v", tt.op, got, tt.want)
			}
			// RequiresBodyFor never answers true where the scope-wide
			// RequiresBody says no body rule exists.
			if !ev.RequiresBody() && ev.RequiresBodyFor(tt.op) {
				t.Error("RequiresBodyFor true while RequiresBody false")
			}
		})
	}
}
