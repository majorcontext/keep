package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

// benchEvaluator creates an Evaluator for benchmarks. It uses testing.B.Fatal
// on error so the benchmark is aborted rather than silently broken.
func benchEvaluator(b *testing.B, rules []config.Rule) *Evaluator {
	b.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		b.Fatal(err)
	}
	ev, err := NewEvaluator(env, "bench-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	if err != nil {
		b.Fatal(err)
	}
	return ev
}

// benchCall creates a Call with standard context fields populated.
func benchCall(operation string, params map[string]any) Call {
	return Call{
		Operation: operation,
		Params:    params,
		Context: CallContext{
			AgentID:   "agent-bench",
			UserID:    "user-bench",
			Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
			Scope:     "bench-scope",
			Direction: "inbound",
			Labels:    map[string]string{"env": "bench"},
		},
	}
}

func BenchmarkEvaluate(b *testing.B) {
	b.Run("simple_match", benchSimpleMatch)
	b.Run("simple_no_match", benchSimpleNoMatch)
	b.Run("cel_expression", benchCELExpression)
	b.Run("cel_expression_no_match", benchCELExpressionNoMatch)
	b.Run("multiple_rules_early_match", benchMultipleRulesEarlyMatch)
	b.Run("multiple_rules_late_match", benchMultipleRulesLateMatch)
	b.Run("multiple_rules_no_match", benchMultipleRulesNoMatch)
	b.Run("glob_matching", benchGlobMatching)
	b.Run("glob_star_catch_all", benchGlobCatchAll)
	b.Run("redaction_regex", benchRedactionRegex)
	b.Run("large_params", benchLargeParams)
	b.Run("large_string_value", benchLargeStringValue)
}

// benchSimpleMatch: single rule, exact operation match, no CEL expression.
func benchSimpleMatch(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:    "block-deletes",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "delete_issue"},
			Message: "deletes not allowed",
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("delete_issue", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchSimpleNoMatch: single rule, operation does not match.
func benchSimpleNoMatch(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:   "block-deletes",
			Action: config.ActionDeny,
			Match:  config.Match{Operation: "delete_issue"},
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("create_issue", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchCELExpression: rule with a `when` clause (boolean expression on params).
func benchCELExpression(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:    "block-low-priority",
			Action:  config.ActionDeny,
			Match:   config.Match{When: "params.priority == 0"},
			Message: "low priority denied",
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("create_issue", map[string]any{"priority": int64(0)})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchCELExpressionNoMatch: CEL expression that evaluates to false.
func benchCELExpressionNoMatch(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:   "block-low-priority",
			Action: config.ActionDeny,
			Match:  config.Match{When: "params.priority == 0"},
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("create_issue", map[string]any{"priority": int64(5)})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchMultipleRulesEarlyMatch: 12 rules, first rule matches (best case).
func benchMultipleRulesEarlyMatch(b *testing.B) {
	b.ReportAllocs()
	rules := make([]config.Rule, 12)
	rules[0] = config.Rule{
		Name:    "rule-0",
		Action:  config.ActionDeny,
		Match:   config.Match{Operation: "target_op"},
		Message: "denied",
	}
	for i := 1; i < 12; i++ {
		rules[i] = config.Rule{
			Name:   fmt.Sprintf("rule-%d", i),
			Action: config.ActionDeny,
			Match:  config.Match{Operation: fmt.Sprintf("other_op_%d", i)},
		}
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("target_op", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchMultipleRulesLateMatch: 12 rules, last rule matches (worst case for match).
func benchMultipleRulesLateMatch(b *testing.B) {
	b.ReportAllocs()
	rules := make([]config.Rule, 12)
	for i := 0; i < 11; i++ {
		rules[i] = config.Rule{
			Name:   fmt.Sprintf("rule-%d", i),
			Action: config.ActionDeny,
			Match:  config.Match{Operation: fmt.Sprintf("other_op_%d", i)},
		}
	}
	rules[11] = config.Rule{
		Name:    "rule-11",
		Action:  config.ActionDeny,
		Match:   config.Match{Operation: "target_op"},
		Message: "denied",
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("target_op", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchMultipleRulesNoMatch: 12 rules, none match (worst case overall).
func benchMultipleRulesNoMatch(b *testing.B) {
	b.ReportAllocs()
	rules := make([]config.Rule, 12)
	for i := 0; i < 12; i++ {
		rules[i] = config.Rule{
			Name:   fmt.Sprintf("rule-%d", i),
			Action: config.ActionDeny,
			Match:  config.Match{Operation: fmt.Sprintf("other_op_%d", i)},
		}
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("unmatched_op", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchGlobMatching: rule with wildcard operation like "read_*".
func benchGlobMatching(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:    "block-reads",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "read_*"},
			Message: "reads not allowed",
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("read_document", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchGlobCatchAll: rule with "*" catch-all operation.
func benchGlobCatchAll(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:    "log-everything",
			Action:  config.ActionLog,
			Match:   config.Match{Operation: "*"},
			Message: "logged",
		},
	}
	ev := benchEvaluator(b, rules)
	call := benchCall("any_operation", map[string]any{"key": "value"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchRedactionRegex: rule with redact action and regex pattern.
func benchRedactionRegex(b *testing.B) {
	b.ReportAllocs()
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
	ev := benchEvaluator(b, rules)
	call := benchCall("send_message", map[string]any{
		"body": "credentials: AKIAIOSFODNN7EXAMPLE and some other text around it for padding",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchLargeParams: evaluation with a params map containing many keys.
func benchLargeParams(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:   "check-status",
			Action: config.ActionDeny,
			Match:  config.Match{When: "params.status == 'blocked'"},
		},
	}
	ev := benchEvaluator(b, rules)

	params := make(map[string]any, 50)
	for i := 0; i < 49; i++ {
		params[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
	}
	params["status"] = "blocked"
	call := benchCall("update_record", params)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}

// benchLargeStringValue: evaluation with a large string value in params.
func benchLargeStringValue(b *testing.B) {
	b.ReportAllocs()
	rules := []config.Rule{
		{
			Name:   "redact-ssn",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "*"},
			Redact: &config.RedactSpec{
				Target: "params.content",
				Patterns: []config.RedactPattern{
					{Match: `\d{3}-\d{2}-\d{4}`, Replace: "***SSN***"},
				},
			},
		},
	}
	ev := benchEvaluator(b, rules)

	// Build a ~10KB string with an SSN buried in the middle.
	bigText := strings.Repeat("Lorem ipsum dolor sit amet. ", 200)
	bigText = bigText[:5000] + " SSN: 123-45-6789 " + bigText[5000:]
	call := benchCall("process_document", map[string]any{
		"content": bigText,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Evaluate(call)
	}
}
