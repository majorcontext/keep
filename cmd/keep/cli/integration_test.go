//go:build integration

package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Build the binary to a temp location.
	tmp, err := os.MkdirTemp("", "keep-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "keep")
	cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/majorcontext/keep/cmd/keep")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(string(out))
	}

	os.Exit(m.Run())
}

func runKeep(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run keep: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestIntegration_Version(t *testing.T) {
	stdout, _, exitCode := runKeep(t, "version")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("expected stdout to contain 'dev', got: %s", stdout)
	}
}

func TestIntegration_ValidateSuccess(t *testing.T) {
	stdout, _, exitCode := runKeep(t, "validate", "testdata/valid-rules")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("expected stdout to contain 'OK', got: %s", stdout)
	}
	if !strings.Contains(stdout, "0 errors") {
		t.Errorf("expected stdout to contain '0 errors', got: %s", stdout)
	}
}

func TestIntegration_ValidateFailure(t *testing.T) {
	stdout, stderr, exitCode := runKeep(t, "validate", "testdata/invalid-rules")
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit, got 0\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "error") && !strings.Contains(combined, "Error") {
		t.Errorf("expected error info in output, got stdout: %s, stderr: %s", stdout, stderr)
	}
}

func TestIntegration_ValidateNonexistent(t *testing.T) {
	_, _, exitCode := runKeep(t, "validate", "/nonexistent")
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit for nonexistent path, got 0")
	}
}

func TestIntegration_TestAllPass(t *testing.T) {
	stdout, _, exitCode := runKeep(t,
		"test", "testdata/valid-rules",
		"--fixtures", "testdata/fixtures/linear-tests.yaml",
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PASS") {
		t.Errorf("expected stdout to contain 'PASS', got: %s", stdout)
	}
	if !strings.Contains(stdout, "0 failed") {
		t.Errorf("expected stdout to contain '0 failed', got: %s", stdout)
	}
}

func TestIntegration_TestSomeFail(t *testing.T) {
	dir := t.TempDir()
	// Fixture expects allow but the rule denies delete_issue.
	content := `scope: linear-tools
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

	stdout, _, exitCode := runKeep(t,
		"test", "testdata/valid-rules",
		"--fixtures", fp,
	)
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "FAIL") {
		t.Errorf("expected stdout to contain 'FAIL', got: %s", stdout)
	}
}

func TestIntegration_TestNoFixturesFlag(t *testing.T) {
	_, stderr, exitCode := runKeep(t, "test", "testdata/valid-rules")
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit when --fixtures is missing, got 0")
	}
	if !strings.Contains(stderr, "fixtures") {
		t.Errorf("expected stderr to mention 'fixtures', got: %s", stderr)
	}
}

func TestIntegration_Help(t *testing.T) {
	stdout, _, exitCode := runKeep(t, "--help")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	for _, want := range []string{"validate", "test", "version"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected stdout to contain %q, got: %s", want, stdout)
		}
	}
}

func TestIntegration_ValidateWithProfilesAndPacks(t *testing.T) {
	profilesDir := t.TempDir()
	packsDir := t.TempDir()
	stdout, _, exitCode := runKeep(t,
		"validate", "testdata/valid-rules",
		"--profiles", profilesDir,
		"--packs", packsDir,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("expected stdout to contain 'OK', got: %s", stdout)
	}
}
