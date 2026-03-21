package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/secrets"
)

// --- Unit tests for content helper functions ---

func TestContainsAny_Match(t *testing.T) {
	got := keepcel.ContainsAnyFunc("hello world", []string{"hello", "test"})
	if !got {
		t.Error("expected true: 'hello' is present in 'hello world'")
	}
}

func TestContainsAny_NoMatch(t *testing.T) {
	got := keepcel.ContainsAnyFunc("hello world", []string{"foo", "bar"})
	if got {
		t.Error("expected false: neither 'foo' nor 'bar' is in 'hello world'")
	}
}

func TestContainsAny_CaseInsensitive(t *testing.T) {
	got := keepcel.ContainsAnyFunc("HELLO WORLD", []string{"hello"})
	if !got {
		t.Error("expected true: case-insensitive match for 'hello' in 'HELLO WORLD'")
	}
}

func TestEstimateTokens(t *testing.T) {
	got := keepcel.EstimateTokensFunc("hello world")
	// len("hello world") = 11, 11/4 = 2
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// --- CEL integration tests ---

func TestContainsAny_CEL(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "containsAny(params.text, ['hello', 'world'])")
	got, err := prog.Eval(map[string]any{"text": "hello there"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: 'hello' is in 'hello there'")
	}
}

func TestEstimateTokens_CEL(t *testing.T) {
	env := mustNewEnv(t)
	// A string with more than 40 chars so len/4 > 10
	prog := mustCompile(t, env, "estimateTokens(params.content) > 10")
	got, err := prog.Eval(map[string]any{"content": "this is a fairly long string that has more than forty characters total"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: long string should have token estimate > 10")
	}
}

func TestHasSecrets_True(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, env, "hasSecrets(params.text)")
	got, err := prog.Eval(map[string]any{"text": "key is AKIAIOSFODNN7REALKEY"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected hasSecrets to return true for AWS key")
	}
}

func TestHasSecrets_False(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, env, "hasSecrets(params.text)")
	got, err := prog.Eval(map[string]any{"text": "nothing secret here"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if got {
		t.Error("expected hasSecrets to return false for clean text")
	}
}
