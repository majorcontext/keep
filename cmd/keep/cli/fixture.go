package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FixtureFile is a parsed test fixture file.
type FixtureFile struct {
	Path  string     // source file path (set during loading, not in YAML)
	Scope string     `yaml:"scope"`
	Tests []TestCase `yaml:"tests"`
}

// TestCase is a single test: a call and the expected result.
type TestCase struct {
	Name   string      `yaml:"name"`
	Call   FixtureCall `yaml:"call"`
	Expect Expectation `yaml:"expect"`
}

// FixtureCall is the call to evaluate.
type FixtureCall struct {
	Operation string          `yaml:"operation"`
	Params    map[string]any  `yaml:"params"`
	Context   *FixtureContext `yaml:"context,omitempty"`
}

// FixtureContext is optional context overrides.
type FixtureContext struct {
	AgentID   string            `yaml:"agent_id,omitempty"`
	UserID    string            `yaml:"user_id,omitempty"`
	Scope     string            `yaml:"scope,omitempty"`
	Direction string            `yaml:"direction,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
	Timestamp string            `yaml:"timestamp,omitempty"` // RFC3339 format
}

// Expectation is the expected evaluation result.
type Expectation struct {
	Decision  string             `yaml:"decision"`
	Rule      string             `yaml:"rule,omitempty"`
	Message   string             `yaml:"message,omitempty"`
	Mutations []ExpectedMutation `yaml:"mutations,omitempty"`
}

// ExpectedMutation is an expected mutation from a redact action.
type ExpectedMutation struct {
	Path     string `yaml:"path"`
	Replaced string `yaml:"replaced"`
}

// LoadFixtures reads and parses fixture files from a path.
// Path can be a single .yaml/.yml file or a directory (loads all yaml files).
func LoadFixtures(path string) ([]FixtureFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		f, err := parseFixtureFile(path)
		if err != nil {
			return nil, err
		}
		return []FixtureFile{f}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", path, err)
	}

	var files []FixtureFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		fp := filepath.Join(path, entry.Name())
		f, err := parseFixtureFile(fp)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, nil
}

func parseFixtureFile(path string) (FixtureFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FixtureFile{}, fmt.Errorf("read file %q: %w", path, err)
	}

	var f FixtureFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return FixtureFile{}, fmt.Errorf("parse %q: %w", path, err)
	}
	f.Path = path

	for i, tc := range f.Tests {
		if tc.Expect.Decision == "" {
			return FixtureFile{}, fmt.Errorf("test %q in %q: expect.decision must not be empty", tc.Name, path)
		}

		// Ensure context is initialised.
		if tc.Call.Context == nil {
			tc.Call.Context = &FixtureContext{}
		}

		// Apply file-level scope default when test case has no scope.
		if tc.Call.Context.Scope == "" && f.Scope != "" {
			tc.Call.Context.Scope = f.Scope
		}

		// Default agent_id.
		if tc.Call.Context.AgentID == "" {
			tc.Call.Context.AgentID = "test"
		}

		f.Tests[i] = tc
	}

	return f, nil
}
