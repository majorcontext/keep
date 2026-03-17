package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfiles_Valid(t *testing.T) {
	profiles, err := LoadProfiles("testdata/profiles")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	linear, ok := profiles["linear"]
	if !ok {
		t.Fatalf("expected profile %q not found; got keys: %v", "linear", profileKeys(profiles))
	}
	expectedAliases := map[string]string{
		"team":        "params.teamId",
		"assignee":    "params.assigneeId",
		"priority":    "params.priority",
		"title":       "params.title",
		"description": "params.description",
	}
	for alias, target := range expectedAliases {
		got, ok := linear.Aliases[alias]
		if !ok {
			t.Errorf("alias %q missing from linear profile", alias)
			continue
		}
		if got != target {
			t.Errorf("alias %q: expected %q, got %q", alias, target, got)
		}
	}
}

func TestLoadProfiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	profiles, err := LoadProfiles(dir)
	if err != nil {
		t.Fatalf("expected no error for empty dir, got: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected empty map, got %d entries", len(profiles))
	}
}

func TestLoadProfiles_NonexistentDir(t *testing.T) {
	profiles, err := LoadProfiles("/tmp/does-not-exist-profile-dir-xyz")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected empty map, got %d entries", len(profiles))
	}
}

func TestLoadProfiles_BadAliasName(t *testing.T) {
	dir := t.TempDir()
	writeProfileFile(t, dir, "bad.yaml", `
name: bad
aliases:
  Team: "params.teamId"
`)
	_, err := LoadProfiles(dir)
	if err == nil {
		t.Fatal("expected error for uppercase alias name, got nil")
	}
}

func TestLoadProfiles_AliasNotParams(t *testing.T) {
	dir := t.TempDir()
	writeProfileFile(t, dir, "bad.yaml", `
name: bad
aliases:
  agent: "context.agent_id"
`)
	_, err := LoadProfiles(dir)
	if err == nil {
		t.Fatal("expected error for alias targeting non-params path, got nil")
	}
}

func TestLoadProfiles_AliasOverlong(t *testing.T) {
	dir := t.TempDir()
	// alias name > 32 chars
	longName := "abcdefghijklmnopqrstuvwxyz1234567" // 33 chars
	writeProfileFile(t, dir, "bad.yaml", "name: bad\naliases:\n  "+longName+": \"params.foo\"\n")
	_, err := LoadProfiles(dir)
	if err == nil {
		t.Fatal("expected error for overlong alias name, got nil")
	}
}

func TestLoadProfiles_ShadowsBuiltin(t *testing.T) {
	builtins := []string{"size", "has", "contains"}
	for _, builtin := range builtins {
		t.Run(builtin, func(t *testing.T) {
			dir := t.TempDir()
			writeProfileFile(t, dir, "bad.yaml", "name: bad\naliases:\n  "+builtin+": \"params.foo\"\n")
			_, err := LoadProfiles(dir)
			if err == nil {
				t.Fatalf("expected error for alias shadowing builtin %q, got nil", builtin)
			}
		})
	}
}

func TestLoadProfiles_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeProfileFile(t, dir, "a.yaml", `
name: myprofile
aliases:
  team: "params.teamId"
`)
	writeProfileFile(t, dir, "b.yaml", `
name: myprofile
aliases:
  assignee: "params.assigneeId"
`)
	_, err := LoadProfiles(dir)
	if err == nil {
		t.Fatal("expected error for duplicate profile name, got nil")
	}
}

// helpers

func writeProfileFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeProfileFile: %v", err)
	}
}

func profileKeys(m map[string]*Profile) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
