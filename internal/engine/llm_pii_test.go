package engine

import (
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

// makeLLMPIIEvaluator builds an evaluator with a PII email detection rule.
func makeLLMPIIEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
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
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, defs, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func makeLLMTextCall(text, direction string) Call {
	return Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": text},
		Context:   CallContext{Scope: "test", Direction: direction},
	}
}

func TestLLMPII_EmailVariants(t *testing.T) {
	ev := makeLLMPIIEvaluator(t)

	mustDeny := []struct {
		name string
		text string
	}{
		{"support ticket", "Summarize this complaint: From: jane.doe@acmecorp.com, Subject: Billing error"},
		{"customer email inline", "The customer john.smith@example.com reported a bug"},
		{"email in log paste", "ERROR 2024-01-15 user=admin@internal.corp failed login"},
		{"multiple emails", "CC: alice@example.com, bob@example.org"},
		{"email with subdomain", "Contact support@mail.example.co.uk for help"},
		{"email with plus", "user+tag@example.com submitted a ticket"},
		{"email with dots", "first.middle.last@company.io is the account owner"},
		{"email in JSON", `{"email": "user@example.com", "name": "Test"}`},
		{"email in URL context", "Send to mailto:admin@example.com for approval"},
		{"short domain", "test@ab.co"},
		{"numeric local part", "12345@example.com"},
		{"hyphenated domain", "user@my-company.example.com"},
	}

	for _, tc := range mustDeny {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeLLMTextCall(tc.text, "request"))
			if result.Decision != Deny {
				t.Errorf("expected deny for PII, got %s for text: %s", result.Decision, tc.text)
			}
			if result.Rule != "block-pii-in-prompts" {
				t.Errorf("denied by wrong rule %q, expected block-pii-in-prompts", result.Rule)
			}
		})
	}
}

func TestLLMPII_NotEmails(t *testing.T) {
	ev := makeLLMPIIEvaluator(t)

	mustAllow := []struct {
		name string
		text string
	}{
		{"opaque customer ID", "Customer #4821 has a billing issue"},
		{"no email", "Summarize this complaint about billing"},
		{"at sign in code", "arr@idx is the element at index"},
		{"twitter handle", "Follow us @example on Twitter"},
		{"at in template", "Use ${user@host} for the connection string"},
	}

	for _, tc := range mustAllow {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeLLMTextCall(tc.text, "request"))
			if result.Decision == Deny && result.Rule == "block-pii-in-prompts" {
				t.Errorf("expected allow, got deny for: %s", tc.text)
			}
		})
	}
}

func TestLLMPII_DirectionMatters(t *testing.T) {
	ev := makeLLMPIIEvaluator(t)

	// PII rule should only fire on requests, not responses.
	result := ev.Evaluate(makeLLMTextCall("Contact jane@example.com for details", "response"))
	if result.Decision == Deny && result.Rule == "block-pii-in-prompts" {
		t.Error("PII rule should not fire on response direction")
	}
}
