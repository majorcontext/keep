package cli

import (
	"bytes"
	"strings"
	"testing"
)

func executeValidate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	// Reset root command state for each test.
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"validate"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestValidateCmd_ValidDir(t *testing.T) {
	out, err := executeValidate(t, "testdata/valid-rules")
	if err != nil {
		t.Fatalf("validate valid dir: unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "2 scopes") {
		t.Errorf("expected output to contain '2 scopes', got: %s", out)
	}
	if !strings.Contains(out, "0 errors") {
		t.Errorf("expected output to contain '0 errors', got: %s", out)
	}
}

func TestValidateCmd_InvalidRules(t *testing.T) {
	out, err := executeValidate(t, "testdata/invalid-rules")
	if err == nil {
		t.Fatalf("validate invalid rules: expected error, got nil\noutput: %s", out)
	}
	// Output should contain some error context.
	combined := out + err.Error()
	if !strings.Contains(combined, "bad") && !strings.Contains(combined, "error") && !strings.Contains(combined, "Error") {
		t.Errorf("expected error context in output or error, got output: %s, err: %v", out, err)
	}
}

func TestValidateCmd_NonexistentDir(t *testing.T) {
	out, err := executeValidate(t, "testdata/does-not-exist")
	if err == nil {
		t.Fatalf("validate nonexistent dir: expected error, got nil\noutput: %s", out)
	}
}

func TestValidateCmd_WithProfiles(t *testing.T) {
	// Use an empty temp dir for profiles so the flag is accepted without errors.
	profilesDir := t.TempDir()
	out, err := executeValidate(t, "--profiles", profilesDir, "testdata/valid-rules")
	if err != nil {
		t.Fatalf("validate with --profiles: unexpected error: %v\noutput: %s", err, out)
	}
}

func TestValidateCmd_WithPacks(t *testing.T) {
	// Use an empty temp dir for packs so the flag is accepted without errors.
	packsDir := t.TempDir()
	out, err := executeValidate(t, "--packs", packsDir, "testdata/valid-rules")
	if err != nil {
		t.Fatalf("validate with --packs: unexpected error: %v\noutput: %s", err, out)
	}
}
