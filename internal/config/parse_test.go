package config

import (
	"strings"
	"testing"
)

func TestParseRuleFile_Valid(t *testing.T) {
	data := []byte(`
scope: test-scope
mode: enforce
rules:
  - name: block-deletes
    match:
      operation: "delete_*"
    action: deny
    message: "no deletes"
`)
	rf, err := ParseRuleFile(data)
	if err != nil {
		t.Fatalf("ParseRuleFile() error: %v", err)
	}
	if rf.Scope != "test-scope" {
		t.Errorf("Scope = %q, want test-scope", rf.Scope)
	}
	if rf.Mode != ModeEnforce {
		t.Errorf("Mode = %q, want enforce", rf.Mode)
	}
	if len(rf.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1", len(rf.Rules))
	}
	if rf.Rules[0].Name != "block-deletes" {
		t.Errorf("Rules[0].Name = %q, want block-deletes", rf.Rules[0].Name)
	}
}

func TestParseRuleFile_Empty(t *testing.T) {
	_, err := ParseRuleFile([]byte{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "empty input") {
		t.Errorf("error = %q, want mention of empty input", err)
	}
}

func TestParseRuleFile_InvalidYAML(t *testing.T) {
	_, err := ParseRuleFile([]byte("{{{}}}"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Errorf("error = %q, want mention of invalid YAML", err)
	}
}

func TestParseRuleFile_ValidationError(t *testing.T) {
	// Missing scope.
	_, err := ParseRuleFile([]byte(`
rules:
  - name: test
    match:
      operation: "foo"
    action: deny
`))
	if err == nil {
		t.Fatal("expected error for missing scope")
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Errorf("error = %q, want mention of scope", err)
	}
}

func TestParseRuleFile_VersionV1(t *testing.T) {
	data := []byte(`
version: v1
scope: test-scope
rules:
  - name: block-deletes
    action: deny
`)
	rf, err := ParseRuleFile(data)
	if err != nil {
		t.Fatalf("ParseRuleFile() error: %v", err)
	}
	if rf.Version != "v1" {
		t.Errorf("Version = %q, want v1", rf.Version)
	}
}

func TestParseRuleFile_NoVersionDefaultsToV1(t *testing.T) {
	data := []byte(`
scope: test-scope
rules:
  - name: block-deletes
    action: deny
`)
	rf, err := ParseRuleFile(data)
	if err != nil {
		t.Fatalf("ParseRuleFile() error: %v", err)
	}
	if rf.Version != "v1" {
		t.Errorf("Version = %q, want v1", rf.Version)
	}
}

func TestParseRuleFile_UnsupportedVersion(t *testing.T) {
	data := []byte(`
version: v2
scope: test-scope
rules:
  - name: block-deletes
    action: deny
`)
	_, err := ParseRuleFile(data)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if !strings.Contains(err.Error(), `unsupported rule file version "v2"`) {
		t.Errorf("error = %q, want mention of unsupported version v2", err)
	}
	if !strings.Contains(err.Error(), "supported: v1") {
		t.Errorf("error = %q, want mention of supported: v1", err)
	}
}

func TestParseRuleFile_PacksRejected(t *testing.T) {
	_, err := ParseRuleFile([]byte(`
scope: test-scope
packs:
  - name: some-pack
rules:
  - name: test
    match:
      operation: "foo"
    action: deny
`))
	if err == nil {
		t.Fatal("expected error for pack references")
	}
	if !strings.Contains(err.Error(), "pack references are not supported") {
		t.Errorf("error = %q, want mention of pack references", err)
	}
}
