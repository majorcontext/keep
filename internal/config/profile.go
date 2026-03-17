package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var aliasNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const maxAliasNameLen = 32

// reservedWords is the set of alias names that must not be used because they
// shadow CEL built-ins or top-level variable names.
var reservedWords = map[string]bool{
	"params":      true,
	"context":     true,
	"size":        true,
	"has":         true,
	"matches":     true,
	"startsWith":  true,
	"endsWith":    true,
	"contains":    true,
	"exists":      true,
	"all":         true,
	"filter":      true,
	"map":         true,
	"exists_one":  true,
	"int":         true,
	"uint":        true,
	"double":      true,
	"string":      true,
	"bool":        true,
	"bytes":       true,
	"list":        true,
	"type":        true,
	"null":        true,
	"true":        true,
	"false":       true,
}

// LoadProfiles reads all .yaml and .yml files from dir, parses them
// as profiles, and validates alias names and targets.
// Returns profiles indexed by name. Returns an empty map if dir is empty
// or does not exist.
func LoadProfiles(dir string) (map[string]*Profile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]*Profile), nil
		}
		return nil, fmt.Errorf("loadprofiles: cannot read directory %q: %w", dir, err)
	}

	result := make(map[string]*Profile)
	// Track which file first defined each profile name for error reporting.
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
			return nil, fmt.Errorf("loadprofiles: %s: cannot read file: %w", name, err)
		}
		if len(data) > maxFileBytes {
			return nil, fmt.Errorf("loadprofiles: %s: file size %d exceeds maximum of %d bytes", name, len(data), maxFileBytes)
		}

		var p Profile
		if err := yaml.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("loadprofiles: %s: invalid YAML: %w", name, err)
		}

		if err := validateProfile(&p); err != nil {
			return nil, fmt.Errorf("loadprofiles: %s: %w", name, err)
		}

		if prev, dup := seenIn[p.Name]; dup {
			return nil, fmt.Errorf("loadprofiles: %s: duplicate profile name %q already defined in %s", name, p.Name, prev)
		}
		seenIn[p.Name] = name
		result[p.Name] = &p
	}

	return result, nil
}

// validateProfile checks that a Profile is well-formed.
func validateProfile(p *Profile) error {
	var errs []error

	if p.Name == "" {
		errs = append(errs, errors.New("name: required and must not be empty"))
	}

	for alias, target := range p.Aliases {
		// Alias name must match [a-z][a-z0-9_]* and be <= 32 chars.
		if len(alias) > maxAliasNameLen {
			errs = append(errs, fmt.Errorf("alias %q: name exceeds maximum length of %d", alias, maxAliasNameLen))
		} else if !aliasNameRe.MatchString(alias) {
			errs = append(errs, fmt.Errorf("alias %q: name is invalid (must match [a-z][a-z0-9_]*)", alias))
		}

		// Alias name must not shadow reserved words.
		if reservedWords[alias] {
			errs = append(errs, fmt.Errorf("alias %q: name shadows a reserved word", alias))
		}

		// Alias target must start with "params." and be a valid field path.
		if !IsParamsPath(target) {
			errs = append(errs, fmt.Errorf("alias %q: target %q must start with \"params.\"", alias, target))
		} else if err := ValidateFieldPath(target); err != nil {
			errs = append(errs, fmt.Errorf("alias %q: target %q: invalid field path: %w", alias, target, err))
		}
	}

	return errors.Join(errs...)
}
