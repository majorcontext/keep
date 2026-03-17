package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadResult holds all parsed and resolved configuration.
type LoadResult struct {
	// Scopes contains the raw parsed rule files before pack resolution.
	// Use ResolvedRules for the authoritative post-resolution rule list.
	Scopes map[string]*RuleFile

	// ResolvedRules maps scope name to the merged rule list (packs + inline).
	ResolvedRules map[string][]Rule

	// Profiles maps profile name to its parsed profile.
	Profiles map[string]*Profile
}

// LoadAll reads rules, profiles, and packs from their respective directories,
// validates everything, resolves pack references and overrides, and returns
// the fully resolved configuration.
// profilesDir and packsDir may be empty strings if not needed.
func LoadAll(rulesDir string, profilesDir string, packsDir string) (*LoadResult, error) {
	// 1. Load rules -- mandatory.
	scopes, err := LoadRules(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("loadall: %w", err)
	}

	// 2. Load profiles if a directory was provided.
	var profiles map[string]*Profile
	if profilesDir != "" {
		profiles, err = LoadProfiles(profilesDir)
		if err != nil {
			return nil, fmt.Errorf("loadall: %w", err)
		}
	} else {
		profiles = make(map[string]*Profile)
	}

	// 3. Load packs if a directory was provided.
	var packs map[string]*StarterPack
	if packsDir != "" {
		packs, err = LoadPacks(packsDir)
		if err != nil {
			return nil, fmt.Errorf("loadall: %w", err)
		}
	} else {
		packs = make(map[string]*StarterPack)
	}

	// 4. Validate profile cross-references for rule files and packs,
	// but only when a profiles directory was provided.
	if profilesDir != "" {
		for scope, rf := range scopes {
			if rf.Profile != "" {
				if _, ok := profiles[rf.Profile]; !ok {
					return nil, fmt.Errorf("loadall: scope %q references profile %q which was not found", scope, rf.Profile)
				}
			}
		}
		for _, sp := range packs {
			if sp.Profile != "" {
				if _, ok := profiles[sp.Profile]; !ok {
					return nil, fmt.Errorf("loadall: scope %q references profile %q which was not found", sp.Name, sp.Profile)
				}
			}
		}
	}

	// 5. Resolve packs for each scope.
	// If no packs directory was provided, skip pack resolution and use only
	// the inline rules from each rule file.
	resolvedRules := make(map[string][]Rule, len(scopes))
	for scope, rf := range scopes {
		if packsDir == "" {
			// No packs directory: return only the inline rules.
			resolvedRules[scope] = append([]Rule(nil), rf.Rules...)
		} else {
			rules, err := ResolvePacks(rf, packs)
			if err != nil {
				return nil, fmt.Errorf("loadall: scope %q: %w", scope, err)
			}
			resolvedRules[scope] = rules
		}
	}

	return &LoadResult{
		Scopes:        scopes,
		ResolvedRules: resolvedRules,
		Profiles:      profiles,
	}, nil
}

// LoadRules reads all .yaml and .yml files from dir, parses them as
// rule files, validates each one, and checks scope uniqueness across
// all files. Returns the parsed files indexed by scope name.
// Errors from all files are accumulated and returned together.
func LoadRules(dir string) (map[string]*RuleFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("loadrules: cannot read directory %q: %w", dir, err)
	}

	result := make(map[string]*RuleFile)
	// Track which file first defined each scope for error reporting.
	seenIn := make(map[string]string)

	var errs []error
	found := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		found++

		fullPath := filepath.Join(dir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("loadrules: %s: cannot read file: %w", name, err))
			continue
		}
		if len(data) > maxFileBytes {
			errs = append(errs, fmt.Errorf("loadrules: %s: file size %d exceeds maximum of %d bytes", name, len(data), maxFileBytes))
			continue
		}

		var rf RuleFile
		if err := yaml.Unmarshal(data, &rf); err != nil {
			errs = append(errs, fmt.Errorf("loadrules: %s: invalid YAML: %w", name, err))
			continue
		}

		if err := Validate(&rf); err != nil {
			errs = append(errs, fmt.Errorf("loadrules: %s: %w", name, err))
			continue
		}

		if prev, dup := seenIn[rf.Scope]; dup {
			errs = append(errs, fmt.Errorf("loadrules: %s: duplicate scope %q already defined in %s", name, rf.Scope, prev))
			continue
		}
		seenIn[rf.Scope] = name
		result[rf.Scope] = &rf
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if found == 0 {
		return nil, fmt.Errorf("loadrules: no rule files found in %q", dir)
	}

	return result, nil
}
