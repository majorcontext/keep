package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	validRule := Rule{
		Name:   "block-secrets",
		Action: ActionDeny,
	}

	tests := []struct {
		name        string
		rf          *RuleFile
		wantErr     bool
		errContains string
	}{
		{
			name: "valid file passes",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{validRule},
			},
			wantErr: false,
		},
		{
			name: "missing scope is an error",
			rf: &RuleFile{
				Rules: []Rule{validRule},
			},
			wantErr:     true,
			errContains: "scope",
		},
		{
			name: "scope name with uppercase is an error",
			rf: &RuleFile{
				Scope: "MyScope",
				Rules: []Rule{validRule},
			},
			wantErr:     true,
			errContains: "scope",
		},
		{
			name: "scope name over 64 chars is an error",
			rf: &RuleFile{
				Scope: "a" + strings.Repeat("b", 64),
				Rules: []Rule{validRule},
			},
			wantErr:     true,
			errContains: "scope",
		},
		{
			name: "missing rules nil is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: nil,
			},
			wantErr:     true,
			errContains: "rules",
		},
		{
			name: "missing rules empty is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{},
			},
			wantErr:     true,
			errContains: "rules",
		},
		{
			name: "rule missing name is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Action: ActionDeny},
				},
			},
			wantErr:     true,
			errContains: "name",
		},
		{
			name: "rule missing action is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "my-rule"},
				},
			},
			wantErr:     true,
			errContains: "action",
		},
		{
			name: "rule name with uppercase is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "MyRule", Action: ActionDeny},
				},
			},
			wantErr:     true,
			errContains: "name",
		},
		{
			name: "rule name over 64 chars is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "a" + strings.Repeat("b", 64), Action: ActionDeny},
				},
			},
			wantErr:     true,
			errContains: "name",
		},
		{
			name: "duplicate rule names within scope is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "dup-rule", Action: ActionDeny},
					{Name: "dup-rule", Action: ActionLog},
				},
			},
			wantErr:     true,
			errContains: "duplicate",
		},
		{
			name: "action redact without redact block is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "my-rule", Action: ActionRedact},
				},
			},
			wantErr:     true,
			errContains: "redact",
		},
		{
			name: "action redact with redact block is valid",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{
						Name:   "my-rule",
						Action: ActionRedact,
						Redact: &RedactSpec{
							Target:   "params.content",
							Patterns: []RedactPattern{{Match: `\d+`, Replace: "[NUM]"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "redact target not under params is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{
						Name:   "my-rule",
						Action: ActionRedact,
						Redact: &RedactSpec{
							Target:   "response",
							Patterns: []RedactPattern{{Match: `\d+`, Replace: "[NUM]"}},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "params",
		},
		{
			name: "invalid mode value is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Mode:  Mode("strict"),
				Rules: []Rule{validRule},
			},
			wantErr:     true,
			errContains: "mode",
		},
		{
			name: "valid mode enforce",
			rf: &RuleFile{
				Scope: "my-scope",
				Mode:  ModeEnforce,
				Rules: []Rule{validRule},
			},
			wantErr: false,
		},
		{
			name: "valid mode audit_only",
			rf: &RuleFile{
				Scope: "my-scope",
				Mode:  ModeAuditOnly,
				Rules: []Rule{validRule},
			},
			wantErr: false,
		},
		{
			name: "invalid on_error value is an error",
			rf: &RuleFile{
				Scope:   "my-scope",
				OnError: ErrorMode("fail"),
				Rules:   []Rule{validRule},
			},
			wantErr:     true,
			errContains: "on_error",
		},
		{
			name: "valid on_error closed",
			rf: &RuleFile{
				Scope:   "my-scope",
				OnError: ErrorModeClosed,
				Rules:   []Rule{validRule},
			},
			wantErr: false,
		},
		{
			name: "valid on_error open",
			rf: &RuleFile{
				Scope:   "my-scope",
				OnError: ErrorModeOpen,
				Rules:   []Rule{validRule},
			},
			wantErr: false,
		},
		{
			name: "when expression over 2048 chars is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{
						Name:   "long-when",
						Action: ActionDeny,
						Match:  Match{When: strings.Repeat("x", 2049)},
					},
				},
			},
			wantErr:     true,
			errContains: "when",
		},
		{
			name: "when expression at exactly 2048 chars is valid",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{
						Name:   "long-when",
						Action: ActionDeny,
						Match:  Match{When: strings.Repeat("x", 2048)},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid action value is an error",
			rf: &RuleFile{
				Scope: "my-scope",
				Rules: []Rule{
					{Name: "my-rule", Action: Action("block")},
				},
			},
			wantErr:     true,
			errContains: "action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.rf)
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.errContains)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

func makeRedactRule(spec *RedactSpec) *RuleFile {
	return &RuleFile{
		Scope: "my-scope",
		Rules: []Rule{
			{
				Name:   "my-rule",
				Action: ActionRedact,
				Redact: spec,
			},
		},
	}
}

func TestValidateRedact_ValidPattern(t *testing.T) {
	rf := makeRedactRule(&RedactSpec{
		Target:   "params.content",
		Patterns: []RedactPattern{{Match: `\d{4}-\d{4}`, Replace: "[REDACTED]"}},
	})
	if err := Validate(rf); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRedact_InvalidRegex(t *testing.T) {
	rf := makeRedactRule(&RedactSpec{
		Target:   "params.content",
		Patterns: []RedactPattern{{Match: "(unclosed", Replace: "[REDACTED]"}},
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for invalid RE2 pattern")
	}
	if !strings.Contains(err.Error(), "pattern") && !strings.Contains(err.Error(), "match") {
		t.Errorf("Validate() error = %q, want it to mention pattern or match", err.Error())
	}
}

func TestValidateRedact_MissingTarget(t *testing.T) {
	rf := makeRedactRule(&RedactSpec{
		Target:   "",
		Patterns: []RedactPattern{{Match: `\d+`, Replace: "[NUM]"}},
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for missing target")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "target")
	}
}

func TestValidateRedact_EmptyPatterns(t *testing.T) {
	rf := makeRedactRule(&RedactSpec{
		Target:   "params.content",
		Patterns: []RedactPattern{},
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for empty patterns")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "pattern")
	}
}

func TestValidateRedact_MissingReplace(t *testing.T) {
	// Replace is a string so it defaults to "". The key validation is that match is non-empty.
	// Test that a pattern with empty match is an error.
	rf := makeRedactRule(&RedactSpec{
		Target:   "params.content",
		Patterns: []RedactPattern{{Match: "", Replace: "[REDACTED]"}},
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for pattern with empty match")
	}
	if !strings.Contains(err.Error(), "match") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "match")
	}
}

func TestValidateRedact_InvalidTargetPath(t *testing.T) {
	rf := makeRedactRule(&RedactSpec{
		Target:   "123invalid",
		Patterns: []RedactPattern{{Match: `\d+`, Replace: "[NUM]"}},
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for invalid field path syntax")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "target")
	}
}

func TestValidate_TooManyRules(t *testing.T) {
	rules := make([]Rule, maxRulesPerScope+1)
	for i := range rules {
		rules[i] = Rule{Name: fmt.Sprintf("rule-%d", i), Action: ActionDeny}
	}
	rf := &RuleFile{Scope: "my-scope", Rules: rules}
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for too many rules")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Validate() error = %q, want it to mention exceeds maximum", err.Error())
	}
}

func TestValidate_TooManyPatternsPerRedact(t *testing.T) {
	patterns := make([]RedactPattern, maxPatternsPerRedact+1)
	for i := range patterns {
		patterns[i] = RedactPattern{Match: `\d+`, Replace: "[NUM]"}
	}
	rf := makeRedactRule(&RedactSpec{
		Target:   "params.content",
		Patterns: patterns,
	})
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for too many redact patterns")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Validate() error = %q, want it to mention exceeds maximum", err.Error())
	}
}

func TestValidate_DefsValid(t *testing.T) {
	rf := &RuleFile{
		Scope: "my-scope",
		Defs: map[string]string{
			"allowed_teams": "['TEAM-ENG', 'TEAM-INFRA']",
			"max_priority":  "1",
		},
		Rules: []Rule{
			{Name: "my-rule", Action: ActionDeny},
		},
	}
	if err := Validate(rf); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidate_DefsBadName(t *testing.T) {
	tests := []struct {
		name        string
		defName     string
		errContains string
	}{
		{"uppercase", "MyDef", "invalid"},
		{"starts with number", "1abc", "invalid"},
		{"contains hyphen", "my-def", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := &RuleFile{
				Scope: "my-scope",
				Defs:  map[string]string{tt.defName: "'value'"},
				Rules: []Rule{{Name: "my-rule", Action: ActionDeny}},
			}
			err := Validate(rf)
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestValidate_DefsShadowsBuiltin(t *testing.T) {
	for _, name := range []string{"params", "context", "now", "size", "true", "false"} {
		t.Run(name, func(t *testing.T) {
			rf := &RuleFile{
				Scope: "my-scope",
				Defs:  map[string]string{name: "'value'"},
				Rules: []Rule{{Name: "my-rule", Action: ActionDeny}},
			}
			err := Validate(rf)
			if err == nil {
				t.Fatalf("Validate() = nil, want error for shadowed name %q", name)
			}
			if !strings.Contains(err.Error(), "shadows") {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "shadows")
			}
		})
	}
}

func TestValidate_DefsEmptyValue(t *testing.T) {
	rf := &RuleFile{
		Scope: "my-scope",
		Defs:  map[string]string{"my_def": ""},
		Rules: []Rule{{Name: "my-rule", Action: ActionDeny}},
	}
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for empty def value")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "must not be empty")
	}
}

func TestValidate_DefsValueTooLong(t *testing.T) {
	rf := &RuleFile{
		Scope: "my-scope",
		Defs:  map[string]string{"my_def": strings.Repeat("x", 2049)},
		Rules: []Rule{{Name: "my-rule", Action: ActionDeny}},
	}
	err := Validate(rf)
	if err == nil {
		t.Fatal("Validate() = nil, want error for too-long def value")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), "exceeds maximum")
	}
}

func TestValidate_RedactSecretsOnly(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{
				Name:   "redact-secrets",
				Match:  Match{Operation: "llm.text"},
				Action: ActionRedact,
				Redact: &RedactSpec{
					Target:  "params.text",
					Secrets: true,
				},
			},
		},
	}
	if err := Validate(rf); err != nil {
		t.Errorf("expected secrets-only redact rule to be valid, got: %v", err)
	}
}

func TestValidate_RedactNoSecretsNoPatterns(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{
				Name:   "bad-redact",
				Match:  Match{Operation: "llm.text"},
				Action: ActionRedact,
				Redact: &RedactSpec{
					Target: "params.text",
				},
			},
		},
	}
	if err := Validate(rf); err == nil {
		t.Error("expected error for redact rule with no secrets and no patterns")
	}
}

func TestValidate_DefNameHasSecrets(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{Name: "r1", Match: Match{Operation: "op"}, Action: ActionLog},
		},
		Defs: map[string]string{"hassecrets": "'shadowed'"},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "shadows") {
		t.Errorf("expected shadow error for hasSecrets def, got: %v", err)
	}
}

