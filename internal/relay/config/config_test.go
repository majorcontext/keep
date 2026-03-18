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
	if cfg.Listen != ":8090" {
		t.Errorf("listen: got %q, want %q", cfg.Listen, ":8090")
	}
	if cfg.RulesDir != "./rules" {
		t.Errorf("rules_dir: got %q, want %q", cfg.RulesDir, "./rules")
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("routes: got %d, want 2", len(cfg.Routes))
	}

	r0 := cfg.Routes[0]
	if r0.Scope != "linear-tools" {
		t.Errorf("routes[0].scope: got %q, want %q", r0.Scope, "linear-tools")
	}
	if r0.Upstream != "https://mcp.linear.app/mcp" {
		t.Errorf("routes[0].upstream: got %q, want %q", r0.Upstream, "https://mcp.linear.app/mcp")
	}
	if r0.Auth == nil {
		t.Fatal("routes[0].auth: expected non-nil")
	}
	if r0.Auth.Type != "bearer" {
		t.Errorf("routes[0].auth.type: got %q, want %q", r0.Auth.Type, "bearer")
	}
	if r0.Auth.TokenEnv != "LINEAR_API_KEY" {
		t.Errorf("routes[0].auth.token_env: got %q, want %q", r0.Auth.TokenEnv, "LINEAR_API_KEY")
	}

	r1 := cfg.Routes[1]
	if r1.Scope != "slack-tools" {
		t.Errorf("routes[1].scope: got %q, want %q", r1.Scope, "slack-tools")
	}
	if r1.Auth != nil {
		t.Errorf("routes[1].auth: expected nil, got %+v", r1.Auth)
	}
}

func TestLoad_MissingListen(t *testing.T) {
	yaml := `
rules_dir: "./rules"
routes:
  - scope: foo
    upstream: "https://example.com"
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing listen, got nil")
	}
}

func TestLoad_MissingRulesDir(t *testing.T) {
	yaml := `
listen: ":8090"
routes:
  - scope: foo
    upstream: "https://example.com"
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing rules_dir, got nil")
	}
}

func TestLoad_MissingRoutes(t *testing.T) {
	yaml := `
listen: ":8090"
rules_dir: "./rules"
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for nil routes, got nil")
	}
}

func TestLoad_EmptyRoutes(t *testing.T) {
	yaml := `
listen: ":8090"
rules_dir: "./rules"
routes: []
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty routes, got nil")
	}
}

func TestLoad_RouteMissingScope(t *testing.T) {
	yaml := `
listen: ":8090"
rules_dir: "./rules"
routes:
  - upstream: "https://example.com"
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for route missing scope, got nil")
	}
}

func TestLoad_RouteMissingUpstream(t *testing.T) {
	yaml := `
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: foo
`
	path := writeTempYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for route missing upstream, got nil")
	}
}

func TestLoad_AuthTypes(t *testing.T) {
	tests := []struct {
		name     string
		authYAML string
		wantType string
	}{
		{
			name: "bearer",
			authYAML: `
      type: bearer
      token_env: MY_TOKEN`,
			wantType: "bearer",
		},
		{
			name: "header",
			authYAML: `
      type: header
      header: X-Api-Key`,
			wantType: "header",
		},
		{
			name: "passthrough",
			authYAML: `
      type: passthrough`,
			wantType: "passthrough",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			y := `
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: test-scope
    upstream: "https://example.com"
    auth:` + tc.authYAML + "\n"
			path := writeTempYAML(t, y)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.Routes) != 1 {
				t.Fatalf("expected 1 route, got %d", len(cfg.Routes))
			}
			auth := cfg.Routes[0].Auth
			if auth == nil {
				t.Fatal("expected non-nil auth")
			}
			if auth.Type != tc.wantType {
				t.Errorf("auth.type: got %q, want %q", auth.Type, tc.wantType)
			}
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	yaml := `
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: foo
    upstream: "https://example.com"
`
	path := writeTempYAML(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log.format default: got %q, want %q", cfg.Log.Format, "json")
	}
	if cfg.Log.Output != "stdout" {
		t.Errorf("log.output default: got %q, want %q", cfg.Log.Output, "stdout")
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
