package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAll_RulesOnly(t *testing.T) {
	result, err := LoadAll("testdata/full/rules", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(result.Scopes))
	}
	if _, ok := result.Scopes["linear-tools"]; !ok {
		t.Error("expected scope \"linear-tools\" to be present")
	}
	if len(result.Profiles) != 0 {
		t.Errorf("expected empty profiles, got %d", len(result.Profiles))
	}
	// ResolvedRules should contain only inline rules (no packs resolved since no packs dir)
	resolved, ok := result.ResolvedRules["linear-tools"]
	if !ok {
		t.Fatal("expected resolved rules for \"linear-tools\"")
	}
	// When no packs dir is provided, pack refs are not resolved;
	// only inline rules appear.
	if len(resolved) != 1 {
		t.Errorf("expected 1 inline rule, got %d", len(resolved))
	}
	if resolved[0].Name != "team-allowlist" {
		t.Errorf("expected rule name \"team-allowlist\", got %q", resolved[0].Name)
	}
}

func TestLoadAll_WithProfiles(t *testing.T) {
	result, err := LoadAll("testdata/full/rules", "testdata/full/profiles", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(result.Scopes))
	}
	if len(result.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(result.Profiles))
	}
	if _, ok := result.Profiles["linear"]; !ok {
		t.Error("expected profile \"linear\" to be present")
	}
}

func TestLoadAll_WithPacks(t *testing.T) {
	result, err := LoadAll("testdata/full/rules", "", "testdata/full/packs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, ok := result.ResolvedRules["linear-tools"]
	if !ok {
		t.Fatal("expected resolved rules for \"linear-tools\"")
	}
	// Pack has 3 rules; no-auto-p0 is disabled by override; + 1 inline = 3 total
	if len(resolved) != 3 {
		t.Errorf("expected 3 resolved rules (2 pack + 1 inline), got %d", len(resolved))
	}
	// First rule should be from the pack
	if resolved[0].Name != "no-delete" {
		t.Errorf("first rule = %q, want \"no-delete\"", resolved[0].Name)
	}
	// no-auto-p0 should be disabled (not present)
	for _, r := range resolved {
		if r.Name == "no-auto-p0" {
			t.Error("rule \"no-auto-p0\" should have been disabled by override")
		}
	}
	// Last rule should be the inline one
	last := resolved[len(resolved)-1]
	if last.Name != "team-allowlist" {
		t.Errorf("last rule = %q, want \"team-allowlist\"", last.Name)
	}
}

func TestLoadAll_Full(t *testing.T) {
	result, err := LoadAll("testdata/full/rules", "testdata/full/profiles", "testdata/full/packs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 {
		t.Errorf("expected 1 scope, got %d", len(result.Scopes))
	}
	if len(result.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(result.Profiles))
	}
	if _, ok := result.Profiles["linear"]; !ok {
		t.Error("expected profile \"linear\"")
	}
	resolved, ok := result.ResolvedRules["linear-tools"]
	if !ok {
		t.Fatal("expected resolved rules for \"linear-tools\"")
	}
	if len(resolved) != 3 {
		t.Errorf("expected 3 resolved rules, got %d", len(resolved))
	}
}

func TestLoadAll_PackNotFound(t *testing.T) {
	// rules dir references "linear-safe-defaults" pack, but we supply an empty packs dir
	dir := t.TempDir()
	packsEmpty := filepath.Join(dir, "packs-empty")
	if err := os.MkdirAll(packsEmpty, 0755); err != nil {
		t.Fatalf("failed to create empty packs dir: %v", err)
	}
	_, err := LoadAll("testdata/full/rules", "", packsEmpty)
	if err == nil {
		t.Fatal("expected error when pack not found, got nil")
	}
	if !strings.Contains(err.Error(), "linear-safe-defaults") {
		t.Errorf("expected error to mention pack name, got: %v", err)
	}
}

func TestLoadAll_InvalidRules(t *testing.T) {
	dir := t.TempDir()
	badYAML := []byte("scope: bad\nrules: [[[not valid yaml")
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), badYAML, 0644); err != nil {
		t.Fatalf("failed to write bad yaml: %v", err)
	}
	_, err := LoadAll(dir, "", "")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadAll_ProfileCrossRefMissing(t *testing.T) {
	// Rule file references a profile that does not exist in the profiles dir.
	rulesDir := t.TempDir()
	profilesDir := t.TempDir()

	ruleContent := `scope: test-scope
profile: nonexistent-profile
rules:
  - name: test-rule
    action: deny
    message: "blocked"
`
	if err := os.WriteFile(filepath.Join(rulesDir, "rules.yaml"), []byte(ruleContent), 0644); err != nil {
		t.Fatalf("failed to write rules.yaml: %v", err)
	}

	_, err := LoadAll(rulesDir, profilesDir, "")
	if err == nil {
		t.Fatal("expected error for missing profile cross-ref, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-profile") {
		t.Errorf("expected error to mention missing profile name, got: %v", err)
	}
}

func TestLoadAll_ProfileCrossRefValid(t *testing.T) {
	// Rule file references a profile that exists — should succeed.
	rulesDir := t.TempDir()
	profilesDir := t.TempDir()

	ruleContent := `scope: test-scope
profile: my-profile
rules:
  - name: test-rule
    action: deny
    message: "blocked"
`
	profileContent := `name: my-profile
aliases:
  foo: "params.fooId"
`
	if err := os.WriteFile(filepath.Join(rulesDir, "rules.yaml"), []byte(ruleContent), 0644); err != nil {
		t.Fatalf("failed to write rules.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "my-profile.yaml"), []byte(profileContent), 0644); err != nil {
		t.Fatalf("failed to write my-profile.yaml: %v", err)
	}

	_, err := LoadAll(rulesDir, profilesDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAll_PackProfileCrossRefMissing(t *testing.T) {
	// A pack references a profile that does not exist in the profiles dir.
	rulesDir := t.TempDir()
	profilesDir := t.TempDir()
	packsDir := t.TempDir()

	ruleContent := `scope: test-scope
rules:
  - name: test-rule
    action: deny
    message: "blocked"
`
	packContent := `name: my-pack
profile: ghost-profile
rules:
  - name: pack-rule
    action: deny
    message: "blocked"
`
	if err := os.WriteFile(filepath.Join(rulesDir, "rules.yaml"), []byte(ruleContent), 0644); err != nil {
		t.Fatalf("failed to write rules.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "my-pack.yaml"), []byte(packContent), 0644); err != nil {
		t.Fatalf("failed to write my-pack.yaml: %v", err)
	}

	_, err := LoadAll(rulesDir, profilesDir, packsDir)
	if err == nil {
		t.Fatal("expected error for missing pack profile cross-ref, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-profile") {
		t.Errorf("expected error to mention missing profile name, got: %v", err)
	}
}
