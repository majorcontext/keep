# Config Package Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Parse and validate Keep rule files, profiles, and starter packs from YAML into Go structs.

**Architecture:** A single `internal/config` package that reads YAML files from directories, parses them into typed Go structs, and validates all structural constraints. No CEL compilation -- that's the engine's job. The package exports parsed types that the engine consumes.

**Tech Stack:** Go, `gopkg.in/yaml.v3` for YAML parsing, `regexp` for RE2 validation, standard library for everything else.

---

### Task 1: Go module and project skeleton

**Files:**
- Create: `go.mod`
- Create: `go.sum` (generated)
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Initialize Go module**

Run: `go mod init github.com/majorcontext/keep`
Expected: `go.mod` created

- [ ] **Step 2: Add yaml dependency**

Run: `go get gopkg.in/yaml.v3`
Expected: `go.sum` populated

- [ ] **Step 3: Create config package with types**

Create `internal/config/config.go` with the core types only (no loading logic yet):

```go
// Package config parses and validates Keep rule files, profiles, and starter packs.
package config

import "time"

// RuleFile is the parsed representation of a single YAML rule file.
type RuleFile struct {
	Scope   string    `yaml:"scope"`
	Profile string    `yaml:"profile,omitempty"`
	Mode    Mode      `yaml:"mode,omitempty"`
	OnError ErrorMode `yaml:"on_error,omitempty"`
	Packs   []PackRef `yaml:"packs,omitempty"`
	Rules   []Rule    `yaml:"rules"`
}

// Mode controls whether rules are enforced or only audited.
type Mode string

const (
	ModeEnforce   Mode = "enforce"
	ModeAuditOnly Mode = "audit_only"
)

// ErrorMode controls behavior when a CEL expression errors at eval time.
type ErrorMode string

const (
	ErrorModeClosed ErrorMode = "closed"
	ErrorModeOpen   ErrorMode = "open"
)

// Rule is an atomic unit of policy.
type Rule struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description,omitempty"`
	Match       Match      `yaml:"match,omitempty"`
	Action      Action     `yaml:"action"`
	Message     string     `yaml:"message,omitempty"`
	Redact      *RedactSpec `yaml:"redact,omitempty"`
}

// Match determines when a rule applies.
type Match struct {
	Operation string `yaml:"operation,omitempty"`
	When      string `yaml:"when,omitempty"`
}

// Action is what to do when a rule matches.
type Action string

const (
	ActionDeny   Action = "deny"
	ActionLog    Action = "log"
	ActionRedact Action = "redact"
)

// RedactSpec defines what to redact and how.
type RedactSpec struct {
	Target   string          `yaml:"target"`
	Patterns []RedactPattern `yaml:"patterns"`
}

// RedactPattern is a regex pattern and its replacement.
type RedactPattern struct {
	Match   string `yaml:"match"`
	Replace string `yaml:"replace"`
}

// PackRef references a starter pack with optional overrides.
type PackRef struct {
	Name      string                 `yaml:"name"`
	Overrides map[string]interface{} `yaml:"overrides,omitempty"`
}

// Profile maps short alias names to parameter field paths.
type Profile struct {
	Name    string            `yaml:"name"`
	Aliases map[string]string `yaml:"aliases"`
}

// StarterPack is a reusable set of rules.
type StarterPack struct {
	Name    string `yaml:"name"`
	Profile string `yaml:"profile,omitempty"`
	Rules   []Rule `yaml:"rules"`
}
```

- [ ] **Step 4: Write a basic parse test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuleFileUnmarshal(t *testing.T) {
	input := `
scope: test-scope
mode: enforce
rules:
  - name: deny-all
    match:
      operation: "*"
    action: deny
    message: "blocked"
`
	var rf RuleFile
	if err := yaml.Unmarshal([]byte(input), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Scope != "test-scope" {
		t.Errorf("scope = %q, want %q", rf.Scope, "test-scope")
	}
	if rf.Mode != ModeEnforce {
		t.Errorf("mode = %q, want %q", rf.Mode, ModeEnforce)
	}
	if len(rf.Rules) != 1 {
		t.Fatalf("rules count = %d, want 1", len(rf.Rules))
	}
	if rf.Rules[0].Action != ActionDeny {
		t.Errorf("action = %q, want %q", rf.Rules[0].Action, ActionDeny)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): add core types and YAML unmarshaling"
```

