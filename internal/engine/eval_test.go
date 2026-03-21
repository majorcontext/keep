package engine

import (
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/secrets"
)

func makeEvaluator(t *testing.T, rules []config.Rule) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func makeCall(operation string, params map[string]any) Call {
	return Call{
		Operation: operation,
		Params:    params,
		Context: CallContext{
			AgentID:   "agent-1",
			UserID:    "user-1",
			Timestamp: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			Scope:     "test-scope",
			Direction: "inbound",
			Labels:    map[string]string{"env": "test"},
		},
	}
}

func TestEval_AllowNoRules(t *testing.T) {
	ev := makeEvaluator(t, nil)
	result := ev.Evaluate(makeCall("anything", nil))
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
}

func TestEval_DenyMatchesOperation(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "block-deletes",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "delete_*"},
			Message: "deletes are not allowed",
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("delete_issue", nil))
	if result.Decision != Deny {
		t.Errorf("expected Deny, got %s", result.Decision)
	}
	if result.Rule != "block-deletes" {
		t.Errorf("expected rule block-deletes, got %s", result.Rule)
	}
}

func TestEval_DenyMatchesWhen(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "block-low-priority",
			Action:  config.ActionDeny,
			Match:   config.Match{When: "params.priority == 0"},
			Message: "low priority denied",
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("create_issue", map[string]any{"priority": int64(0)}))
	if result.Decision != Deny {
		t.Errorf("expected Deny, got %s", result.Decision)
	}
	if result.Rule != "block-low-priority" {
		t.Errorf("expected rule block-low-priority, got %s", result.Rule)
	}
	if result.Message != "low priority denied" {
		t.Errorf("expected message 'low priority denied', got %q", result.Message)
	}
}

func TestEval_DenyShortCircuit(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "first-deny",
			Action: config.ActionDeny,
			Match:  config.Match{Operation: "delete_*"},
		},
		{
			Name:   "second-deny",
			Action: config.ActionDeny,
			Match:  config.Match{Operation: "delete_*"},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("delete_issue", nil))
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %s", result.Decision)
	}
	evaluated := result.Audit.RulesEvaluated
	if len(evaluated) != 1 {
		t.Fatalf("expected 1 rule evaluated, got %d", len(evaluated))
	}
	if !evaluated[0].Matched {
		t.Error("expected first rule to be matched")
	}
	if evaluated[0].Name != "first-deny" {
		t.Errorf("expected first-deny, got %s", evaluated[0].Name)
	}
}

func TestEval_Log(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "log-everything",
			Action: config.ActionLog,
			Match:  config.Match{Operation: "*"},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("create_issue", nil))
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
	evaluated := result.Audit.RulesEvaluated
	if len(evaluated) != 1 {
		t.Fatalf("expected 1 rule evaluated, got %d", len(evaluated))
	}
	if !evaluated[0].Matched {
		t.Error("expected log rule to be matched")
	}
	if evaluated[0].Action != "log" {
		t.Errorf("expected action log, got %s", evaluated[0].Action)
	}
}

func TestEval_Redact(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "redact-aws",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "*"},
			Redact: &config.RedactSpec{
				Target: "params.body",
				Patterns: []config.RedactPattern{
					{Match: `AKIA[0-9A-Z]{16}`, Replace: "***AWS_KEY***"},
				},
			},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("send_message", map[string]any{
		"body": "my key is AKIAIOSFODNN7EXAMPLE ok",
	}))
	if result.Decision != Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
	if len(result.Mutations) == 0 {
		t.Fatal("expected mutations, got none")
	}
	if result.Mutations[0].Path != "params.body" {
		t.Errorf("expected mutation path params.body, got %s", result.Mutations[0].Path)
	}
}

func TestEval_RedactAccumulates(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "redact-aws",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "*"},
			Redact: &config.RedactSpec{
				Target: "params.body",
				Patterns: []config.RedactPattern{
					{Match: `AKIA[0-9A-Z]{16}`, Replace: "***AWS_KEY***"},
				},
			},
		},
		{
			Name:   "redact-ssn",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "*"},
			Redact: &config.RedactSpec{
				Target: "params.notes",
				Patterns: []config.RedactPattern{
					{Match: `\d{3}-\d{2}-\d{4}`, Replace: "***SSN***"},
				},
			},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("send_message", map[string]any{
		"body":  "my key is AKIAIOSFODNN7EXAMPLE ok",
		"notes": "SSN is 123-45-6789",
	}))
	if result.Decision != Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
	if len(result.Mutations) != 2 {
		t.Fatalf("expected 2 mutations, got %d", len(result.Mutations))
	}
}

