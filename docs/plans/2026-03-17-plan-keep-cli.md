# `keep` CLI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `keep` CLI with `validate` and `test` commands for checking rule files and testing policy against fixtures.

**Architecture:** Thin CLI shell using Cobra. All logic lives in the engine (`keep.Load`) and config packages. The CLI handles argument parsing, output formatting, and exit codes. The test command loads YAML fixture files and evaluates calls against loaded rules.

**Tech Stack:** Go, `github.com/spf13/cobra` for CLI, `keep` engine package for evaluation.

**Depends on:** Config (sub-project 1) and Engine (sub-project 2) must be complete.

---

### Task 1: Cobra skeleton with version command

**Files:**
- Create: `cmd/keep/main.go`
- Create: `cmd/keep/cli/root.go`
- Create: `cmd/keep/cli/version.go`

- [ ] **Step 1: Add cobra dependency**

Run: `go get github.com/spf13/cobra`

- [ ] **Step 2: Create main.go**

```go
package main

import (
	"os"

	"github.com/majorcontext/keep/cmd/keep/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Create root command**

Create `cmd/keep/cli/root.go`:

```go
package cli

import "github.com/spf13/cobra"

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "keep",
	Short: "API-level policy engine for AI agents",
}

func Execute() error {
	return rootCmd.Execute()
}
```

- [ ] **Step 4: Create version command**

Create `cmd/keep/cli/version.go` that prints version, commit, date.

- [ ] **Step 5: Build and verify**

Run: `go build -o keep ./cmd/keep && ./keep version`
Expected: prints version info

- [ ] **Step 6: Commit**

```bash
git add cmd/keep/ go.mod go.sum
git commit -m "feat(cli): add keep CLI skeleton with version command"
```

---

### Task 2: `keep validate` command

**Files:**
- Create: `cmd/keep/cli/validate.go`
- Create: `cmd/keep/cli/validate_test.go`
- Create: `cmd/keep/cli/testdata/valid-rules/linear.yaml`
- Create: `cmd/keep/cli/testdata/invalid-rules/bad-expression.yaml`

- [ ] **Step 1: Write failing tests**

Create `cmd/keep/cli/validate_test.go`:
- `TestValidateCmd_ValidDir` -- runs validate against valid rules dir, exit 0, output shows file count and rule count
- `TestValidateCmd_InvalidRules` -- runs against invalid rules, exit 1, output shows errors with file and context
- `TestValidateCmd_NonexistentDir` -- exit 2, error message
- `TestValidateCmd_WithProfiles` -- `--profiles` flag works
- `TestValidateCmd_WithPacks` -- `--packs` flag works

Test approach: execute the Cobra command programmatically, capture stdout/stderr, check exit behavior.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestValidateCmd'`
Expected: FAIL

- [ ] **Step 3: Implement validate command**

Create `cmd/keep/cli/validate.go`:

```go
var validateCmd = &cobra.Command{
	Use:   "validate <rules-dir>",
	Short: "Validate rule files, profiles, and starter packs",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	validateCmd.Flags().String("profiles", "", "Path to profiles directory")
	validateCmd.Flags().String("packs", "", "Path to starter packs directory")
	rootCmd.AddCommand(validateCmd)
}
```

`runValidate`:
1. Call `keep.Load(rulesDir, opts...)` -- if it returns nil error, rules are valid
2. On success: print per-file summary (scope name, rule count, profile if set)
3. On error: print errors with file context, return exit code 1
4. On filesystem error: exit code 2

Output format matches PRD CLI spec.

- [ ] **Step 4: Create testdata**

Valid rules and intentionally invalid rules for CLI tests.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestValidateCmd'`
Expected: PASS

- [ ] **Step 6: Manual smoke test**

Run: `go build -o keep ./cmd/keep && ./keep validate testdata/rules`
Expected: success output

- [ ] **Step 7: Commit**

```bash
git add cmd/keep/
git commit -m "feat(cli): add keep validate command"
```

---

### Task 3: Fixture file format -- parsing

**Files:**
- Create: `cmd/keep/cli/fixture.go`
- Create: `cmd/keep/cli/fixture_test.go`
- Create: `cmd/keep/cli/testdata/fixtures/linear-tests.yaml`

- [ ] **Step 1: Write failing tests**

Create `cmd/keep/cli/fixture_test.go`:
- `TestParseFixtures_Valid` -- parses a fixture file, returns test cases with call and expect
- `TestParseFixtures_DefaultScope` -- file-level scope applied when test.call.context.scope is empty
- `TestParseFixtures_DefaultContext` -- missing context fields get defaults (agent_id="test", timestamp=now)
- `TestParseFixtures_Invalid` -- missing `expect.decision`, returns error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestParseFixtures'`
Expected: FAIL

