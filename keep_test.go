package keep_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/majorcontext/keep"
)

func TestLoad_ValidRules(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	scopes := eng.Scopes()
	want := []string{"anthropic-gateway", "linear-tools"}
	if len(scopes) != len(want) {
		t.Fatalf("Scopes() = %v, want %v", scopes, want)
	}
	for i, s := range scopes {
		if s != want[i] {
			t.Errorf("Scopes()[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestLoad_InvalidRules(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(`
scope: bad
mode: enforce
rules:
  - name: bad-rule
    match:
      operation: "foo"
      when: "this is not valid CEL %%% {{{"
    action: deny
    message: "should fail"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = keep.Load(dir)
	if err == nil {
		t.Fatal("Load() expected error for invalid CEL, got nil")
	}
}

func TestLoad_WithOptions(t *testing.T) {
	eng, err := keep.Load("testdata/rules",
		keep.WithProfilesDir("testdata/profiles"),
		keep.WithPacksDir("testdata/packs"),
	)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()
	if len(eng.Scopes()) == 0 {
		t.Error("expected at least one scope")
	}
}

func TestEngine_Scopes(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	scopes := eng.Scopes()
	// Must be sorted.
	for i := 1; i < len(scopes); i++ {
		if scopes[i] < scopes[i-1] {
			t.Errorf("Scopes() not sorted: %v", scopes)
			break
		}
	}
}

func TestEvaluate_Allow(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"priority": 1, "title": "Test issue"},
	}, "linear-tools")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("Decision = %q, want %q", result.Decision, keep.Allow)
	}
}

func TestEvaluate_Deny(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{
		Operation: "delete_issue",
		Params:    map[string]any{"issueId": "ISSUE-123"},
	}, "linear-tools")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, keep.Deny)
	}
	if result.Rule != "no-delete" {
		t.Errorf("Rule = %q, want %q", result.Rule, "no-delete")
	}
}

func TestEvaluate_DenyWhen(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"priority": 0, "title": "Outage"},
	}, "linear-tools")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, keep.Deny)
	}
	if result.Rule != "no-auto-p0" {
		t.Errorf("Rule = %q, want %q", result.Rule, "no-auto-p0")
	}
}

func TestEvaluate_Redact(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{
		Operation: "llm.tool_result",
		Params:    map[string]any{"content": "key is AKIAIOSFODNN7EXAMPLE"},
	}, "anthropic-gateway")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Redact {
		t.Errorf("Decision = %q, want %q", result.Decision, keep.Redact)
	}
	if len(result.Mutations) == 0 {
		t.Fatal("expected at least one mutation")
	}
	m := result.Mutations[0]
	if m.Path != "params.content" {
		t.Errorf("Mutation.Path = %q, want %q", m.Path, "params.content")
	}
	if m.Replaced != "key is [REDACTED:AWS_KEY]" {
		t.Errorf("Mutation.Replaced = %q, want %q", m.Replaced, "key is [REDACTED:AWS_KEY]")
	}
}

func TestEvaluate_UnknownScope(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	_, err = eng.Evaluate(keep.Call{
		Operation: "anything",
	}, "nonexistent-scope")
	if err == nil {
		t.Fatal("Evaluate() expected error for unknown scope, got nil")
	}
}

func TestApplyMutations(t *testing.T) {
	params := map[string]any{
		"content": "key is AKIAIOSFODNN7EXAMPLE",
	}
	mutations := []keep.Mutation{
		{
			Path:     "params.content",
			Original: "key is AKIAIOSFODNN7EXAMPLE",
			Replaced: "key is [REDACTED:AWS_KEY]",
		},
	}

	result := keep.ApplyMutations(params, mutations)
	if result["content"] != "key is [REDACTED:AWS_KEY]" {
		t.Errorf("ApplyMutations content = %q, want %q", result["content"], "key is [REDACTED:AWS_KEY]")
	}
	// Original must be unchanged.
	if params["content"] != "key is AKIAIOSFODNN7EXAMPLE" {
		t.Error("ApplyMutations modified original params")
	}
}

func TestLoad_WithDefs(t *testing.T) {
	eng, err := keep.Load("testdata/rules-with-defs")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	scopes := eng.Scopes()
	if len(scopes) != 1 || scopes[0] != "test-defs" {
		t.Fatalf("Scopes() = %v, want [test-defs]", scopes)
	}

	// Allowed team => allow
	result, err := eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"team": "TEAM-ENG", "priority": int64(1)},
	}, "test-defs")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("Decision = %q, want %q for allowed team", result.Decision, keep.Allow)
	}

	// Disallowed team => deny
	result, err = eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"team": "TEAM-SALES", "priority": int64(1)},
	}, "test-defs")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q for disallowed team", result.Decision, keep.Deny)
	}
	if result.Rule != "team-check" {
		t.Errorf("Rule = %q, want %q", result.Rule, "team-check")
	}

	// Priority too high => deny
	result, err = eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"team": "TEAM-ENG", "priority": int64(5)},
	}, "test-defs")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q for high priority", result.Decision, keep.Deny)
	}
	if result.Rule != "priority-check" {
		t.Errorf("Rule = %q, want %q", result.Rule, "priority-check")
	}

	// Agent branch prefix => deny
	result, err = eng.Evaluate(keep.Call{
		Operation: "push",
		Params:    map[string]any{"branch": "agent/fix-123"},
	}, "test-defs")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q for agent branch", result.Decision, keep.Deny)
	}

	// Non-agent branch => allow
	result, err = eng.Evaluate(keep.Call{
		Operation: "push",
		Params:    map[string]any{"branch": "main"},
	}, "test-defs")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("Decision = %q, want %q for main branch", result.Decision, keep.Allow)
	}
}

func TestLoad_SecretsRule(t *testing.T) {
	dir := t.TempDir()
	ruleYAML := `
scope: test
mode: enforce
rules:
  - name: redact-secrets
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true
`
	os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(ruleYAML), 0644)

	eng, err := keep.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7REALKEY"},
		Context:   keep.CallContext{Timestamp: time.Now(), Scope: "test"},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
}

func TestReload(t *testing.T) {
	// Copy existing rules to a temp dir.
	dir := t.TempDir()
	data, err := os.ReadFile("testdata/rules/linear.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "linear.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	eng, err := keep.Load(dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	if len(eng.Scopes()) != 1 {
		t.Fatalf("Scopes() = %v, want 1 scope", eng.Scopes())
	}

	// Add a new rule file.
	newRule := []byte(`
scope: new-scope
mode: enforce
rules:
  - name: block-all
    match:
      operation: "*"
    action: deny
    message: "blocked"
`)
	if err := os.WriteFile(filepath.Join(dir, "new.yaml"), newRule, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := eng.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	scopes := eng.Scopes()
	if len(scopes) != 2 {
		t.Fatalf("after Reload, Scopes() = %v, want 2 scopes", scopes)
	}

	// Verify the new scope works.
	result, err := eng.Evaluate(keep.Call{
		Operation: "anything",
	}, "new-scope")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, keep.Deny)
	}
}
