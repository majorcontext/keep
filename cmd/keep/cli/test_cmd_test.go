package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	// Reset root command state for each test.
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"test"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestTestCmd_AllPass(t *testing.T) {
	out, err := executeTest(t,
		"testdata/valid-rules",
		"--fixtures", "testdata/fixtures/linear-tests.yaml",
	)
	if err != nil {
		t.Fatalf("expected no error, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected output to contain 'PASS', got: %s", out)
	}
	if !strings.Contains(out, "3 tests") {
		t.Errorf("expected '3 tests' in output, got: %s", out)
	}
	if !strings.Contains(out, "3 passed") {
		t.Errorf("expected '3 passed' in output, got: %s", out)
	}
	if !strings.Contains(out, "0 failed") {
		t.Errorf("expected '0 failed' in output, got: %s", out)
	}
}

func TestTestCmd_SomeFail(t *testing.T) {
	dir := t.TempDir()
	// Fixture expects allow but the rule denies delete_issue.
	content := `
scope: linear-tools
tests:
  - name: "wrong expectation"
    call:
      operation: "delete_issue"
      params:
        issueId: "ISSUE-999"
    expect:
      decision: allow
`
	fp := filepath.Join(dir, "wrong.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeTest(t,
		"testdata/valid-rules",
		"--fixtures", fp,
	)
	// Should return an error because tests failed.
	if err == nil {
		t.Fatalf("expected error due to test failure, got nil\noutput: %s", out)
	}
	combined := out + err.Error()
	if !strings.Contains(combined, "FAIL") {
		t.Errorf("expected 'FAIL' in output, got: %s", combined)
	}
	if !strings.Contains(combined, "expected:") {
		t.Errorf("expected 'expected:' in output, got: %s", combined)
	}
	if !strings.Contains(combined, "got:") {
		t.Errorf("expected 'got:' in output, got: %s", combined)
	}
}

func TestTestCmd_LoadError(t *testing.T) {
	_, err := executeTest(t,
		"testdata/does-not-exist",
		"--fixtures", "testdata/fixtures/linear-tests.yaml",
	)
	if err == nil {
		t.Fatal("expected error for bad rules dir, got nil")
	}
}

func TestTestCmd_BadFixtures(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(fp, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := executeTest(t,
		"testdata/valid-rules",
		"--fixtures", fp,
	)
	if err == nil {
		t.Fatal("expected error for bad fixture file, got nil")
	}
}

func TestTestCmd_EnforceMode(t *testing.T) {
	// Create rules with mode: audit_only and a fixture expecting deny.
	// WithForceEnforce() should override audit_only to enforce, so the deny fires.
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	rulesContent := `
scope: audit-scope
mode: audit_only
rules:
  - name: always-deny
    match:
      operation: "dangerous_op"
    action: deny
    message: "Denied by always-deny."
`
	if err := os.WriteFile(filepath.Join(rulesDir, "rules.yaml"), []byte(rulesContent), 0644); err != nil {
		t.Fatal(err)
	}

	fixturesDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Fixture expects deny — in audit_only without force enforce this would
	// return allow, but with WithForceEnforce it should return deny.
	fixturesContent := `
scope: audit-scope
tests:
  - name: "deny fires in force-enforce mode"
    call:
      operation: "dangerous_op"
      params: {}
    expect:
      decision: deny
      rule: always-deny
`
	fp := filepath.Join(fixturesDir, "test.yaml")
	if err := os.WriteFile(fp, []byte(fixturesContent), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeTest(t, rulesDir, "--fixtures", fp)
	if err != nil {
		t.Fatalf("expected all tests to pass with force-enforce mode, got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected 'PASS' in output, got: %s", out)
	}
	if !strings.Contains(out, "1 passed") {
		t.Errorf("expected '1 passed' in output, got: %s", out)
	}
}
