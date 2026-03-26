package keep_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/majorcontext/keep"
)

// ruleYAML is a helper for tests that need a temp rule file.
func writeRule(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

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

func TestEvaluate_Concurrent(t *testing.T) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defer eng.Close()

	const goroutines = 50
	const iterations = 100

	errc := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < iterations; i++ {
				// Alternate between scopes and operations to exercise contention.
				result, err := eng.Evaluate(keep.Call{
					Operation: "create_issue",
					Params:    map[string]any{"priority": 1, "title": "Concurrent test"},
				}, "linear-tools")
				if err != nil {
					errc <- err
					return
				}
				if result.Decision != keep.Allow {
					errc <- fmt.Errorf("expected Allow, got %s", result.Decision)
					return
				}

				result, err = eng.Evaluate(keep.Call{
					Operation: "delete_issue",
					Params:    map[string]any{"issueId": "ISSUE-1"},
				}, "linear-tools")
				if err != nil {
					errc <- err
					return
				}
				if result.Decision != keep.Deny {
					errc <- fmt.Errorf("expected Deny, got %s", result.Decision)
					return
				}
			}
			errc <- nil
		}()
	}

	for g := 0; g < goroutines; g++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
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
	_ = os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(ruleYAML), 0644)

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

func TestWithMode_Enforce(t *testing.T) {
	dir := writeRule(t, `
scope: test-mode
mode: audit_only
rules:
  - name: block-all
    match:
      operation: "*"
    action: deny
    message: "blocked"
`)
	eng, err := keep.Load(dir, keep.WithMode("enforce"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{Operation: "anything"}, "test-mode")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want deny (enforce override)", result.Decision)
	}
	if !result.Audit.Enforced {
		t.Error("Audit.Enforced = false, want true")
	}
}

func TestWithMode_AuditOnly(t *testing.T) {
	dir := writeRule(t, `
scope: test-mode
mode: enforce
rules:
  - name: block-all
    match:
      operation: "*"
    action: deny
    message: "blocked"
`)
	eng, err := keep.Load(dir, keep.WithMode("audit_only"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(keep.Call{Operation: "anything"}, "test-mode")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("Decision = %q, want allow (audit_only override)", result.Decision)
	}
	if result.Audit.Enforced {
		t.Error("Audit.Enforced = true, want false")
	}
}

func TestWithMode_Invalid(t *testing.T) {
	_, err := keep.Load("testdata/rules", keep.WithMode("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestWithForceEnforce_StillWorks(t *testing.T) {
	dir := writeRule(t, `
scope: test-mode
mode: audit_only
rules:
  - name: block-all
    match:
      operation: "*"
    action: deny
`)
	eng, err := keep.Load(dir, keep.WithForceEnforce())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, _ := eng.Evaluate(keep.Call{Operation: "anything"}, "test-mode")
	if result.Decision != keep.Deny {
		t.Errorf("Decision = %q, want deny (ForceEnforce)", result.Decision)
	}
}

func TestWithAuditHook(t *testing.T) {
	var events []keep.AuditEntry
	eng, err := keep.Load("testdata/rules",
		keep.WithAuditHook(func(entry keep.AuditEntry) {
			events = append(events, entry)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// Allow path.
	eng.Evaluate(keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"priority": 1, "title": "Test"},
	}, "linear-tools")

	// Deny path.
	eng.Evaluate(keep.Call{
		Operation: "delete_issue",
		Params:    map[string]any{"issueId": "X"},
	}, "linear-tools")

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Decision != keep.Allow {
		t.Errorf("events[0].Decision = %q, want allow", events[0].Decision)
	}
	if events[1].Decision != keep.Deny {
		t.Errorf("events[1].Decision = %q, want deny", events[1].Decision)
	}
	if events[1].Rule != "no-delete" {
		t.Errorf("events[1].Rule = %q, want no-delete", events[1].Rule)
	}
}

func TestWithAuditHook_NotCalledOnError(t *testing.T) {
	var called bool
	eng, err := keep.Load("testdata/rules",
		keep.WithAuditHook(func(entry keep.AuditEntry) {
			called = true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// Unknown scope produces an error, hook should not fire.
	_, err = eng.Evaluate(keep.Call{Operation: "anything"}, "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Error("audit hook should not be called on scope-not-found error")
	}
}
