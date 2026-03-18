package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Valid(t *testing.T) {
	cfg, err := Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("listen: got %q, want %q", cfg.Listen, ":8080")
	}
	if cfg.RulesDir != "./rules" {
		t.Errorf("rules_dir: got %q, want %q", cfg.RulesDir, "./rules")
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("provider: got %q, want %q", cfg.Provider, "anthropic")
	}
	if cfg.Upstream != "https://api.anthropic.com" {
		t.Errorf("upstream: got %q, want %q", cfg.Upstream, "https://api.anthropic.com")
	}
	if cfg.Scope != "anthropic-gateway" {
		t.Errorf("scope: got %q, want %q", cfg.Scope, "anthropic-gateway")
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log.format: got %q, want %q", cfg.Log.Format, "json")
	}
	if cfg.Log.Output != "stdout" {
		t.Errorf("log.output: got %q, want %q", cfg.Log.Output, "stdout")
	}
	// decompose defaults applied
	if !cfg.Decompose.ToolResultEnabled() {
		t.Error("decompose.tool_result: expected true by default")
	}
	if !cfg.Decompose.ToolUseEnabled() {
		t.Error("decompose.tool_use: expected true by default")
	}
	if cfg.Decompose.TextEnabled() {
		t.Error("decompose.text: expected false by default")
	}
	if !cfg.Decompose.RequestSummaryEnabled() {
		t.Error("decompose.request_summary: expected true by default")
	}
	if !cfg.Decompose.ResponseSummaryEnabled() {
		t.Error("decompose.response_summary: expected true by default")
	}
}

func TestLoad_MissingListen(t *testing.T) {
	y := `
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: test-scope
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing listen, got nil")
	}
}

func TestLoad_MissingRulesDir(t *testing.T) {
	y := `
listen: ":8080"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: test-scope
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing rules_dir, got nil")
	}
}

func TestLoad_MissingProvider(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
upstream: "https://api.anthropic.com"
scope: test-scope
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
}

func TestLoad_MissingUpstream(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
scope: test-scope
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing upstream, got nil")
	}
}

func TestLoad_MissingScope(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing scope, got nil")
	}
}

func TestLoad_InvalidProvider(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
provider: invalid
upstream: "https://api.anthropic.com"
scope: test-scope
`
	path := writeTempYAML(t, y)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
}

func TestLoad_DecomposeDefaults(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway
`
	path := writeTempYAML(t, y)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Decompose.ToolResultEnabled() {
		t.Error("decompose.tool_result: expected true by default")
	}
	if !cfg.Decompose.ToolUseEnabled() {
		t.Error("decompose.tool_use: expected true by default")
	}
	if cfg.Decompose.TextEnabled() {
		t.Error("decompose.text: expected false by default")
	}
	if !cfg.Decompose.RequestSummaryEnabled() {
		t.Error("decompose.request_summary: expected true by default")
	}
	if !cfg.Decompose.ResponseSummaryEnabled() {
		t.Error("decompose.response_summary: expected true by default")
	}
}

func TestLoad_DecomposeOverride(t *testing.T) {
	y := `
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: test-scope
decompose:
  text: true
`
	path := writeTempYAML(t, y)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// overridden value
	if !cfg.Decompose.TextEnabled() {
		t.Error("decompose.text: expected true after override")
	}
	// others keep their defaults
	if !cfg.Decompose.ToolResultEnabled() {
		t.Error("decompose.tool_result: expected true (default)")
	}
	if !cfg.Decompose.ToolUseEnabled() {
		t.Error("decompose.tool_use: expected true (default)")
	}
	if !cfg.Decompose.RequestSummaryEnabled() {
		t.Error("decompose.request_summary: expected true (default)")
	}
	if !cfg.Decompose.ResponseSummaryEnabled() {
		t.Error("decompose.response_summary: expected true (default)")
	}
}

// writeTempYAML writes content to a temp file and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeTempYAML: %v", err)
	}
	return path
}
