package engine

import (
	"strings"
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

// makeEvaluatorWithMode creates an evaluator with explicit mode and onError settings.
func makeEvaluatorWithMode(t *testing.T, mode config.Mode, onError config.ErrorMode, rules []config.Rule) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", mode, onError, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// celErrorCall returns a call where params.items is a string (not a list),
// so the expression "params.items.all(x, x > 0)" will fail at eval time.
func celErrorCall() Call {
	return Call{
		Operation: "test_op",
		Params: map[string]any{
			// items is a string, not a list — causes eval error when .all() is called on it
			"items": "not_a_list",
		},
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

// TestEval_CELError_FailClosed verifies that when a CEL expression errors at eval time
// and onError is ErrorModeClosed, the result is Deny with the error in the message.
func TestEval_CELError_FailClosed(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "check-items",
			Action: config.ActionDeny,
			// params.items is a string, not a list — .all() on a string fails at eval time
			Match:   config.Match{When: "params.items.all(x, x > 0)"},
			Message: "items check failed",
		},
	}
	ev := makeEvaluatorWithMode(t, config.ModeEnforce, config.ErrorModeClosed, rules)
	result := ev.Evaluate(celErrorCall())

	if result.Decision != Deny {
		t.Errorf("expected Deny (fail-closed), got %s", result.Decision)
	}
	if !strings.Contains(result.Message, "check-items") {
		t.Errorf("expected message to contain rule name 'check-items', got %q", result.Message)
	}
	if !strings.Contains(result.Message, "fail-closed") {
		t.Errorf("expected message to contain 'fail-closed', got %q", result.Message)
	}
}

// TestEval_CELError_FailOpen verifies that when a CEL expression errors at eval time
// and onError is ErrorModeOpen, the result is Allow and the error is recorded in the audit.
func TestEval_CELError_FailOpen(t *testing.T) {
	rules := []config.Rule{
		{
			Name:   "check-items",
			Action: config.ActionDeny,
			// params.items is a string, not a list — .all() on a string fails at eval time
			Match:   config.Match{When: "params.items.all(x, x > 0)"},
			Message: "items check failed",
		},
	}
	ev := makeEvaluatorWithMode(t, config.ModeEnforce, config.ErrorModeOpen, rules)
	result := ev.Evaluate(celErrorCall())

	if result.Decision != Allow {
		t.Errorf("expected Allow (fail-open), got %s", result.Decision)
	}

	// The error should be recorded in the audit's RulesEvaluated.
	if len(result.Audit.RulesEvaluated) == 0 {
		t.Fatal("expected at least one rule in audit, got none")
	}
	rr := result.Audit.RulesEvaluated[0]
	if rr.Name != "check-items" {
		t.Errorf("expected rule name 'check-items', got %q", rr.Name)
	}
	if !rr.Error {
		t.Error("expected RuleResult.Error to be true for fail-open CEL error")
	}
	if rr.ErrorMessage == "" {
		t.Error("expected RuleResult.ErrorMessage to be non-empty for fail-open CEL error")
	}
}

// TestEval_AuditOnly_DenyNotEnforced verifies that in audit_only mode, a deny rule that
// matches results in Allow (not Deny), but the audit entry records that it would have denied.
func TestEval_AuditOnly_DenyNotEnforced(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "block-all",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "*"},
			Message: "all calls blocked",
		},
	}
	ev := makeEvaluatorWithMode(t, config.ModeAuditOnly, config.ErrorModeClosed, rules)
	result := ev.Evaluate(makeCall("test_op", nil))

	// In audit_only mode the call is allowed even if a deny rule matches.
	if result.Decision != Allow {
		t.Errorf("expected Allow in audit_only mode, got %s", result.Decision)
	}

	// The audit entry should record what WOULD have happened.
	if result.Audit.Decision != Deny {
		t.Errorf("expected audit Decision to be Deny (what would have happened), got %s", result.Audit.Decision)
	}
	if result.Audit.Rule != "block-all" {
		t.Errorf("expected audit Rule to be 'block-all', got %q", result.Audit.Rule)
	}

	// The audit entry should flag that enforcement was overridden.
	if result.Audit.Enforced {
		t.Error("expected Audit.Enforced to be false in audit_only mode")
	}

	// At least one rule should show as matched with action deny.
	if len(result.Audit.RulesEvaluated) == 0 {
		t.Fatal("expected at least one rule in audit, got none")
	}
	rr := result.Audit.RulesEvaluated[0]
	if !rr.Matched {
		t.Error("expected rule to be marked as matched in audit")
	}
	if rr.Action != "deny" {
		t.Errorf("expected action 'deny' in audit, got %q", rr.Action)
	}
}

