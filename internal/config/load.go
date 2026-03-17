package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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