---

### Task 2: Validation -- names, formats, required fields

**Files:**
- Create: `internal/config/validate.go`
- Create: `internal/config/validate_test.go`
- Create: `internal/config/testdata/valid/basic.yaml`
- Create: `internal/config/testdata/invalid/missing-scope.yaml`
- Create: `internal/config/testdata/invalid/bad-scope-name.yaml`
- Create: `internal/config/testdata/invalid/bad-rule-name.yaml`
- Create: `internal/config/testdata/invalid/duplicate-rule.yaml`
- Create: `internal/config/testdata/invalid/missing-action.yaml`
- Create: `internal/config/testdata/invalid/redact-without-spec.yaml`

- [ ] **Step 1: Write failing tests for validation**

Create `internal/config/validate_test.go` with tests for:
- Valid file passes validation
- Missing `scope` is an error
- Scope name with uppercase is an error
- Scope name over 64 chars is an error
- Missing `rules` is an error
- Rule missing `name` is an error
- Rule missing `action` is an error
- Rule name with uppercase is an error
- Duplicate rule names within a scope is an error
- Action `redact` without `redact` block is an error
- Invalid mode value is an error
- Invalid on_error value is an error
- When expression over 2048 chars is an error

Each test calls `Validate(rf *RuleFile) error` and checks the error message.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestValidate'`
Expected: FAIL (Validate not defined)

- [ ] **Step 3: Implement Validate**

Create `internal/config/validate.go`:
- `Validate(rf *RuleFile) error` -- validates a single parsed rule file
- Returns a `ValidationError` that includes file context and a list of issues
- Uses `regexp.MustCompile` for name format checks
- Checks all 13 items from the PRD validate spec (except CEL compilation, profile resolution, and pack resolution -- those come later)

- [ ] **Step 4: Create testdata fixtures**

Create YAML fixture files in `internal/config/testdata/` for both valid and invalid cases. The tests load these files.

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestValidate'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/validate.go internal/config/validate_test.go internal/config/testdata/
git commit -m "feat(config): add rule file validation"
```

---

### Task 3: RE2 pattern validation for redact rules

**Files:**
- Modify: `internal/config/validate.go`
- Modify: `internal/config/validate_test.go`
- Create: `internal/config/testdata/invalid/bad-redact-regex.yaml`
- Create: `internal/config/testdata/invalid/missing-redact-target.yaml`

- [ ] **Step 1: Write failing tests**

Add tests to `validate_test.go`:
- Valid RE2 pattern passes
- Invalid RE2 pattern (e.g., `"(unclosed"`) is an error with pattern context
- Missing `target` in redact block is an error
- Empty `patterns` list in redact block is an error
- Missing `replace` in a pattern is an error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestValidateRedact'`
Expected: FAIL

- [ ] **Step 3: Implement redact validation**

Add to `validate.go`:
- Compile each redact pattern's `match` field with `regexp.Compile`
- Validate `target` is present and looks like a dot-separated field path
- Validate each pattern has both `match` and `replace`

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestValidateRedact'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): validate redact patterns and field paths"
```

---

### Task 4: Directory loading -- LoadRules

**Files:**
- Create: `internal/config/load.go`
- Create: `internal/config/load_test.go`
- Create: `internal/config/testdata/rules/linear.yaml`
- Create: `internal/config/testdata/rules/slack.yaml`
- Create: `internal/config/testdata/rules-duplicate-scope/a.yaml`
- Create: `internal/config/testdata/rules-duplicate-scope/b.yaml`

- [ ] **Step 1: Write failing tests**

Create `internal/config/load_test.go`:
- `TestLoadRules_ValidDir` -- loads a directory with two valid rule files, returns two `RuleFile` structs
- `TestLoadRules_EmptyDir` -- returns error (no rule files found)
- `TestLoadRules_DuplicateScope` -- two files with same scope name, returns error
- `TestLoadRules_NonexistentDir` -- returns error
- `TestLoadRules_SkipsNonYaml` -- ignores `.md` files in the directory

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestLoadRules'`
Expected: FAIL