// TestEval_AuditOnly_DenyContinuesEvaluation verifies that in audit_only mode, a deny rule
// does NOT short-circuit — all remaining rules are still evaluated and recorded in the audit.
func TestEval_AuditOnly_DenyContinuesEvaluation(t *testing.T) {
	rules := []config.Rule{
		{
			Name:    "first-deny",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "*"},
			Message: "first deny",
		},
		{
			Name:   "log-after-deny",
			Action: config.ActionLog,
			Match:  config.Match{Operation: "*"},
		},
		{
			Name:    "second-deny",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "*"},
			Message: "second deny",
		},
	}
	ev := makeEvaluatorWithMode(t, config.ModeAuditOnly, config.ErrorModeClosed, rules)
	result := ev.Evaluate(makeCall("test_op", nil))

	if result.Decision != Allow {
		t.Errorf("expected Allow in audit_only mode, got %s", result.Decision)
	}
	if result.Audit.Decision != Deny {
		t.Errorf("expected audit Decision Deny, got %s", result.Audit.Decision)
	}
	// Should record the first deny match.
	if result.Audit.Rule != "first-deny" {
		t.Errorf("expected audit Rule 'first-deny', got %q", result.Audit.Rule)
	}
	// All three rules should be evaluated.
	if len(result.Audit.RulesEvaluated) != 3 {
		t.Fatalf("expected 3 rules evaluated, got %d", len(result.Audit.RulesEvaluated))
	}
	// Verify all three rules were matched.
	for i, rr := range result.Audit.RulesEvaluated {
		if !rr.Matched {
			t.Errorf("rule %d (%s): expected Matched=true", i, rr.Name)
		}
	}
}

// TestEval_AuditOnly_RedactNotEnforced verifies that in audit_only mode, a redact rule that
// matches results in Allow with no mutations, but audit records the match.
func TestEval_AuditOnly_RedactNotEnforced(t *testing.T) {
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
	ev := makeEvaluatorWithMode(t, config.ModeAuditOnly, config.ErrorModeClosed, rules)
	result := ev.Evaluate(makeCall("send_message", map[string]any{
		"body": "my key is AKIAIOSFODNN7EXAMPLE ok",
	}))

	// In audit_only mode the result is Allow with no mutations.
	if result.Decision != Allow {
		t.Errorf("expected Allow in audit_only mode, got %s", result.Decision)
	}
	if len(result.Mutations) != 0 {
		t.Errorf("expected no mutations in audit_only mode, got %d", len(result.Mutations))
	}

	// Audit should record the redact rule matched.
	if len(result.Audit.RulesEvaluated) == 0 {
		t.Fatal("expected at least one rule in audit, got none")
	}
	rr := result.Audit.RulesEvaluated[0]
	if !rr.Matched {
		t.Error("expected rule to be marked as matched in audit")
	}
	if rr.Action != "redact" {
		t.Errorf("expected action 'redact' in audit, got %q", rr.Action)
	}

	// Enforced should be false.
	if result.Audit.Enforced {
		t.Error("expected Audit.Enforced to be false in audit_only mode")
	}
}

