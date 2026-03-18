package redact_test

import (
	"testing"

	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/redact"
)

// helper: compile patterns or fail
func mustCompile(t *testing.T, patterns []config.RedactPattern) []redact.CompiledPattern {
	t.Helper()
	compiled, err := redact.CompilePatterns(patterns)
	if err != nil {
		t.Fatalf("CompilePatterns: %v", err)
	}
	return compiled
}

// TestRedact_SinglePattern verifies that a single AWS key pattern replaces the match.
func TestRedact_SinglePattern(t *testing.T) {
	patterns := mustCompile(t, []config.RedactPattern{
		{Match: `AKIA[0-9A-Z]{16}`, Replace: "[REDACTED:AWS_KEY]"},
	})

	params := map[string]any{
		"content": "my key is AKIAIOSFODNN7EXAMPLE and nothing else",
	}

	mutations := redact.Apply(params, "params.content", patterns)

	if len(mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(mutations))
	}
	m := mutations[0]
	if m.Path != "params.content" {
		t.Errorf("Path: got %q, want %q", m.Path, "params.content")
	}
	if m.Original != "my key is AKIAIOSFODNN7EXAMPLE and nothing else" {
		t.Errorf("Original: got %q", m.Original)
	}
	want := "my key is [REDACTED:AWS_KEY] and nothing else"
	if m.Replaced != want {
		t.Errorf("Replaced: got %q, want %q", m.Replaced, want)
	}
}

// TestRedact_MultiplePatterns verifies that two patterns are applied sequentially.
func TestRedact_MultiplePatterns(t *testing.T) {
	patterns := mustCompile(t, []config.RedactPattern{
		{Match: `AKIA[0-9A-Z]{16}`, Replace: "[REDACTED:AWS_KEY]"},
		{Match: `secret-\w+`, Replace: "[REDACTED:SECRET]"},
	})

	params := map[string]any{
		"content": "key=AKIAIOSFODNN7EXAMPLE token=secret-abc123",
	}

	mutations := redact.Apply(params, "params.content", patterns)

	if len(mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(mutations))
	}
	want := "key=[REDACTED:AWS_KEY] token=[REDACTED:SECRET]"
	if mutations[0].Replaced != want {
		t.Errorf("Replaced: got %q, want %q", mutations[0].Replaced, want)
	}
}

// TestRedact_NoMatch verifies that when no pattern matches, no mutations are returned.
func TestRedact_NoMatch(t *testing.T) {
	patterns := mustCompile(t, []config.RedactPattern{
		{Match: `AKIA[0-9A-Z]{16}`, Replace: "[REDACTED:AWS_KEY]"},
	})

	params := map[string]any{
		"content": "nothing sensitive here",
	}

	mutations := redact.Apply(params, "params.content", patterns)

	if len(mutations) != 0 {
		t.Errorf("expected 0 mutations, got %d", len(mutations))
	}
}

// TestRedact_FieldPath verifies that the mutation carries the correct path.
func TestRedact_FieldPath(t *testing.T) {
	patterns := mustCompile(t, []config.RedactPattern{
		{Match: `AKIA[0-9A-Z]{16}`, Replace: "[REDACTED:AWS_KEY]"},
	})

	params := map[string]any{
		"content": "AKIAIOSFODNN7EXAMPLE",
	}

	mutations := redact.Apply(params, "params.content", patterns)

	if len(mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(mutations))
	}
	if mutations[0].Path != "params.content" {
		t.Errorf("Path: got %q, want %q", mutations[0].Path, "params.content")
	}
}

// TestApplyMutations_Simple verifies that a single mutation is applied to the map.
func TestApplyMutations_Simple(t *testing.T) {
	params := map[string]any{
		"content": "my key is AKIAIOSFODNN7EXAMPLE",
	}
	mutations := []redact.Mutation{
		{Path: "params.content", Original: "my key is AKIAIOSFODNN7EXAMPLE", Replaced: "my key is [REDACTED:AWS_KEY]"},
	}

	result := redact.ApplyMutations(params, mutations)

	got, ok := result["content"]
	if !ok {
		t.Fatal("content key missing from result")
	}
	if got != "my key is [REDACTED:AWS_KEY]" {
		t.Errorf("content: got %q", got)
	}
}

// TestApplyMutations_NestedPath verifies that mutations are applied to nested map paths.
func TestApplyMutations_NestedPath(t *testing.T) {
	params := map[string]any{
		"input": map[string]any{
			"command": "run AKIAIOSFODNN7EXAMPLE",
		},
	}
	mutations := []redact.Mutation{
		{Path: "params.input.command", Original: "run AKIAIOSFODNN7EXAMPLE", Replaced: "run [REDACTED:AWS_KEY]"},
	}

	result := redact.ApplyMutations(params, mutations)

	inputMap, ok := result["input"].(map[string]any)
	if !ok {
		t.Fatal("input key missing or not a map")
	}
	got, ok := inputMap["command"]
	if !ok {
		t.Fatal("command key missing from nested map")
	}
	if got != "run [REDACTED:AWS_KEY]" {
		t.Errorf("command: got %q", got)
	}
}

// TestApplyMutations_MissingPath verifies that a mutation targeting a non-existent path is skipped.
func TestApplyMutations_MissingPath(t *testing.T) {
	params := map[string]any{
		"content": "hello",
	}
	mutations := []redact.Mutation{
		{Path: "params.nonexistent", Original: "x", Replaced: "y"},
	}

	// Should not panic, result should have content unchanged.
	result := redact.ApplyMutations(params, mutations)

	got, ok := result["content"]
	if !ok {
		t.Fatal("content key missing")
	}
	if got != "hello" {
		t.Errorf("content changed unexpectedly: got %q", got)
	}
	if _, exists := result["nonexistent"]; exists {
		t.Error("nonexistent key should not appear in result")
	}
}

// TestApplyMutations_OriginalUnmodified verifies that the original map is not changed.
func TestApplyMutations_OriginalUnmodified(t *testing.T) {
	params := map[string]any{
		"content": "my key is AKIAIOSFODNN7EXAMPLE",
	}
	mutations := []redact.Mutation{
		{Path: "params.content", Original: "my key is AKIAIOSFODNN7EXAMPLE", Replaced: "my key is [REDACTED:AWS_KEY]"},
	}

	_ = redact.ApplyMutations(params, mutations)

	if params["content"] != "my key is AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("original params was modified: got %q", params["content"])
	}
}