- [ ] **Step 3: Implement LoadRules**

Create `internal/config/load.go`:

```go
// LoadRules reads all .yaml and .yml files from dir, parses them as
// rule files, validates each one, and checks scope uniqueness across
// all files. Returns the parsed files indexed by scope name.
func LoadRules(dir string) (map[string]*RuleFile, error)
```

Implementation:
- `os.ReadDir` to list files
- Filter to `.yaml` / `.yml` extensions
- `os.ReadFile` + `yaml.Unmarshal` for each
- `Validate` each parsed file
- Check scope uniqueness across all files
- Return `map[scope_name]*RuleFile`

- [ ] **Step 4: Create testdata**

Create the fixture directories and YAML files.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestLoadRules'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/load.go internal/config/load_test.go internal/config/testdata/
git commit -m "feat(config): add directory-based rule file loading"
```

---

### Task 5: Profile loading and validation

**Files:**
- Create: `internal/config/profile.go`
- Create: `internal/config/profile_test.go`
- Create: `internal/config/testdata/profiles/linear.yaml`
- Create: `internal/config/testdata/profiles-invalid/bad-alias.yaml`

- [ ] **Step 1: Write failing tests**

Create `internal/config/profile_test.go`:
- `TestLoadProfiles_Valid` -- loads a directory, returns parsed profiles indexed by name
- `TestLoadProfiles_EmptyDir` -- returns empty map (profiles are optional)
- `TestLoadProfiles_BadAliasName` -- uppercase alias name, returns error
- `TestLoadProfiles_AliasNotParams` -- alias targets `context.agent_id`, returns error
- `TestLoadProfiles_AliasOverlong` -- alias name > 32 chars, returns error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestLoadProfiles'`
Expected: FAIL

- [ ] **Step 3: Implement profile loading**

Create `internal/config/profile.go`:

```go
// LoadProfiles reads all .yaml and .yml files from dir, parses them
// as profiles, and validates alias names and targets.
// Returns profiles indexed by name. Returns an empty map if dir is empty
// or does not exist.
func LoadProfiles(dir string) (map[string]*Profile, error)
```

Validation:
- Alias names match `[a-z][a-z0-9_]*`, max 32 chars
- Alias targets must start with `params.`
- Alias names must not shadow `params`, `context`, or CEL built-in function names (`size`, `has`, `matches`, `startsWith`, `endsWith`, `contains`, `exists`, `all`, `filter`, `map`, `exists_one`, `int`, `uint`, `double`, `string`, `bool`, `bytes`, `list`, `type`, `null`, `true`, `false`)
- Profile names unique across all loaded files

- [ ] **Step 4: Create testdata**

Linear profile fixture:
```yaml
name: linear
aliases:
  team: "params.teamId"
  assignee: "params.assigneeId"
  priority: "params.priority"
  title: "params.title"
  description: "params.description"
```

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestLoadProfiles'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/profile.go internal/config/profile_test.go internal/config/testdata/
git commit -m "feat(config): add profile loading and validation"
```

---

### Task 6: Starter pack loading and override merging

**Files:**
- Create: `internal/config/pack.go`
- Create: `internal/config/pack_test.go`
- Create: `internal/config/testdata/packs/linear-safe-defaults.yaml`

- [ ] **Step 1: Write failing tests**

Create `internal/config/pack_test.go`:
- `TestLoadPacks_Valid` -- loads directory, returns packs indexed by name
- `TestLoadPacks_EmptyDir` -- returns empty map
- `TestResolvePacks_NoOverrides` -- pack rules prepended to rule list as-is
- `TestResolvePacks_Disabled` -- override `disabled` removes the rule
- `TestResolvePacks_OverrideWhen` -- replaces the when clause
- `TestResolvePacks_OverrideMessage` -- replaces the message
- `TestResolvePacks_UnknownOverrideTarget` -- override references non-existent rule name, returns error
- `TestResolvePacks_UnknownPackRef` -- rule file references non-existent pack, returns error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestLoadPacks|TestResolvePacks'`
Expected: FAIL

- [ ] **Step 3: Implement pack loading and resolution**

Create `internal/config/pack.go`:

