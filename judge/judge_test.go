package judge_test

import (
	"context"
	"testing"

	"github.com/majorcontext/keep/judge"
)

func TestDecisionConstants(t *testing.T) {
	if judge.Allow != "allow" {
		t.Errorf("Allow = %q, want %q", judge.Allow, "allow")
	}
	if judge.Deny != "deny" {
		t.Errorf("Deny = %q, want %q", judge.Deny, "deny")
	}
}

func TestProviderInterface(t *testing.T) {
	var p judge.Provider = &mockProvider{
		verdict: judge.Verdict{Decision: judge.Deny, Reason: "test"},
	}

	v, err := p.Judge(context.Background(), judge.Request{
		Prompt:  "Is this safe?",
		Content: "hello world",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Decision != judge.Deny {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Deny)
	}
	if v.Reason != "test" {
		t.Errorf("Reason = %q, want %q", v.Reason, "test")
	}
}

type mockProvider struct {
	verdict judge.Verdict
}

func (m *mockProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	return m.verdict, nil
}
