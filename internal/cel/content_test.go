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

// TestHasSecrets_FieldScoped verifies that hasSecrets(params.X) checks only
// the named field and not every field in params. If only params.secret_field
// contains a secret, then hasSecrets(params.safe_field) must return false.
func TestHasSecrets_FieldScoped(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}

	params := map[string]any{
		"safe_field":   "nothing secret here",
		"secret_field": "key is AKIAIOSFODNN7REALKEY",
	}

	// Checking the safe field should return false even though secret_field has a secret.
	expr := keepcel.InjectOriginalParams("hasSecrets(params.safe_field)")
	prog, err := env.Compile(expr)
	if err != nil {
		t.Fatalf("Compile(%q) error: %v", expr, err)
	}
	got, err := prog.Eval(params, nil, params)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if got {
		t.Error("hasSecrets(params.safe_field) should return false when only secret_field has a secret")
	}

	// Checking the secret field should return true.
	expr2 := keepcel.InjectOriginalParams("hasSecrets(params.secret_field)")
	prog2, err := env.Compile(expr2)
	if err != nil {
		t.Fatalf("Compile(%q) error: %v", expr2, err)
	}
	got2, err := prog2.Eval(params, nil, params)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got2 {
		t.Error("hasSecrets(params.secret_field) should return true when secret_field has a secret")
	}
}
