package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules_ValidDir(t *testing.T) {
	result, err := LoadRules("testdata/rules")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 rule files, got %d", len(result))
	}
	if _, ok := result["linear-tools"]; !ok {
		t.Error("expected scope \"linear-tools\" to be present")
	}
	if _, ok := result["slack-tools"]; !ok {
		t.Error("expected scope \"slack-tools\" to be present")
	}
	linear := result["linear-tools"]
	if len(linear.Rules) != 2 {
		t.Errorf("expected 2 rules in linear-tools, got %d", len(linear.Rules))
	}
	slack := result["slack-tools"]
	if len(slack.Rules) != 1 {
		t.Errorf("expected 1 rule in slack-tools, got %d", len(slack.Rules))
	}
}

func TestLoadRules_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadRules(dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func TestLoadRules_DuplicateScope(t *testing.T) {
	_, err := LoadRules("testdata/rules-duplicate-scope")
	if err == nil {
		t.Fatal("expected error for duplicate scope, got nil")
	}
	// Error should mention the duplicate scope name
	errStr := err.Error()
	if !contains(errStr, "duplicate-scope") {
		t.Errorf("expected error to mention scope name \"duplicate-scope\", got: %s", errStr)
	}
}

func TestLoadRules_NonexistentDir(t *testing.T) {
	_, err := LoadRules("testdata/does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
}

func TestLoadRules_SkipsNonYaml(t *testing.T) {
	dir := t.TempDir()

	// Write a non-yaml file
	mdPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(mdPath, []byte("# readme"), 0644); err != nil {
		t.Fatalf("failed to write md file: %v", err)
	}

	// Write a valid yaml rule file
	yamlContent := `scope: only-yaml-scope
rules:
  - name: test-rule
    action: deny
    message: "test"
`
	yamlPath := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml file: %v", err)
	}

	result, err := LoadRules(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 rule file, got %d", len(result))
	}
	if _, ok := result["only-yaml-scope"]; !ok {
		t.Error("expected scope \"only-yaml-scope\" to be present")
	}
}

// contains is a helper since strings.Contains is in strings package.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
