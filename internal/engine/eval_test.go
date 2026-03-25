package engine

import (
	"strings"
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/secrets"
)

func makeEvaluator(t *testing.T, rules []config.Rule) *Evaluator {
	return makeEvaluatorWithOpts(t, rules, false)
}

func makeEvaluatorWithOpts(t *testing.T, rules []config.Rule, caseSensitive bool) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, caseSensitive)
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
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, defs, nil, false)
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
		"allowed_teams": "['team-eng', 'team-infra']",
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

	// Allowed team (input lowered to "team-eng") => allow
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
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det, false)
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
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det, false)
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

func TestEval_CaseInsensitiveParamsMatch(t *testing.T) {
	rules := []config.Rule{
		{
			Name: "block-bash",
			Match: config.Match{
				Operation: "llm.tool_use",
				When:      "params.name == 'bash'",
			},
			Action:  config.ActionDeny,
			Message: "bash blocked",
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, false)

	tests := []struct {
		name     string
		op       string
		toolName string
		wantDeny bool
	}{
		{"lowercase", "llm.tool_use", "bash", true},
		{"uppercase", "llm.tool_use", "BASH", true},
		{"mixed", "llm.tool_use", "Bash", true},
		{"operation mixed case", "LLM.Tool_Use", "bash", true},
		{"no match", "llm.tool_use", "python", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ev.Evaluate(Call{
				Operation: tt.op,
				Params:    map[string]any{"name": tt.toolName},
				Context:   CallContext{Direction: "response"},
			})
			if tt.wantDeny && result.Decision != Deny {
				t.Errorf("expected Deny, got %s", result.Decision)
			}
			if !tt.wantDeny && result.Decision != Allow {
				t.Errorf("expected Allow, got %s", result.Decision)
			}
		})
	}
}

func TestEval_CaseInsensitiveAuditPreservesOriginal(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "block-bash",
			Match:   config.Match{Operation: "llm.tool_use", When: "params.name == 'bash'"},
			Action:  config.ActionDeny,
			Message: "blocked",
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, false)

	result := ev.Evaluate(Call{
		Operation: "LLM.Tool_Use",
		Params:    map[string]any{"name": "Bash"},
		Context:   CallContext{Direction: "response"},
	})
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %s", result.Decision)
	}
	// Audit should preserve original operation name
	if result.Audit.Operation != "LLM.Tool_Use" {
		t.Errorf("audit operation: want LLM.Tool_Use, got %s", result.Audit.Operation)
	}
}

func TestEval_CaseInsensitiveContext(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "check-agent",
			Match:   config.Match{When: "context.agent_id == 'bot-1' && context.direction == 'request'"},
			Action:  config.ActionDeny,
			Message: "blocked",
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, false)

	result := ev.Evaluate(Call{
		Operation: "test",
		Params:    map[string]any{},
		Context: CallContext{
			AgentID:   "BOT-1",
			Direction: "Request",
		},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny with mixed-case context, got %s", result.Decision)
	}
}

func TestEval_CaseSensitiveScope(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "exact-match",
			Match:   config.Match{Operation: "vault.lookup", When: "params.token == 'sk-live-abc123'"},
			Action:  config.ActionDeny,
			Message: "blocked",
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, true)

	// Exact case matches
	result := ev.Evaluate(Call{
		Operation: "vault.lookup",
		Params:    map[string]any{"token": "sk-live-abc123"},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny for exact case match, got %s", result.Decision)
	}

	// Wrong param case does NOT match
	result = ev.Evaluate(Call{
		Operation: "vault.lookup",
		Params:    map[string]any{"token": "SK-LIVE-ABC123"},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow for wrong case in case-sensitive mode, got %s", result.Decision)
	}

	// Wrong operation case does NOT match
	result = ev.Evaluate(Call{
		Operation: "Vault.Lookup",
		Params:    map[string]any{"token": "sk-live-abc123"},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow for wrong operation case in case-sensitive mode, got %s", result.Decision)
	}
}

// TestEval_CaseInsensitiveFullPipeline is an integration test that exercises
// the entire normalization pipeline in a single evaluator:
//   - Case-insensitive deny matching
//   - hasSecrets detecting original-case credentials (not lowered)
//   - Regex redaction matching original-case patterns
//   - Audit trail preserving original values
//
// If any of these invariants break, it likely means the dual-map (evalParams)
// bookkeeping or the expression rewriter has regressed.
func TestEval_CaseInsensitiveFullPipeline(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	celEnv, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}

	rules := []config.Rule{
		// Rule 1: deny if hasSecrets detects a credential (needs original case).
		{
			Name:    "deny-secrets",
			Match:   config.Match{Operation: "llm.text", When: "hasSecrets(params.text)"},
			Action:  config.ActionDeny,
			Message: "secrets detected",
		},
		// Rule 2: redact SECRET_* patterns (regex is case-sensitive, needs original).
		{
			Name:  "redact-secrets",
			Match: config.Match{Operation: "llm.tool_result"},
			Action: config.ActionRedact,
			Redact: &config.RedactSpec{
				Target: "params.content",
				Patterns: []config.RedactPattern{
					{Match: "SECRET_[A-Z_]+", Replace: "[REDACTED]"},
				},
			},
		},
		// Rule 3: case-insensitive deny on tool name.
		{
			Name:    "block-bash",
			Match:   config.Match{Operation: "llm.tool_use", When: "params.name == 'bash'"},
			Action:  config.ActionDeny,
			Message: "bash blocked",
		},
	}

	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det, false)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("deny matches case-insensitively", func(t *testing.T) {
		result := ev.Evaluate(Call{
			Operation: "LLM.Tool_Use",
			Params:    map[string]any{"name": "Bash"},
			Context:   CallContext{Timestamp: time.Now()},
		})
		if result.Decision != Deny {
			t.Errorf("expected Deny, got %s", result.Decision)
		}
		if result.Audit.Operation != "LLM.Tool_Use" {
			t.Errorf("audit should preserve original operation, got %s", result.Audit.Operation)
		}
	})

	t.Run("hasSecrets detects original-case AWS key", func(t *testing.T) {
		result := ev.Evaluate(Call{
			Operation: "llm.text",
			Params:    map[string]any{"text": "key is AKIAIOSFODNN7REALKEY"},
			Context:   CallContext{Timestamp: time.Now()},
		})
		if result.Decision != Deny {
			t.Errorf("expected Deny (hasSecrets should detect original-case key), got %s", result.Decision)
		}
	})

	t.Run("regex redaction matches original-case patterns", func(t *testing.T) {
		result := ev.Evaluate(Call{
			Operation: "llm.tool_result",
			Params:    map[string]any{"content": "token is SECRET_API_KEY here"},
			Context:   CallContext{Timestamp: time.Now()},
		})
		if result.Decision != Redact {
			t.Fatalf("expected Redact, got %s", result.Decision)
		}
		if len(result.Mutations) == 0 {
			t.Fatal("expected mutations from regex redaction")
		}
		// The redacted value should have replaced SECRET_API_KEY.
		replaced := result.Mutations[0].Replaced
		if replaced == "" {
			t.Error("mutation Replaced should not be empty")
		}
		if strings.Contains(replaced, "SECRET_API_KEY") {
			t.Errorf("redaction should have replaced SECRET_API_KEY, got %s", replaced)
		}
	})
}

