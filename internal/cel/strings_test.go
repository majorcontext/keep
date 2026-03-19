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

func TestMatchesDomain_Exact(t *testing.T) {
	got := keepcel.MatchesDomainFunc("user@example.com", []string{"example.com", "test.org"})
	if !got {
		t.Error("expected true: user@example.com matches example.com")
	}
}

func TestMatchesDomain_Subdomain(t *testing.T) {
	got := keepcel.MatchesDomainFunc("user@sub.example.com", []string{"example.com"})
	if !got {
		t.Error("expected true: sub.example.com is a subdomain of example.com")
	}
}

func TestMatchesDomain_CaseInsensitive(t *testing.T) {
	got := keepcel.MatchesDomainFunc("USER@EXAMPLE.COM", []string{"example.com"})
	if !got {
		t.Error("expected true: USER@EXAMPLE.COM should match example.com case-insensitively")
	}
}

func TestMatchesDomain_NoMatch(t *testing.T) {
	got := keepcel.MatchesDomainFunc("user@other.com", []string{"example.com"})
	if got {
		t.Error("expected false: user@other.com does not match example.com")
	}
}

func TestMatchesDomain_NoAt(t *testing.T) {
	got := keepcel.MatchesDomainFunc("notanemail", []string{"example.com"})
	if got {
		t.Error("expected false: invalid email with no @ should return false")
	}
}

// --- CEL integration tests ---

func TestMatchesDomain_CEL(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "matchesDomain(params.email, ['example.com', 'test.org'])")
	got, err := prog.Eval(map[string]any{"email": "user@example.com"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: user@example.com matches example.com")
	}
}

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
