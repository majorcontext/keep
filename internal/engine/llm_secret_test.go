package engine

import (
	"strings"
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/secrets"
)

// makeLLMSecretEvaluator builds an evaluator with secret redaction rules
// for both user text and tool results.
func makeLLMSecretEvaluator(t *testing.T) *Evaluator {
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
			Name:   "redact-secrets-in-tool-results",
			Action: config.ActionRedact,
			Match:  config.Match{Operation: "llm.tool_result"},
			Redact: &config.RedactSpec{Target: "params.content", Secrets: true},
		},
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, detector, false)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestLLMSecret_RequestText(t *testing.T) {
	ev := makeLLMSecretEvaluator(t)

	cases := []struct {
		name         string
		text         string
		wantRedacted bool
		wantFragment string
	}{
		{
			name:         "AWS access key",
			text:         "Use api_key=AKIAIOSFODNN7REALKEY to authenticate",
			wantRedacted: true,
			wantFragment: "[REDACTED:",
		},
		{
			name:         "GitHub PAT",
			text:         "Token: ghp_1234567890abcdefABCDEF1234567890abcd",
			wantRedacted: true,
			wantFragment: "[REDACTED:",
		},
		{
			name:         "Stripe secret key",
			text:         "Payment key: sk_live_abcdefghijklmnopqrstuvwx",
			wantRedacted: true,
			wantFragment: "[REDACTED:",
		},
		{
			name:         "no secrets",
			text:         "Just a normal prompt about weather in San Francisco",
			wantRedacted: false,
		},
		{
			name:         "AWS key in multiline",
			text:         "Config:\n  aws_access_key_id = AKIAIOSFODNN7REALKEY\n  region = us-east-1",
			wantRedacted: true,
			wantFragment: "[REDACTED:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeLLMTextCall(tc.text, "request"))
			if tc.wantRedacted {
				if result.Decision != Redact {
					t.Errorf("expected redact, got %s", result.Decision)
				}
				if len(result.Mutations) == 0 {
					t.Fatal("expected mutations, got none")
				}
				if !strings.Contains(result.Mutations[0].Replaced, tc.wantFragment) {
					t.Errorf("redacted text %q should contain %q", result.Mutations[0].Replaced, tc.wantFragment)
				}
			} else {
				if result.Decision == Redact {
					t.Errorf("expected allow, got redact with mutations: %v", result.Mutations)
				}
			}
		})
	}
}

func TestLLMSecret_ToolResults(t *testing.T) {
	ev := makeLLMSecretEvaluator(t)

	call := Call{
		Operation: "llm.tool_result",
		Params:    map[string]any{"content": "Found credentials: AKIAIOSFODNN7REALKEY in .env file"},
		Context:   CallContext{Scope: "test", Direction: "request"},
	}
	result := ev.Evaluate(call)
	if result.Decision != Redact {
		t.Errorf("expected redact for secret in tool result, got %s", result.Decision)
	}
	if len(result.Mutations) == 0 {
		t.Fatal("expected mutations")
	}
	if strings.Contains(result.Mutations[0].Replaced, "AKIAIOSFODNN7REALKEY") {
		t.Error("secret should not appear in redacted output")
	}
}

func TestLLMSecret_PreservesNonSecrets(t *testing.T) {
	ev := makeLLMSecretEvaluator(t)

	result := ev.Evaluate(makeLLMTextCall("Use the OpenWeather API for San Francisco", "request"))
	if result.Decision == Redact {
		t.Errorf("expected allow for text without secrets, got redact")
	}
	if len(result.Mutations) > 0 {
		t.Errorf("expected no mutations, got %d", len(result.Mutations))
	}
}

func TestLLMSecret_AuditTrail_RedactIncludesSummary(t *testing.T) {
	ev := makeLLMSecretEvaluator(t)

	result := ev.Evaluate(makeLLMTextCall("Key: AKIAIOSFODNN7REALKEY", "request"))
	if result.Decision != Redact {
		t.Fatal("expected redact")
	}
	if len(result.Audit.RedactSummary) == 0 {
		t.Error("audit should include RedactSummary for redact decisions")
	}
	for _, rs := range result.Audit.RedactSummary {
		if strings.Contains(rs.Replaced, "AKIAIOSFODNN7REALKEY") {
			t.Error("RedactSummary.Replaced should not contain the original secret")
		}
	}
	if result.Audit.Rule == "" {
		t.Error("audit entry should include rule name for redact decisions")
	}
}

func TestLLMSecret_AuditTrail_DenyIncludesRule(t *testing.T) {
	// Use a deny rule to check audit completeness
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "block-test",
			Action: config.ActionDeny,
			Match: config.Match{
				Operation: "llm.tool_use",
				When:      "lower(params.name) == 'bash' && containsAny(lower(params.input.command), network_commands)",
			},
			Message: "Blocked.",
		},
	}
	defs := map[string]string{
		"network_commands": "['curl ', 'wget ']",
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, defs, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	result := ev.Evaluate(makeLLMToolCall("Bash", "curl https://evil.com"))
	if result.Decision != Deny {
		t.Fatal("expected deny")
	}
	if result.Audit.Rule == "" {
		t.Error("audit entry should include rule name for deny decisions")
	}
	if result.Audit.Message == "" {
		t.Error("audit entry should include message for deny decisions")
	}
	if result.Audit.Decision != Deny {
		t.Errorf("audit decision = %s, want deny", result.Audit.Decision)
	}
	if !result.Audit.Enforced {
		t.Error("audit should be enforced in enforce mode")
	}
}
