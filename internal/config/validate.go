package config

import (
	"errors"
	"fmt"
	"regexp"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var defNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const (
	maxNameLen           = 64
	maxWhenLen           = 2048
	maxRulesPerScope     = 500
	maxPatternsPerRedact = 50
)

// Validate checks that rf is a well-formed rule file.
// It accumulates all validation errors and returns them joined.
func Validate(rf *RuleFile) error {
	var errs []error

	// 0. version must be supported
	if rf.Version != "" && rf.Version != SupportedVersion {
		errs = append(errs, fmt.Errorf("unsupported rule file version %q (supported: %s)", rf.Version, SupportedVersion))
	}

	// 1. scope is required and non-empty
	if rf.Scope == "" {
		errs = append(errs, errors.New("scope: required and must not be empty"))
	} else {
		// 2. scope matches name format, max 64 chars
		if len(rf.Scope) > maxNameLen {
			errs = append(errs, fmt.Errorf("scope: name %q exceeds maximum length of %d", rf.Scope, maxNameLen))
		} else if !nameRe.MatchString(rf.Scope) {
			errs = append(errs, fmt.Errorf("scope: name %q is invalid (must match [a-z][a-z0-9-]*)", rf.Scope))
		}
	}

	// 3. rules is required and non-empty; must not exceed maxRulesPerScope
	if len(rf.Rules) == 0 {
		errs = append(errs, errors.New("rules: required and must not be empty"))
	} else if len(rf.Rules) > maxRulesPerScope {
		errs = append(errs, fmt.Errorf("rules: %d rules exceeds maximum of %d per scope", len(rf.Rules), maxRulesPerScope))
	} else {
		seen := make(map[string]bool, len(rf.Rules))
		for i, rule := range rf.Rules {
			ruleErrs := validateRule(i, rule)
			errs = append(errs, ruleErrs...)

			// 6. rule names unique within scope
			if rule.Name != "" {
				if seen[rule.Name] {
					errs = append(errs, fmt.Errorf("rules[%d]: duplicate rule name %q within scope", i, rule.Name))
				}
				seen[rule.Name] = true
			}
		}
	}

	// defs validation
	if len(rf.Defs) > 0 {
		errs = append(errs, validateDefs(rf.Defs)...)
	}

	// 9. mode if set must be "enforce" or "audit_only"
	if rf.Mode != "" && rf.Mode != ModeEnforce && rf.Mode != ModeAuditOnly {
		errs = append(errs, fmt.Errorf("mode: invalid value %q (must be %q or %q)", rf.Mode, ModeEnforce, ModeAuditOnly))
	}

	// 10. on_error if set must be "closed" or "open"
	if rf.OnError != "" && rf.OnError != ErrorModeClosed && rf.OnError != ErrorModeOpen {
		errs = append(errs, fmt.Errorf("on_error: invalid value %q (must be %q or %q)", rf.OnError, ErrorModeClosed, ErrorModeOpen))
	}

	return errors.Join(errs...)
}

func validateRule(i int, rule Rule) []error {
	var errs []error

	// 4. each rule has a name
	if rule.Name == "" {
		errs = append(errs, fmt.Errorf("rules[%d]: name is required", i))
	} else {
		// 5. each rule name matches name format, max 64 chars
		if len(rule.Name) > maxNameLen {
			errs = append(errs, fmt.Errorf("rules[%d]: name %q exceeds maximum length of %d", i, rule.Name, maxNameLen))
		} else if !nameRe.MatchString(rule.Name) {
			errs = append(errs, fmt.Errorf("rules[%d]: name %q is invalid (must match [a-z][a-z0-9-]*)", i, rule.Name))
		}
	}

	// 7. each rule has a valid action
	switch rule.Action {
	case ActionDeny, ActionLog, ActionRedact:
		// valid
	case "":
		errs = append(errs, fmt.Errorf("rules[%d]: action is required", i))
	default:
		errs = append(errs, fmt.Errorf("rules[%d]: action %q is invalid (must be %q, %q, or %q)", i, rule.Action, ActionDeny, ActionLog, ActionRedact))
	}

	// 8. if action is redact, redact block must be present and valid
	if rule.Action == ActionRedact && rule.Redact == nil {
		errs = append(errs, fmt.Errorf("rules[%d]: action %q requires a redact block", i, ActionRedact))
	} else if rule.Action == ActionRedact && rule.Redact != nil {
		errs = append(errs, validateRedact(i, rule.Redact)...)
	}

	// 11. when expression if set must be <= 2048 chars
	if len(rule.Match.When) > maxWhenLen {
		errs = append(errs, fmt.Errorf("rules[%d]: when expression exceeds maximum length of %d", i, maxWhenLen))
	}

	return errs
}

// reservedDefNames points to the shared ReservedNames set; def names that
// match any entry are rejected to prevent shadowing CEL built-ins or
// Keep-specific functions.
var reservedDefNames = ReservedNames

func validateDefs(defs map[string]string) []error {
	var errs []error
	for name, value := range defs {
		// Name must match [a-z][a-z0-9_]*
		if !defNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("defs: name %q is invalid (must match [a-z][a-z0-9_]*)", name))
		} else if len(name) > maxNameLen {
			errs = append(errs, fmt.Errorf("defs: name %q exceeds maximum length of %d", name, maxNameLen))
		}

		// Name must not shadow built-in variables or functions
		if reservedDefNames[name] {
			errs = append(errs, fmt.Errorf("defs: name %q shadows a built-in variable or function", name))
		}

		// Value must be non-empty
		if value == "" {
			errs = append(errs, fmt.Errorf("defs: value for %q must not be empty", name))
		} else if len(value) > maxWhenLen {
			errs = append(errs, fmt.Errorf("defs: value for %q exceeds maximum length of %d", name, maxWhenLen))
		}
	}
	return errs
}

func validateRedact(i int, spec *RedactSpec) []error {
	var errs []error

	// target must be non-empty and a valid field path
	if spec.Target == "" {
		errs = append(errs, fmt.Errorf("rules[%d]: redact target is required", i))
	} else if err := ValidateFieldPath(spec.Target); err != nil {
		errs = append(errs, fmt.Errorf("rules[%d]: redact target %q: invalid field path: %w", i, spec.Target, err))
	} else if !IsParamsPath(spec.Target) {
		errs = append(errs, fmt.Errorf("rules[%d]: redact target %q must be a params.* path", i, spec.Target))
	}

	// Must have secrets: true or non-empty patterns (or both)
	if !spec.Secrets && len(spec.Patterns) == 0 {
		errs = append(errs, fmt.Errorf("rules[%d]: redact requires secrets: true or non-empty patterns", i))
	} else if len(spec.Patterns) > maxPatternsPerRedact {
		errs = append(errs, fmt.Errorf("rules[%d]: redact patterns: %d patterns exceeds maximum of %d", i, len(spec.Patterns), maxPatternsPerRedact))
	} else {
		for j, p := range spec.Patterns {
			if p.Match == "" {
				errs = append(errs, fmt.Errorf("rules[%d]: redact patterns[%d]: match must not be empty", i, j))
				continue
			}
			if _, err := regexp.Compile(p.Match); err != nil {
				errs = append(errs, fmt.Errorf("rules[%d]: redact patterns[%d]: match %q is not a valid RE2 pattern: %w", i, j, p.Match, err))
			}
		}
	}

	return errs
}