```go
// LoadPacks reads all .yaml and .yml files from dir and parses them
// as starter packs. Returns packs indexed by name.
func LoadPacks(dir string) (map[string]*StarterPack, error)

// ResolvePacks takes a rule file's pack references, looks them up in the
// loaded packs, applies overrides, and returns the merged rule list
// (pack rules first, then inline rules).
func ResolvePacks(rf *RuleFile, packs map[string]*StarterPack) ([]Rule, error)
```

Override merge semantics:
- `"disabled"` string value removes the rule
- Map value is shallow merge: specified fields replace pack rule fields
- Cannot override `name` or `operation`

- [ ] **Step 4: Create testdata**

```yaml
# testdata/packs/linear-safe-defaults.yaml
name: linear-safe-defaults
profile: linear
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."
  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 issues must be created by a human."
  - name: no-close-issues
    match:
      operation: "update_issue"
      when: "params.stateId in ['done', 'cancelled']"
    action: deny
    message: "Agents cannot close or cancel issues."
```

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestLoadPacks|TestResolvePacks'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/pack.go internal/config/pack_test.go internal/config/testdata/
git commit -m "feat(config): add starter pack loading and override merging"
```

---

### Task 7: Field path validation utility

**Files:**
- Create: `internal/config/fieldpath.go`
- Create: `internal/config/fieldpath_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/config/fieldpath_test.go`:
- `TestValidateFieldPath_Valid` -- `"params.content"`, `"params.input.command"`, `"context.agent_id"`, `"context.labels.sandbox_id"` all pass
- `TestValidateFieldPath_Invalid` -- `""`, `"."`, `".foo"`, `"foo."`, `"foo..bar"`, `"123"` all fail
- `TestIsParamsPath` -- `"params.foo"` returns true, `"context.foo"` returns false

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestValidateFieldPath|TestIsParamsPath'`
Expected: FAIL

- [ ] **Step 3: Implement**

Create `internal/config/fieldpath.go`:

```go
// ValidateFieldPath checks that a dot-separated path is syntactically valid.
func ValidateFieldPath(path string) error

// IsParamsPath returns true if the path starts with "params.".
func IsParamsPath(path string) bool
```

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestValidateFieldPath|TestIsParamsPath'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/fieldpath.go internal/config/fieldpath_test.go
git commit -m "feat(config): add field path validation"
```

---

### Task 8: Integration -- LoadAll convenience function

**Files:**
- Modify: `internal/config/load.go`
- Create: `internal/config/load_all_test.go`
- Create: `internal/config/testdata/full/rules/linear.yaml`
- Create: `internal/config/testdata/full/profiles/linear.yaml`
- Create: `internal/config/testdata/full/packs/linear-safe-defaults.yaml`

- [ ] **Step 1: Write failing tests**

Create `internal/config/load_all_test.go`:
- `TestLoadAll_RulesOnly` -- loads rules dir only, no profiles or packs
- `TestLoadAll_WithProfiles` -- loads rules + profiles
- `TestLoadAll_WithPacks` -- loads rules + packs, resolves pack references
- `TestLoadAll_Full` -- loads rules + profiles + packs, all resolved
- `TestLoadAll_PackNotFound` -- rule file references nonexistent pack, returns error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestLoadAll'`
Expected: FAIL

- [ ] **Step 3: Implement LoadAll**

Add to `internal/config/load.go`:

```go
// LoadResult holds all parsed and resolved configuration.
type LoadResult struct {
	// Scopes maps scope name to its rule file with packs resolved.
	Scopes map[string]*RuleFile

	// ResolvedRules maps scope name to the merged rule list (packs + inline).
	ResolvedRules map[string][]Rule

	// Profiles maps profile name to its parsed profile.
	Profiles map[string]*Profile
}

// LoadAll reads rules, profiles, and packs from their respective directories,
// validates everything, resolves pack references and overrides, and returns
// the fully resolved configuration.
func LoadAll(rulesDir string, profilesDir string, packsDir string) (*LoadResult, error)
```

- [ ] **Step 4: Create testdata**

Full fixture set with rules referencing a profile and a pack.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestLoadAll'`
Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `make test-unit ARGS='./internal/config/'`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add LoadAll for complete config resolution"
```
