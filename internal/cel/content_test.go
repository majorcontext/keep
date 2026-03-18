package cel_test

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
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

func TestContainsPII_SSN(t *testing.T) {
	got := keepcel.ContainsPIIFunc("my ssn is 123-45-6789")
	if !got {
		t.Error("expected true: SSN pattern 123-45-6789 should match")
	}
}

func TestContainsPII_CreditCard(t *testing.T) {
	got := keepcel.ContainsPIIFunc("card 4111111111111111")
	if !got {
		t.Error("expected true: Visa credit card number should match")
	}
}

func TestContainsPII_Clean(t *testing.T) {
	got := keepcel.ContainsPIIFunc("nothing sensitive here")
	if got {
		t.Error("expected false: no PII patterns in clean text")
	}
}

func TestContainsPHI_Stub(t *testing.T) {
	got := keepcel.ContainsPHIFunc("anything")
	if got {
		t.Error("expected false: containsPHI is a stub that always returns false")
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

func TestContainsPII_CEL(t *testing.T) {
	env := mustNewEnv(t)
	prog := mustCompile(t, env, "containsPII(params.content)")
	got, err := prog.Eval(map[string]any{"content": "ssn 123-45-6789"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: SSN in content should trigger containsPII")
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
