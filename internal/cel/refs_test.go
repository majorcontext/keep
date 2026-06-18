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
		// Opaque uses of the params map cannot be resolved to a field, so
		// detection fails SAFE: ReferencesParam answers true for every field.
		{"dynamic index key", "params[params.method] == 'x'", "body", true},
		{"computed string index key", `params["bo" + "dy"] == 'x'`, "body", true},
		{"whole-map op", "size(params) > 0", "body", true},
		{"params wrapped in dyn", "dyn(params).body == 'x'", "body", true},
		{"opaque use alongside specific field", "params.a == 'x' && size(params) > 0", "body", true},
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

// TestReferencesParam_FailSafeDoesNotOverTrigger verifies that recognized
// single-field access does NOT mark the program opaque: a rule that only reads
// params.method must not report a reference to params.body (which would force a
// gatekeeper to buffer the body for every method-only rule).
func TestReferencesParam_FailSafeDoesNotOverTrigger(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.method == 'POST' && params.host == 'api.example.com'")
	if prog.ReferencesParam("body") {
		t.Error("ReferencesParam(\"body\") = true for a method/host-only rule, want false")
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

// TestParamsBodyNilSemantics pins the CEL runtime behavior that
// NewHTTPCallWithBody's docstring relies on: a nil body is stored under the
// "body" key, so has(params.body) is true while params.body != null is false.
// This guards the documented contract against a cel-go change to nil-valued map
// key handling.
func TestParamsBodyNilSemantics(t *testing.T) {
	env := mustNewEnv(t)

	withNil := map[string]any{"body": nil}                          // NewHTTPCallWithBody(nil)
	withVal := map[string]any{"body": map[string]any{"model": "x"}} // populated body
	absent := map[string]any{}                                      // NewHTTPCall (no body key)

	tests := []struct {
		expr   string
		params map[string]any
		want   bool
	}{
		{"has(params.body)", withNil, true},
		{"has(params.body)", absent, false},
		{"params.body != null", withNil, false},
		{"params.body != null", withVal, true},
		{"params.body != null", absent, false},
	}
	for _, tt := range tests {
		prog := mustCompile(t, env, tt.expr)
		got, err := prog.Eval(tt.params, nil)
		if err != nil {
			t.Fatalf("Eval(%q, %v) error: %v", tt.expr, tt.params, err)
		}
		if got != tt.want {
			t.Errorf("Eval(%q, %v) = %v, want %v", tt.expr, tt.params, got, tt.want)
		}
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