func TestEval_OperationMismatch(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "create-only",
			Action: config.ActionDeny,
			Match:  config.Match{Operation: "create_*"},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("delete_issue", nil))
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
	evaluated := result.Audit.RulesEvaluated
	if len(evaluated) != 1 {
		t.Fatalf("expected 1 rule evaluated, got %d", len(evaluated))
	}
	if !evaluated[0].Skipped {
		t.Error("expected rule to be skipped")
	}
}

func TestEval_WhenFalse(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "block-low-priority",
			Action: config.ActionDeny,
			Match:  config.Match{When: "params.priority == 0"},
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("create_issue", map[string]any{"priority": int64(1)}))
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
	evaluated := result.Audit.RulesEvaluated
	if len(evaluated) != 1 {
		t.Fatalf("expected 1 rule evaluated, got %d", len(evaluated))
	}
	if evaluated[0].Matched {
		t.Error("expected rule to not be matched")
	}
}

func TestEval_AuditAlwaysPopulated(t *testing.T) {
	tests := []struct {
		name  string
		rules []config.Rule
	}{
		{"no-rules", nil},
		{"deny-rule", []config.Rule{
			{Name: "deny-all", Action: config.ActionDeny, Match: config.Match{Operation: "*"}},
		}},
		{"log-rule", []config.Rule{
			{Name: "log-all", Action: config.ActionLog, Match: config.Match{Operation: "*"}},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := makeEvaluator(t, tt.rules)
			result := ev.Evaluate(makeCall("test_op", nil))
			audit := result.Audit
			if audit.Timestamp.IsZero() {
				t.Error("expected non-zero timestamp")
			}
			if audit.Scope != "test-scope" {
				t.Errorf("expected scope test-scope, got %s", audit.Scope)
			}
			if audit.Operation != "test_op" {
				t.Errorf("expected operation test_op, got %s", audit.Operation)
			}
			if audit.AgentID != "agent-1" {
				t.Errorf("expected agent_id agent-1, got %s", audit.AgentID)
			}
			if audit.UserID != "user-1" {
				t.Errorf("expected user_id user-1, got %s", audit.UserID)
			}
			if audit.Direction != "inbound" {
				t.Errorf("expected direction inbound, got %s", audit.Direction)
			}
			if audit.Decision == "" {
				t.Error("expected non-empty decision")
			}
			if audit.ParamsSummary == "" {
				t.Error("expected non-empty params summary")
			}
		})
	}
}

func TestEval_SpecificityOrder(t *testing.T) {
	// Broad glob rule comes first in file, exact rule comes second.
	// The exact rule should fire first due to specificity ordering.
	rules := []config.Rule{
		{
			Name:    "broad-deny",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "*"},
			Message: "broad deny",
		},
		{
			Name:    "exact-deny",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "delete_issue"},
			Message: "exact deny",
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("delete_issue", nil))
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %s", result.Decision)
	}
	if result.Rule != "exact-deny" {
		t.Errorf("expected exact-deny to fire first, got %s", result.Rule)
	}
}

func TestEval_SpecificityPreservesFileOrder(t *testing.T) {
	// Two exact rules at the same specificity tier.
	// The first one in the file should fire first (stable sort).
	rules := []config.Rule{
		{
			Name:    "first-exact",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "delete_issue"},
			Message: "first",
		},
		{
			Name:    "second-exact",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "delete_issue"},
			Message: "second",
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("delete_issue", nil))
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %s", result.Decision)
	}
	if result.Rule != "first-exact" {
		t.Errorf("expected first-exact to fire first (stable sort), got %s", result.Rule)
	}
}

func TestEval_SpecificityGlobBeforeCatchAll(t *testing.T) {
	// Catch-all rule (no operation) comes first in file, glob rule comes second.
	// The glob rule should fire first due to specificity ordering.
	rules := []config.Rule{
		{
			Name:    "catch-all",
			Action:  config.ActionDeny,
			Match:   config.Match{},
			Message: "catch-all deny",
		},
		{
			Name:    "glob-deny",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "create_*"},
			Message: "glob deny",
		},
	}
	ev := makeEvaluator(t, rules)
	result := ev.Evaluate(makeCall("create_issue", nil))
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %s", result.Decision)
	}
	if result.Rule != "glob-deny" {
		t.Errorf("expected glob-deny to fire before catch-all, got %s", result.Rule)
	}
}

