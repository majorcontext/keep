package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadPacks reads all .yaml and .yml files from dir and parses them
// as starter packs. Returns packs indexed by name. Returns an empty map
// if dir is empty or does not exist.
func LoadPacks(dir string) (map[string]*StarterPack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]*StarterPack), nil
		}
		return nil, fmt.Errorf("loadpacks: cannot read directory %q: %w", dir, err)
	}

	result := make(map[string]*StarterPack)
	// Track which file first defined each pack name for error reporting.
	seenIn := make(map[string]string)

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		fullPath := filepath.Join(dir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("loadpacks: %s: cannot read file: %w", name, err)
		}

		var sp StarterPack
		if err := yaml.Unmarshal(data, &sp); err != nil {
			return nil, fmt.Errorf("loadpacks: %s: invalid YAML: %w", name, err)
		}

		if err := validatePack(&sp); err != nil {
			return nil, fmt.Errorf("loadpacks: %s: %w", name, err)
		}

		if prev, dup := seenIn[sp.Name]; dup {
			return nil, fmt.Errorf("loadpacks: %s: duplicate pack name %q already defined in %s", name, sp.Name, prev)
		}
		seenIn[sp.Name] = name
		result[sp.Name] = &sp
	}

	return result, nil
}

// validatePack checks that a StarterPack is well-formed, applying the same
// rule-level validations as Validate does for RuleFile rules.
func validatePack(sp *StarterPack) error {
	var errs []error

	if sp.Name == "" {
		errs = append(errs, errors.New("name: required and must not be empty"))
	}

	for i, rule := range sp.Rules {
		ruleErrs := validateRule(i, rule)
		errs = append(errs, ruleErrs...)
	}

	return errors.Join(errs...)
}

// ResolvePacks takes a rule file's pack references, looks them up in the
// loaded packs, applies overrides, and returns the merged rule list
// (pack rules first, then inline rules).
func ResolvePacks(rf *RuleFile, packs map[string]*StarterPack) ([]Rule, error) {
	var merged []Rule

	for _, ref := range rf.Packs {
		pack, ok := packs[ref.Name]
		if !ok {
			return nil, fmt.Errorf("resolvepacks: pack %q not found", ref.Name)
		}

		// Copy the pack's rules to avoid mutating the original.
		rules := make([]Rule, len(pack.Rules))
		copy(rules, pack.Rules)

		// Build an index from rule name to position for override lookups.
		index := make(map[string]int, len(rules))
		for i, r := range rules {
			index[r.Name] = i
		}

		// disabled tracks which rule indices should be removed.
		disabled := make(map[int]bool)

		for ruleName, overrideVal := range ref.Overrides {
			idx, exists := index[ruleName]
			if !exists {
				return nil, fmt.Errorf("resolvepacks: pack %q: override target %q does not exist", ref.Name, ruleName)
			}

			switch v := overrideVal.(type) {
			case string:
				if v != "disabled" {
					return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: string override must be \"disabled\", got %q", ref.Name, ruleName, v)
				}
				disabled[idx] = true

			case map[string]interface{}:
				for field, val := range v {
					switch field {
					case "when":
						s, ok := val.(string)
						if !ok {
							return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: override field %q must be a string", ref.Name, ruleName, field)
						}
						if len(s) > maxWhenLen {
							return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: when expression exceeds maximum length of %d", ref.Name, ruleName, maxWhenLen)
						}
						rules[idx].Match.When = s
					case "message":
						s, ok := val.(string)
						if !ok {
							return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: override field %q must be a string", ref.Name, ruleName, field)
						}
						rules[idx].Message = s
					case "action":
						s, ok := val.(string)
						if !ok {
							return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: override field %q must be a string", ref.Name, ruleName, field)
						}
						a := Action(s)
						if a != ActionDeny && a != ActionLog && a != ActionRedact {
							return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: override action %q is invalid (must be %q, %q, or %q)", ref.Name, ruleName, s, ActionDeny, ActionLog, ActionRedact)
						}
						rules[idx].Action = a
					case "name", "operation":
						return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: cannot override field %q", ref.Name, ruleName, field)
					default:
						return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: unknown override field %q", ref.Name, ruleName, field)
					}
				}

			default:
				return nil, fmt.Errorf("resolvepacks: pack %q: rule %q: override value must be \"disabled\" or a map", ref.Name, ruleName)
			}
		}

		// Append non-disabled rules to the merged list.
		for i, r := range rules {
			if !disabled[i] {
				merged = append(merged, r)
			}
		}
	}

	// Append inline rules after all pack rules.
	merged = append(merged, rf.Rules...)
	return merged, nil
}
