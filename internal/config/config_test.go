package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuleFileUnmarshal(t *testing.T) {
	input := `
scope: test-scope
mode: enforce
rules:
  - name: deny-all
    match:
      operation: "*"
    action: deny
    message: "blocked"
`
	var rf RuleFile
	if err := yaml.Unmarshal([]byte(input), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Scope != "test-scope" {
		t.Errorf("scope = %q, want %q", rf.Scope, "test-scope")
	}
	if rf.Mode != ModeEnforce {
		t.Errorf("mode = %q, want %q", rf.Mode, ModeEnforce)
	}
	if len(rf.Rules) != 1 {
		t.Fatalf("rules count = %d, want 1", len(rf.Rules))
	}
	if rf.Rules[0].Action != ActionDeny {
		t.Errorf("action = %q, want %q", rf.Rules[0].Action, ActionDeny)
	}
}
