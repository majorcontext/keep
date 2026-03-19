package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
)

// --- Unit tests for string helper functions ---

func TestLower(t *testing.T) {
	got := keepcel.LowerFunc("Hello World")
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestUpper(t *testing.T) {
	got := keepcel.UpperFunc("Hello World")
	if got != "HELLO WORLD" {
		t.Errorf("expected %q, got %q", "HELLO WORLD", got)
	}
}

// --- CEL integration tests ---

func TestLower_CEL(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "lower(params.name) == 'hello'")
	got, err := prog.Eval(map[string]any{"name": "Hello"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: lower('Hello') == 'hello'")
	}
}

func TestUpper_CEL(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "upper(params.code) == 'ABC'")
	got, err := prog.Eval(map[string]any{"code": "abc"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: upper('abc') == 'ABC'")
	}
}
