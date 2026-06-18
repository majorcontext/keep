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