func makeEvaluatorWithDefs(t *testing.T, rules []config.Rule, defs map[string]string) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, defs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestEval_WithDefs(t *testing.T) {
	defs := map[string]string{
		"max_items": "5",
	}
	rules := []config.Rule{
		{
			Name:    "too-many-items",
			Action:  config.ActionDeny,
			Match:   config.Match{When: "size(params.items) > max_items"},
			Message: "too many items",
		},
	}
	ev := makeEvaluatorWithDefs(t, rules, defs)

	// 6 items > 5 => deny
	result := ev.Evaluate(makeCall("create", map[string]any{
		"items": []any{"a", "b", "c", "d", "e", "f"},
	}))
	if result.Decision != Deny {
		t.Errorf("expected Deny for 6 items, got %s", result.Decision)
	}

	// 3 items <= 5 => allow
	result = ev.Evaluate(makeCall("create", map[string]any{
		"items": []any{"a", "b", "c"},
	}))
	if result.Decision != Allow {
		t.Errorf("expected Allow for 3 items, got %s", result.Decision)
	}
}

func TestEval_DefsListLiteral(t *testing.T) {
	defs := map[string]string{
		"allowed_teams": "['TEAM-ENG', 'TEAM-INFRA']",
	}
	rules := []config.Rule{
		{
			Name:    "team-check",
			Action:  config.ActionDeny,
			Match:   config.Match{When: "!(params.team in allowed_teams)"},
			Message: "team not allowed",
		},
	}
	ev := makeEvaluatorWithDefs(t, rules, defs)

	// Allowed team => allow
	result := ev.Evaluate(makeCall("create", map[string]any{"team": "TEAM-ENG"}))
	if result.Decision != Allow {
		t.Errorf("expected Allow for TEAM-ENG, got %s", result.Decision)
	}

	// Disallowed team => deny
	result = ev.Evaluate(makeCall("create", map[string]any{"team": "TEAM-SALES"}))
	if result.Decision != Deny {
		t.Errorf("expected Deny for TEAM-SALES, got %s", result.Decision)
	}
}

func TestEval_DefsStringLiteral(t *testing.T) {
	defs := map[string]string{
		"agent_prefix": "'agent/'",
	}
	rules := []config.Rule{
		{
			Name:    "agent-branch",
			Action:  config.ActionDeny,
			Match:   config.Match{When: "params.branch.startsWith(agent_prefix)"},
			Message: "agent branches not allowed",
		},
	}
	ev := makeEvaluatorWithDefs(t, rules, defs)

	result := ev.Evaluate(makeCall("push", map[string]any{"branch": "agent/fix-123"}))
	if result.Decision != Deny {
		t.Errorf("expected Deny for agent/ branch, got %s", result.Decision)
	}

	result = ev.Evaluate(makeCall("push", map[string]any{"branch": "main"}))
	if result.Decision != Allow {
		t.Errorf("expected Allow for main branch, got %s", result.Decision)
	}
}

func TestEval_RedactSecrets(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	celEnv, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "redact-secrets",
			Match:  config.Match{Operation: "llm.text"},
			Action: config.ActionRedact,
			Redact: &config.RedactSpec{
				Target:  "params.text",
				Secrets: true,
			},
		},
	}
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det)
	if err != nil {
		t.Fatal(err)
	}
	result := ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7REALKEY"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
	if len(result.Mutations) == 0 {
		t.Error("expected mutations")
	}
}

func TestEval_HasSecretsInWhen(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	celEnv, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:    "deny-secrets",
			Match:   config.Match{Operation: "llm.text", When: "hasSecrets(params.text)"},
			Action:  config.ActionDeny,
			Message: "secrets detected",
		},
	}
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det)
	if err != nil {
		t.Fatal(err)
	}

	// Should deny when text contains a secret.
	result := ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7REALKEY"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny, got %s", result.Decision)
	}

	// Should allow when text is clean.
	result = ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "nothing secret here"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
}
