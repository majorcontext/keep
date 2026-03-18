package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
)

// helpers

func mustNewEnv(t *testing.T) *keepcel.Env {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}
	return env
}

func mustCompile(t *testing.T, env *keepcel.Env, expr string) *keepcel.Program {
	t.Helper()
	prog, err := env.Compile(expr)
	if err != nil {
		t.Fatalf("Compile(%q) error: %v", expr, err)
	}
	return prog
}

// --- compile tests ---

func TestCompile_SimpleComparison(t *testing.T) {
	env := mustNewEnv(t)
	if _, err := env.Compile("params.priority == 0"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompile_StringMethod(t *testing.T) {
	env := mustNewEnv(t)
	if _, err := env.Compile("params.text.contains('hello')"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompile_LogicOperators(t *testing.T) {
	env := mustNewEnv(t)
	if _, err := env.Compile("params.a > 1 && params.b < 10"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompile_InvalidExpression(t *testing.T) {
	env := mustNewEnv(t)
	_, err := env.Compile("params.priority ===")
	if err == nil {
		t.Error("expected compilation error for invalid expression, got nil")
	}
}

// --- eval tests ---

func TestEval_SimpleComparison(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.priority == 0")
	got, err := prog.Eval(map[string]any{"priority": int64(0)}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_StringContains(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.text.contains('hello')")
	got, err := prog.Eval(map[string]any{"text": "hello world"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_Collection_Any(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.to.exists(x, x.endsWith('@test.com'))")
	got, err := prog.Eval(map[string]any{
		"to": []any{"a@test.com", "b@other.com"},
	}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_Collection_All(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.to.all(x, x.endsWith('@test.com'))")
	got, err := prog.Eval(map[string]any{
		"to": []any{"a@test.com", "b@test.com"},
	}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_MembershipIn(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.team in ['A', 'B']")
	got, err := prog.Eval(map[string]any{"team": "A"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_Context(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "context.agent_id == 'test-agent'")
	got, err := prog.Eval(nil, map[string]any{"agent_id": "test-agent"})
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestEval_NullSafety(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "params.missing == 'x'")
	got, err := prog.Eval(map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if got {
		t.Error("expected false for missing key, got true")
	}
}
