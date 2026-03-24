package config

import (
	"testing"
)

func TestLint_UppercaseStringLiterals(t *testing.T) {
	tests := []struct {
		name     string
		rf       *RuleFile
		wantN    int
		wantNone bool
	}{
		{
			name: "lowercase literals no warning",
			rf: &RuleFile{
				Scope: "test",
				Rules: []Rule{
					{Name: "r1", Match: Match{When: "params.name == 'bash'"}},
				},
			},
			wantNone: true,
		},
		{
			name: "uppercase literal warns",
			rf: &RuleFile{
				Scope: "test",
				Rules: []Rule{
					{Name: "r1", Match: Match{When: "params.name == 'Bash'"}},
				},
			},
			wantN: 1,
		},
		{
			name: "multiple uppercase literals",
			rf: &RuleFile{
				Scope: "test",
				Rules: []Rule{
					{Name: "r1", Match: Match{When: "params.a == 'FOO' && params.b == 'BAR'"}},
				},
			},
			wantN: 1, // one warning per rule, listing both literals
		},
		{
			name: "case_sensitive skips lint",
			rf: &RuleFile{
				Scope:         "test",
				CaseSensitive: true,
				Rules: []Rule{
					{Name: "r1", Match: Match{When: "params.name == 'Bash'"}},
				},
			},
			wantNone: true,
		},
		{
			name: "no when expression",
			rf: &RuleFile{
				Scope: "test",
				Rules: []Rule{
					{Name: "r1", Match: Match{Operation: "create"}},
				},
			},
			wantNone: true,
		},
		{
			name: "double quoted uppercase",
			rf: &RuleFile{
				Scope: "test",
				Rules: []Rule{
					{Name: "r1", Match: Match{When: `params.name == "Bash"`}},
				},
			},
			wantN: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := Lint(tt.rf)
			if tt.wantNone && len(warnings) > 0 {
				t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
			}
			if tt.wantN > 0 && len(warnings) != tt.wantN {
				t.Errorf("expected %d warnings, got %d: %v", tt.wantN, len(warnings), warnings)
			}
		})
	}
}

func TestUppercaseStringLiterals(t *testing.T) {
	tests := []struct {
		expr string
		want int
	}{
		{"params.x == 'hello'", 0},
		{"params.x == 'Hello'", 1},
		{"params.x == 'FOO' && params.y == 'BAR'", 2},
		{"params.x == 'foo' && params.y == 'bar'", 0},
		{`params.x == "Hello"`, 1},
		{"", 0},
		{"params.x > 5", 0},
	}

	for _, tt := range tests {
		lits := uppercaseStringLiterals(tt.expr)
		if len(lits) != tt.want {
			t.Errorf("uppercaseStringLiterals(%q): got %d literals %v, want %d", tt.expr, len(lits), lits, tt.want)
		}
	}
}
