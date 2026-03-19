package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if !strings.Contains(errStr, "duplicate-scope") {
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

func TestLoadRules_AccumulatesErrors(t *testing.T) {
	dir := t.TempDir()

	// Write two invalid yaml rule files
	bad1 := `scope: bad-scope-1
rules:
  - name: "BadName"
    action: deny
`
	bad2 := `scope: bad-scope-2
rules:
  - name: "BadName2"
    action: deny
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(bad1), 0644); err != nil {
		t.Fatalf("failed to write a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(bad2), 0644); err != nil {
		t.Fatalf("failed to write b.yaml: %v", err)
	}

	_, err := LoadRules(dir)
	if err == nil {
		t.Fatal("expected error for invalid files, got nil")
	}
	errStr := err.Error()
	// Both files should be mentioned
	if !strings.Contains(errStr, "a.yaml") {
		t.Errorf("expected error to mention a.yaml, got: %s", errStr)
	}
	if !strings.Contains(errStr, "b.yaml") {
		t.Errorf("expected error to mention b.yaml, got: %s", errStr)
	}
}

func TestLoadRules_WithDefs(t *testing.T) {
	result, err := LoadRules("testdata/rules-with-defs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rf, ok := result["test-defs"]
	if !ok {
		t.Fatal("expected scope test-defs to be present")
	}
	if len(rf.Defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(rf.Defs))
	}
	if rf.Defs["allowed_teams"] != "['TEAM-ENG', 'TEAM-INFRA']" {
		t.Errorf("allowed_teams = %q, want %q", rf.Defs["allowed_teams"], "['TEAM-ENG', 'TEAM-INFRA']")
	}
	if rf.Defs["max_priority"] != "1" {
		t.Errorf("max_priority = %q, want %q", rf.Defs["max_priority"], "1")
	}
}

func TestLoadRules_FileSizeCap(t *testing.T) {
	dir := t.TempDir()

	// Write a file larger than maxFileBytes
	big := make([]byte, maxFileBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.yaml"), big, 0644); err != nil {
		t.Fatalf("failed to write big.yaml: %v", err)
	}

	_, err := LoadRules(dir)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected error to mention size limit, got: %v", err)
	}
}