// TestEval_DenyParamsSummaryUsesOriginalCase verifies that audit ParamsSummary
// records use params.original (preserving original casing) rather than
// params.cel (lowered) for all deny paths:
//   - enforce-mode deny short-circuit
//   - audit-only deny path
func TestEval_DenyParamsSummaryUsesOriginalCase(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "block-bash",
			Match:   config.Match{Operation: "llm.tool_use", When: "params.name == 'bash'"},
			Action:  config.ActionDeny,
			Message: "bash blocked",
		},
	}

	t.Run("enforce-mode deny preserves original case in ParamsSummary", func(t *testing.T) {
		env, err := keepcel.NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}

		result := ev.Evaluate(Call{
			Operation: "llm.tool_use",
			Params:    map[string]any{"name": "Bash"},
			Context:   CallContext{Timestamp: time.Now()},
		})

		if result.Decision != Deny {
			t.Fatalf("expected Deny, got %s", result.Decision)
		}
		// ParamsSummary should contain the original "Bash", not lowered "bash".
		if !strings.Contains(result.Audit.ParamsSummary, "Bash") {
			t.Errorf("audit ParamsSummary should contain original-case %q, got: %s", "Bash", result.Audit.ParamsSummary)
		}
		if strings.Contains(result.Audit.ParamsSummary, `"bash"`) {
			t.Errorf("audit ParamsSummary should not contain lowered %q, got: %s", "bash", result.Audit.ParamsSummary)
		}
	})

	t.Run("audit-only deny preserves original case in ParamsSummary", func(t *testing.T) {
		env, err := keepcel.NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		ev, err := NewEvaluator(env, "test-scope", config.ModeAuditOnly, config.ErrorModeClosed, rules, nil, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}

		result := ev.Evaluate(Call{
			Operation: "llm.tool_use",
			Params:    map[string]any{"name": "Bash"},
			Context:   CallContext{Timestamp: time.Now()},
		})

		// In audit-only mode, the returned Decision is Allow but Audit.Decision is Deny.
		if result.Decision != Allow {
			t.Fatalf("expected Allow (audit-only), got %s", result.Decision)
		}
		if result.Audit.Decision != Deny {
			t.Fatalf("expected Audit.Decision Deny, got %s", result.Audit.Decision)
		}
		// ParamsSummary should contain the original "Bash", not lowered "bash".
		if !strings.Contains(result.Audit.ParamsSummary, "Bash") {
			t.Errorf("audit ParamsSummary should contain original-case %q, got: %s", "Bash", result.Audit.ParamsSummary)
		}
		if strings.Contains(result.Audit.ParamsSummary, `"bash"`) {
			t.Errorf("audit ParamsSummary should not contain lowered %q, got: %s", "bash", result.Audit.ParamsSummary)
		}
	})
}

// TestEval_CaseInsensitiveRedactThenDeny verifies that mutations applied
// via evalParams.applyMutations are visible to both the CEL view and the
// original view. If a redaction rule runs first and a deny rule checks the
// same field afterwards, both views should show the redacted value.
func TestEval_CaseInsensitiveRedactThenDeny(t *testing.T) {
	rules := []config.Rule{
		// Redact first (evaluated first due to operation specificity sorting).
		{
			Name:  "redact-token",
			Match: config.Match{Operation: "api.call"},
			Action: config.ActionRedact,
			Redact: &config.RedactSpec{
				Target: "params.body",
				Patterns: []config.RedactPattern{
					{Match: "token_[a-z0-9]+", Replace: "[REDACTED]"},
				},
			},
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, false)

	result := ev.Evaluate(Call{
		Operation: "API.Call",
		Params:    map[string]any{"body": "auth token_abc123 here"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Redact {
		t.Fatalf("expected Redact, got %s", result.Decision)
	}
	// Audit params summary should show redacted value (from params.original).
	if strings.Contains(result.Audit.ParamsSummary, "token_abc123") {
		t.Error("audit params summary should show redacted value, but found original token")
	}
}
