package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
)

func TestReferencesParam(t *testing.T) {
	env := mustNewEnv(t)

	tests := []struct {
		name  string
		expr  string
		field string
		want  bool
	}{
		{"dot select", "params.body == 'x'", "body", true},
		{"index string literal", `params["body"] == 'x'`, "body", true},
		{"nested select", "params.body.model == 'gpt-4'", "body", true},
		{"index into body", "size(params.body[0]) > 0", "body", true},
		{"inside function call", "hasSecrets(params.body.prompt)", "body", true},
		{"presence test", "has(params.body)", "body", true},
		{"deeply nested", "params.body.messages[0].content != ''", "body", true},
		{"other field only", "params.method == 'POST'", "body", false},
		{"different field requested", "params.body == 'x'", "headers", false},
		{"no params at all", "context.agent_id == 'a'", "body", false},
		{"field named after body substr", "params.bodyguard == 'x'", "body", false},
		{"dynamic index not literal", "params[params.method] == 'x'", "body", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := mustCompile(t, env, tt.expr)
			if got := prog.ReferencesParam(tt.field); got != tt.want {
				t.Errorf("ReferencesParam(%q) on %q = %v, want %v", tt.field, tt.expr, got, tt.want)
			}
		})
	}
}

func TestReferencesParam_MultipleFields(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.body.model == 'gpt-4' && params.headers['x'] == 'y'")

	for _, f := range []string{"body", "headers"} {
		if !prog.ReferencesParam(f) {
			t.Errorf("ReferencesParam(%q) = false, want true", f)
		}
	}
	if prog.ReferencesParam("method") {
		t.Error("ReferencesParam(\"method\") = true, want false")
	}
}

func TestReferencesParam_NilProgram(t *testing.T) {
	// A nil *Program must not panic and reports no references. This matters
	// because a rule with no when clause has a nil compiled program.
	var prog *keepcel.Program
	if prog.ReferencesParam("body") {
		t.Error("nil program ReferencesParam = true, want false")
	}
}
