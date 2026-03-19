package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFixtures_ValidFile(t *testing.T) {
	path := filepath.Join("testdata", "fixtures", "linear-tests.yaml")
	files, err := LoadFixtures(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 fixture file, got %d", len(files))
	}
	f := files[0]
	if f.Scope != "linear-tools" {
		t.Errorf("expected scope 'linear-tools', got %q", f.Scope)
	}
	if len(f.Tests) != 3 {
		t.Errorf("expected 3 tests, got %d", len(f.Tests))
	}
	if f.Path != path {
		t.Errorf("expected Path %q, got %q", path, f.Path)
	}
}

func TestLoadFixtures_DefaultScope(t *testing.T) {
	dir := t.TempDir()
	content := `
scope: my-scope
tests:
  - name: "test with no context scope"
    call:
      operation: "do_thing"
      params: {}
    expect:
      decision: allow
  - name: "test with explicit context scope"
    call:
      operation: "do_thing"
      params: {}
      context:
        scope: "other-scope"
    expect:
      decision: allow
`
	fp := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadFixtures(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// First test: no context scope, should inherit file-level scope
	tc0 := files[0].Tests[0]
	if tc0.Call.Context == nil {
		t.Fatal("expected context to be set after applying defaults")
	}
	if tc0.Call.Context.Scope != "my-scope" {
		t.Errorf("expected scope 'my-scope' from file default, got %q", tc0.Call.Context.Scope)
	}

	// Second test: explicit context scope, should not be overridden
	tc1 := files[0].Tests[1]
	if tc1.Call.Context == nil {
		t.Fatal("expected context to be set")
	}
	if tc1.Call.Context.Scope != "other-scope" {
		t.Errorf("expected scope 'other-scope', got %q", tc1.Call.Context.Scope)
	}
}

func TestLoadFixtures_DefaultContext(t *testing.T) {
	dir := t.TempDir()
	content := `
scope: test-scope
tests:
  - name: "test with no context at all"
    call:
      operation: "do_thing"
      params: {}
    expect:
      decision: allow
`
	fp := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadFixtures(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := files[0].Tests[0]
	if tc.Call.Context == nil {
		t.Fatal("expected context to be initialized")
	}
	if tc.Call.Context.AgentID != "test" {
		t.Errorf("expected default agent_id 'test', got %q", tc.Call.Context.AgentID)
	}
}

func TestLoadFixtures_MissingDecision(t *testing.T) {
	dir := t.TempDir()
	content := `
scope: test-scope
tests:
  - name: "test missing decision"
    call:
      operation: "do_thing"
      params: {}
    expect:
      rule: some-rule
`
	fp := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFixtures(fp)
	if err == nil {
		t.Fatal("expected validation error for missing decision, got nil")
	}
}

func TestLoadFixtures_Directory(t *testing.T) {
	dir := t.TempDir()

	file1 := `
scope: scope-a
tests:
  - name: "test a"
    call:
      operation: "op_a"
      params: {}
    expect:
      decision: allow
`
	file2 := `
scope: scope-b
tests:
  - name: "test b1"
    call:
      operation: "op_b"
      params: {}
    expect:
      decision: deny
  - name: "test b2"
    call:
      operation: "op_c"
      params: {}
    expect:
      decision: allow
`
	notYaml := "this should not be loaded"

	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(file1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(file2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(notYaml), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadFixtures(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 fixture files, got %d", len(files))
	}

	totalTests := 0
	for _, f := range files {
		totalTests += len(f.Tests)
	}
	if totalTests != 3 {
		t.Errorf("expected 3 total tests across files, got %d", totalTests)
	}
}

func TestLoadFixtures_Timestamp(t *testing.T) {
	dir := t.TempDir()
	content := `
scope: test-scope
tests:
  - name: "test with explicit timestamp"
    call:
      operation: "do_thing"
      params: {}
      context:
        timestamp: "2026-03-18T02:00:00Z"
    expect:
      decision: allow
  - name: "test with no timestamp"
    call:
      operation: "do_thing"
      params: {}
    expect:
      decision: allow
`
	fp := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadFixtures(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// First test: explicit timestamp should be preserved.
	tc0 := files[0].Tests[0]
	if tc0.Call.Context == nil {
		t.Fatal("expected context to be set")
	}
	if tc0.Call.Context.Timestamp != "2026-03-18T02:00:00Z" {
		t.Errorf("expected timestamp '2026-03-18T02:00:00Z', got %q", tc0.Call.Context.Timestamp)
	}

	// Second test: no timestamp specified, field should be empty.
	tc1 := files[0].Tests[1]
	if tc1.Call.Context == nil {
		t.Fatal("expected context to be initialized")
	}
	if tc1.Call.Context.Timestamp != "" {
		t.Errorf("expected empty timestamp, got %q", tc1.Call.Context.Timestamp)
	}
}

func TestLoadFixtures_SingleFile(t *testing.T) {
	dir := t.TempDir()
	content := `
scope: single-scope
tests:
  - name: "only test"
    call:
      operation: "op"
      params: {}
    expect:
      decision: allow
`
	fp := filepath.Join(dir, "single.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Also put another yaml in same dir to confirm only single file loaded
	other := `
scope: other-scope
tests:
  - name: "other test"
    call:
      operation: "op2"
      params: {}
    expect:
      decision: deny
`
	if err := os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadFixtures(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file when given single file path, got %d", len(files))
	}
	if files[0].Scope != "single-scope" {
		t.Errorf("expected scope 'single-scope', got %q", files[0].Scope)
	}
}