- [ ] **Step 3: Implement fixture parsing**

Create `cmd/keep/cli/fixture.go`:

```go
// FixtureFile is a parsed test fixture file.
type FixtureFile struct {
	Scope string     `yaml:"scope"`
	Tests []TestCase `yaml:"tests"`
}

// TestCase is a single test: a call and the expected result.
type TestCase struct {
	Name   string     `yaml:"name"`
	Call   FixtureCall `yaml:"call"`
	Expect Expectation `yaml:"expect"`
}

// FixtureCall is the call to evaluate.
type FixtureCall struct {
	Operation string         `yaml:"operation"`
	Params    map[string]any `yaml:"params"`
	Context   *FixtureContext `yaml:"context,omitempty"`
}

// FixtureContext is optional context overrides.
type FixtureContext struct {
	AgentID   string            `yaml:"agent_id,omitempty"`
	UserID    string            `yaml:"user_id,omitempty"`
	Scope     string            `yaml:"scope,omitempty"`
	Direction string            `yaml:"direction,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
}

// Expectation is the expected evaluation result.
type Expectation struct {
	Decision  string             `yaml:"decision"`
	Rule      string             `yaml:"rule,omitempty"`
	Message   string             `yaml:"message,omitempty"`
	Mutations []ExpectedMutation `yaml:"mutations,omitempty"`
}

type ExpectedMutation struct {
	Path     string `yaml:"path"`
	Replaced string `yaml:"replaced"`
}

// LoadFixtures reads and parses all fixture files from a path.
// Path can be a file or directory.
func LoadFixtures(path string) ([]FixtureFile, error)
```

- [ ] **Step 4: Create fixture testdata**

Create `cmd/keep/cli/testdata/fixtures/linear-tests.yaml` matching the PRD format.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestParseFixtures'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/keep/cli/fixture.go cmd/keep/cli/fixture_test.go cmd/keep/cli/testdata/
git commit -m "feat(cli): add fixture file parsing for keep test"
```

---

### Task 4: `keep test` command

**Files:**
- Create: `cmd/keep/cli/test.go`
- Create: `cmd/keep/cli/test_cmd_test.go`
- Create: `cmd/keep/cli/testdata/fixtures/anthropic-tests.yaml`

- [ ] **Step 1: Write failing tests**

Create `cmd/keep/cli/test_cmd_test.go`:
- `TestTestCmd_AllPass` -- all fixtures pass, exit 0, output shows PASS for each
- `TestTestCmd_SomeFail` -- one fixture fails, exit 1, output shows FAIL with expected vs got
- `TestTestCmd_LoadError` -- bad rules dir, exit 2
- `TestTestCmd_BadFixtures` -- unparseable fixture file, exit 2
- `TestTestCmd_EnforceMode` -- rules in audit_only mode are still enforced in test

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestTestCmd'`
Expected: FAIL

- [ ] **Step 3: Implement test command**

Create `cmd/keep/cli/test.go`:

```go
var testCmd = &cobra.Command{
	Use:   "test <rules-dir>",
	Short: "Test rules against fixture files",
	Args:  cobra.ExactArgs(1),
	RunE:  runTest,
}

func init() {
	testCmd.Flags().String("fixtures", "", "Path to fixtures file or directory (required)")
	testCmd.MarkFlagRequired("fixtures")
	testCmd.Flags().String("profiles", "", "Path to profiles directory")
	testCmd.Flags().String("packs", "", "Path to starter packs directory")
	rootCmd.AddCommand(testCmd)
}
```

`runTest`:
1. Load engine with `keep.Load()` -- force all scopes to enforce mode
2. Load fixtures with `LoadFixtures()`
3. For each test case:
   - Build `keep.Call` from fixture (apply defaults)
   - Call `engine.Evaluate(call, scope)`
   - Compare result against expectation
   - Print PASS or FAIL with details
4. Print summary: total, passed, failed
5. Exit 0 if all pass, 1 if any fail, 2 on load errors

Output format matches PRD CLI spec.

**Force enforce mode:** The test command needs the engine to evaluate in enforce mode regardless of the scope's `mode` setting. Add a `keep.WithForceEnforce()` option to the engine that overrides all scopes to `enforce` mode.

- [ ] **Step 4: Create additional fixture testdata**

Add anthropic gateway fixtures for redaction tests.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestTestCmd'`
Expected: PASS

- [ ] **Step 6: End-to-end smoke test**

```bash
go build -o keep ./cmd/keep
./keep validate testdata/rules
./keep test testdata/rules --fixtures cmd/keep/cli/testdata/fixtures/
```

Expected: both commands work correctly.

- [ ] **Step 7: Run all tests**

Run: `make test-unit`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/keep/ keep.go
git commit -m "feat(cli): add keep test command with fixture evaluation"
```
