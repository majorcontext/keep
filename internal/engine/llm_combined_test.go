package engine

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/secrets"
)

// makeLLMCombinedEvaluator builds an evaluator with both secret redaction
// and PII denial rules to test rule interaction and ordering.
func makeLLMCombinedEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	detector, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "redact-secrets-in-text",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "llm.text"},
			Redact: &config.RedactSpec{Target: "params.text", Secrets: true},
		},
		{
			Name:   "block-pii-in-prompts",
			Action: config.ActionDeny,
			Match: config.Match{
				Operation: "llm.text",
				When:      "context.direction == 'request' && matches(params.text, email_pattern)",
			},
			Message: "PII detected in prompt. Use opaque customer IDs.",
		},
	}
	defs := map[string]string{
		"email_pattern": "'[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}'",
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, defs, detector)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestLLMCombined_SecretAndPII_DenyTakesPrecedence(t *testing.T) {
	ev := makeLLMCombinedEvaluator(t)

	// A prompt with BOTH a secret AND an email. The PII deny should fire.
	text := "Customer jane@example.com has key AKIAIOSFODNN7REALKEY"
	result := ev.Evaluate(makeLLMTextCall(text, "request"))

	if result.Decision != Deny {
		t.Errorf("expected deny (PII), got %s (rule=%s)", result.Decision, result.Rule)
	}
}

func TestLLMCombined_SecretOnly_Redacted(t *testing.T) {
	ev := makeLLMCombinedEvaluator(t)

	// Secret without PII: should redact, not deny.
	text := "Use key AKIAIOSFODNN7REALKEY to call the API"
	result := ev.Evaluate(makeLLMTextCall(text, "request"))

	if result.Decision != Redact {
		t.Errorf("expected redact, got %s (rule=%s)", result.Decision, result.Rule)
	}
}

func TestLLMCombined_PIIOnly_Denied(t *testing.T) {
	ev := makeLLMCombinedEvaluator(t)

	// PII without secret: should deny.
	text := "The customer john.smith@example.com reported a bug"
	result := ev.Evaluate(makeLLMTextCall(text, "request"))

	if result.Decision != Deny {
		t.Errorf("expected deny, got %s (rule=%s)", result.Decision, result.Rule)
	}
}

func TestLLMCombined_CleanPrompt_Allowed(t *testing.T) {
	ev := makeLLMCombinedEvaluator(t)

	// No secrets, no PII: should pass through.
	text := "What is the weather in San Francisco?"
	result := ev.Evaluate(makeLLMTextCall(text, "request"))

	if result.Decision != Allow {
		t.Errorf("expected allow, got %s (rule=%s)", result.Decision, result.Rule)
	}
}
