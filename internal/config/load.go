package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadResult holds all parsed and resolved configuration.
type LoadResult struct {
	// Scopes maps scope name to its rule file.
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

	// 4. Resolve packs for each scope.
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
func LoadRules(dir string) (map[string]*RuleFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("loadrules: cannot read directory %q: %w", dir, err)
	}

	result := make(map[string]*RuleFile)
	// Track which file first defined each scope for error reporting.
	seenIn := make(map[string]string)

	found := 0
	for _, entry := range entries {
		if entry.IsDir() {
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
			return nil, fmt.Errorf("loadrules: %s: cannot read file: %w", name, err)
		}

		var rf RuleFile
		if err := yaml.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("loadrules: %s: invalid YAML: %w", name, err)
		}

		if err := Validate(&rf); err != nil {
			return nil, fmt.Errorf("loadrules: %s: %w", name, err)
		}

		if prev, dup := seenIn[rf.Scope]; dup {
			return nil, fmt.Errorf("loadrules: %s: duplicate scope %q already defined in %s", name, rf.Scope, prev)
		}
		seenIn[rf.Scope] = name
		result[rf.Scope] = &rf
	}

	if found == 0 {
		return nil, fmt.Errorf("loadrules: no rule files found in %q", dir)
	}

	return result, nil
}
