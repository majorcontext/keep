package config

import (
	"fmt"
	"strings"
	"unicode"
)

// LintWarning is a non-fatal issue found during linting.
type LintWarning struct {
	Scope   string
	Rule    string
	Message string
}

func (w LintWarning) String() string {
	return fmt.Sprintf("scope %q, rule %q: %s", w.Scope, w.Rule, w.Message)
}

// LintAll runs lint checks across all scopes in a load result.
func LintAll(lr *LoadResult) []LintWarning {
	var all []LintWarning
	for _, rf := range lr.Scopes {
		all = append(all, Lint(rf)...)
	}
	return all
}

// Lint checks a rule file for style issues that are not errors but may
// indicate mistakes. Currently checks for uppercase characters in string
// literals within when expressions (case-insensitive mode normalizes inputs
// to lowercase, so uppercase literals would never match).
func Lint(rf *RuleFile) []LintWarning {
	if rf.CaseSensitive {
		return nil
	}

	var warnings []LintWarning
	for _, rule := range rf.Rules {
		if rule.Match.When == "" {
			continue
		}
		if lits := uppercaseStringLiterals(rule.Match.When); len(lits) > 0 {
			warnings = append(warnings, LintWarning{
				Scope:   rf.Scope,
				Rule:    rule.Name,
				Message: fmt.Sprintf("when expression contains uppercase string literal(s) %s; inputs are lowered by default, so these will never match (use lowercase or set case_sensitive: true)", strings.Join(lits, ", ")),
			})
		}
	}
	return warnings
}

// uppercaseStringLiterals extracts string literals from a CEL expression
// that contain at least one uppercase letter. Handles both single- and
// double-quoted strings.
func uppercaseStringLiterals(expr string) []string {
	var result []string
	i := 0
	for i < len(expr) {
		c := expr[i]
		if c != '\'' && c != '"' {
			i++
			continue
		}

		quote := c
		i++ // skip opening quote
		start := i
		for i < len(expr) {
			if expr[i] == '\\' && i+1 < len(expr) {
				i += 2
				continue
			}
			if expr[i] == quote {
				break
			}
			i++
		}

		lit := expr[start:i]
		if i < len(expr) {
			i++ // skip closing quote
		}

		if hasUppercase(lit) {
			result = append(result, fmt.Sprintf("%c%s%c", quote, lit, quote))
		}
	}
	return result
}

func hasUppercase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
