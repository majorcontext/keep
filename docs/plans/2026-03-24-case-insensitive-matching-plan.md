# Case-Insensitive Matching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Keep engine normalize string inputs to lowercase before CEL evaluation by default, with a per-scope `case_sensitive` escape hatch and a `validate` linter for uppercase string literals.

**Architecture:** Normalization happens in `engine.Evaluator.Evaluate()` — the single evaluation entry point. Original params are preserved for secret detection and redaction. A new `CaseSensitive` config field on `RuleFile` opts scopes out. The `validate` CLI command gains a lint pass over CEL expressions.

**Tech Stack:** Go, CEL (cel-go v0.27.0), YAML config

**Spec:** `docs/plans/2026-03-24-case-insensitive-matching-spec.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/engine/normalize.go` | Create | `deepLowerStrings()` and `lowerContext()` helpers |
| `internal/engine/normalize_test.go` | Create | Tests for normalization helpers |
| `internal/engine/eval.go` | Modify | Add `caseSensitive` field to `Evaluator`, normalize in `Evaluate()`, preserve originals for redaction |
| `internal/engine/glob.go` | Modify | Lowercase both sides in `GlobMatch` when case-insensitive |
| `internal/config/config.go` | Modify | Add `CaseSensitive bool` to `RuleFile` |
| `keep.go` | Modify | Pass `CaseSensitive` through to `NewEvaluator` |
| `internal/engine/eval_test.go` | Modify | Add case-insensitive matching tests |
| `internal/engine/glob_test.go` | Modify | Add case-insensitive glob tests |
| `internal/validate/lint.go` | Create | Uppercase string literal linter for CEL expressions |
| `internal/validate/lint_test.go` | Create | Tests for the linter |
| `cmd/keep/cli/validate.go` | Modify | Integrate lint warnings into validate command |

---

### Task 1: `deepLowerStrings` helper

**Files:**
- Create: `internal/engine/normalize.go`
- Create: `internal/engine/normalize_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/engine/normalize_test.go
package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeepLowerStrings(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{
			name: "simple strings",
			in:   map[string]any{"Name": "Bash", "count": 42},
			want: map[string]any{"Name": "bash", "count": 42},
		},
		{
			name: "nested map",
			in:   map[string]any{"input": map[string]any{"Command": "CURL https://evil.com"}},
			want: map[string]any{"input": map[string]any{"Command": "curl https://evil.com"}},
		},
		{
			name: "slice of strings",
			in:   map[string]any{"tags": []any{"FOO", "Bar", 123}},
			want: map[string]any{"tags": []any{"foo", "bar", 123}},
		},
		{
			name: "nil map",
			in:   nil,
			want: nil,
		},
		{
			name: "preserves keys",
			in:   map[string]any{"ToolName": "Bash"},
			want: map[string]any{"ToolName": "bash"},
		},
		{
			name: "bool and float preserved",
			in:   map[string]any{"enabled": true, "score": 3.14, "name": "Test"},
			want: map[string]any{"enabled": true, "score": 3.14, "name": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepLowerStrings(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestDeepLowerStrings ./internal/engine'`
Expected: FAIL — `deepLowerStrings` not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/engine/normalize.go
package engine

import "strings"

// deepLowerStrings returns a shallow copy of the map with all string values
// recursively lowercased. Map keys are preserved as-is. Non-string values
// (ints, bools, floats) are copied unchanged.
func deepLowerStrings(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = lowerValue(v)
	}
	return out
}

// lowerValue lowercases a string, recurses into maps and slices,
// and returns all other types unchanged.
func lowerValue(v any) any {
	switch val := v.(type) {
	case string:
		return strings.ToLower(val)
	case map[string]any:
		return deepLowerStrings(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = lowerValue(item)
		}
		return out
	default:
		return v
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit ARGS='-run TestDeepLowerStrings ./internal/engine'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/normalize.go internal/engine/normalize_test.go
git commit -m "feat(engine): add deepLowerStrings normalization helper"
```

---

### Task 2: Config — add `CaseSensitive` field

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add `CaseSensitive` to `RuleFile`**

In `internal/config/config.go`, add to the `RuleFile` struct:

```go
type RuleFile struct {
	Scope         string            `yaml:"scope"`
	Profile       string            `yaml:"profile,omitempty"`
	Mode          Mode              `yaml:"mode,omitempty"`
	OnError       ErrorMode         `yaml:"on_error,omitempty"`
	CaseSensitive bool              `yaml:"case_sensitive,omitempty"`
	Defs          map[string]string `yaml:"defs,omitempty"`
	Packs         []PackRef         `yaml:"packs,omitempty"`
	Rules         []Rule            `yaml:"rules"`
}
```

- [ ] **Step 2: Run existing tests to verify nothing breaks**

Run: `make test-unit`
Expected: All existing tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add case_sensitive field to RuleFile"
```

---

### Task 3: Evaluator — wire `caseSensitive` flag

**Files:**
- Modify: `internal/engine/eval.go`
- Modify: `keep.go`

- [ ] **Step 1: Add `caseSensitive` field to `Evaluator` and `NewEvaluator`**

In `internal/engine/eval.go`, add `caseSensitive bool` to the `Evaluator` struct and a new parameter to `NewEvaluator`:

```go
type Evaluator struct {
	rules         []compiledRule
	mode          config.Mode
	onError       config.ErrorMode
	scope         string
	secrets       *secrets.Detector
	caseSensitive bool
}

func NewEvaluator(
	celEnv *keepcel.Env,
	scope string,
	mode config.Mode,
	onError config.ErrorMode,
	rules []config.Rule,
	aliases map[string]string,
	defs map[string]string,
	detector *secrets.Detector,
	caseSensitive bool,
) (*Evaluator, error) {
```

Set `caseSensitive` in the return value:

```go
return &Evaluator{
	rules:         compiled,
	mode:          mode,
	onError:       onError,
	scope:         scope,
	secrets:       detector,
	caseSensitive: caseSensitive,
}, nil
```

- [ ] **Step 2: Update `keep.go` to pass `CaseSensitive` through**

In `keep.go`, in `buildEvaluators()`, pass the new field:

```go
ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases, rf.Defs, detector, rf.CaseSensitive)
```

- [ ] **Step 3: Update `makeEvaluator` test helper in `internal/engine/eval_test.go`**

The existing helper calls `NewEvaluator` and must be updated for the new parameter. Add a `caseSensitive` parameter:

```go
func makeEvaluator(t *testing.T, rules []config.Rule) *Evaluator {
	return makeEvaluatorWithOpts(t, rules, false)
}

func makeEvaluatorWithOpts(t *testing.T, rules []config.Rule, caseSensitive bool) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, caseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}
```

Also fix any other direct `NewEvaluator` calls in test files (e.g., `eval_error_test.go`, `llm_toolcall_test.go`, etc.) by adding `false` as the last argument.

- [ ] **Step 4: Run build and tests to verify compilation**

Run: `make build && make test-unit`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/eval.go internal/engine/eval_test.go keep.go
git commit -m "feat(engine): wire caseSensitive flag through Evaluator"
```

---

### Task 4: Normalize inputs in `Evaluate()`

**Files:**
- Modify: `internal/engine/eval.go`
- Modify: `internal/engine/eval_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/eval_test.go` (or a new test file — follow existing convention):

```go
func TestEvaluate_CaseInsensitive(t *testing.T) {
	rules := []config.Rule{
		{
			Name: "block-bash",
			Match: config.Match{
				Operation: "llm.tool_use",
				When:      "params.name == 'bash'",
			},
			Action:  config.ActionDeny,
			Message: "bash blocked",
		},
	}

	// Default: case-insensitive (false = not case-sensitive)
	ev := makeEvaluatorWithOpts(t, rules, false)

	tests := []struct {
		name      string
		operation string
		toolName  string
		wantDeny  bool
	}{
		{"lowercase", "llm.tool_use", "bash", true},
		{"uppercase", "llm.tool_use", "BASH", true},
		{"mixed", "llm.tool_use", "Bash", true},
		{"operation mixed case", "LLM.Tool_Use", "bash", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ev.Evaluate(Call{
				Operation: tt.operation,
				Params:    map[string]any{"name": tt.toolName},
				Context:   CallContext{Direction: "response"},
			})
			if tt.wantDeny {
				if result.Decision != Deny {
					t.Errorf("expected Deny, got %s", result.Decision)
				}
			} else {
				if result.Decision != Allow {
					t.Errorf("expected Allow, got %s", result.Decision)
				}
			}
		})
	}

	// Verify audit trail preserves original operation name
	result := ev.Evaluate(Call{
		Operation: "LLM.Tool_Use",
		Params:    map[string]any{"name": "Bash"},
		Context:   CallContext{Direction: "response"},
	})
	if result.Audit.Operation != "LLM.Tool_Use" {
		t.Errorf("audit should preserve original operation, got %s", result.Audit.Operation)
	}
}

func TestEvaluate_CaseInsensitiveContext(t *testing.T) {
	rules := []config.Rule{
		{
			Name: "check-agent",
			Match: config.Match{
				When: "context.agent_id == 'bot-1' && context.direction == 'request'",
			},
			Action:  config.ActionDeny,
			Message: "blocked",
		},
	}

	ev := makeEvaluatorWithOpts(t, rules, false)

	result := ev.Evaluate(Call{
		Operation: "test",
		Params:    map[string]any{},
		Context: CallContext{
			AgentID:   "BOT-1",
			Direction: "Request",
		},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny with mixed-case context, got %s", result.Decision)
	}
}

func TestEvaluate_CaseSensitiveScope(t *testing.T) {
	rules := []config.Rule{
		{
			Name: "exact-match",
			Match: config.Match{
				Operation: "vault.lookup",
				When:      "params.token == 'sk-live-abc123'",
			},
			Action:  config.ActionDeny,
			Message: "blocked",
		},
	}

	// case_sensitive: true
	ev := makeEvaluatorWithOpts(t, rules, true)

	// Exact case matches
	result := ev.Evaluate(Call{
		Operation: "vault.lookup",
		Params:    map[string]any{"token": "sk-live-abc123"},
		Context:   CallContext{},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny for exact case match, got %s", result.Decision)
	}

	// Wrong case does NOT match in case-sensitive mode
	result = ev.Evaluate(Call{
		Operation: "vault.lookup",
		Params:    map[string]any{"token": "SK-LIVE-ABC123"},
		Context:   CallContext{},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow for wrong case in case-sensitive mode, got %s", result.Decision)
	}

	// Operation case matters too
	result = ev.Evaluate(Call{
		Operation: "Vault.Lookup",
		Params:    map[string]any{"token": "sk-live-abc123"},
		Context:   CallContext{},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow for wrong operation case in case-sensitive mode, got %s", result.Decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestEvaluate_CaseInsensitive ./internal/engine'`
Expected: FAIL — "Bash" does not match "bash" (no normalization yet)

- [ ] **Step 3: Add normalization to `Evaluate()`**

At the top of `Evaluate()` in `internal/engine/eval.go`, after building `celCtx`, add:

```go
// Preserve originals for secret detection, redaction, and audit trail.
originalParams := call.Params
normalizedOp := call.Operation

// Normalize strings for case-insensitive matching.
if !ev.caseSensitive {
	celParams = deepLowerStrings(celParams)
	for k, v := range celCtx {
		if s, ok := v.(string); ok {
			celCtx[k] = strings.ToLower(s)
		}
	}
	// Labels map
	if labels, ok := celCtx["labels"].(map[string]string); ok {
		lowered := make(map[string]string, len(labels))
		for k, v := range labels {
			lowered[strings.ToLower(k)] = strings.ToLower(v)
		}
		celCtx["labels"] = lowered
	}
	normalizedOp = strings.ToLower(call.Operation)
}
```

**Important:** Do NOT mutate `call.Operation`. Use `normalizedOp` for glob matching and `call.Operation` (unchanged) for all audit entries.

Update the glob matching call to use `normalizedOp`:

```go
// Change: if !GlobMatch(cr.rule.Match.Operation, call.Operation) {
// To:
if !GlobMatch(cr.rule.Match.Operation, normalizedOp) {
```

Update the redaction section (secret detection and regex patterns) to use `originalParams` instead of `celParams`. There are two sites in the `ActionRedact` case:

**Site 1 — secret detection (around line 305):**
```go
// Change: if val := getNestedString(celParams, keys); val != "" {
// To:
if val := getNestedString(originalParams, keys); val != "" {
```

**Site 2 — custom regex patterns (around line 322):**
```go
// Change: m := redact.Apply(celParams, cr.rule.Redact.Target, cr.patterns)
// To:
m := redact.Apply(originalParams, cr.rule.Redact.Target, cr.patterns)
```

After applying mutations at both sites, apply to both maps:

```go
celParams = redact.ApplyMutations(celParams, sm)
originalParams = redact.ApplyMutations(originalParams, sm)
mutations = append(mutations, sm...)
```

Update all four audit entry construction sites to use `call.Operation` (original, not `normalizedOp`) for the `Operation` field, and `paramsSummary(originalParams)` for `ParamsSummary`. The four sites are:
1. Error-mode deny (around line 231)
2. Enforce-mode deny (around line 275)
3. Audit-only deny (around line 346)
4. Final allow/redact return (around line 401)

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestEvaluate_Case ./internal/engine'`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `make test-unit`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/engine/eval.go internal/engine/eval_test.go
git commit -m "feat(engine): normalize inputs for case-insensitive matching"
```

---

### Task 5: `hasSecrets()` uses original params

Gitleaks patterns like `AKIA[0-9A-Z]{16}` will NOT match lowered input (`akiaiosfodnn7example`). The `hasSecrets()` CEL function must receive original-case values.

**Approach:** Add an `_originalParams` variable to the CEL activation map. The `hasSecrets` function binding reads the field value from `_originalParams` instead of its direct (lowered) argument. In `case_sensitive` mode, `_originalParams` equals `params`.

**Files:**
- Modify: `internal/cel/env.go`
- Modify: `internal/cel/env_test.go`
- Modify: `internal/engine/eval.go`
- Modify: `internal/engine/eval_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEvaluate_HasSecretsWithNormalization(t *testing.T) {
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(mustDetector(t)))
	if err != nil {
		t.Fatal(err)
	}

	rules := []config.Rule{
		{
			Name: "detect-aws-key",
			Match: config.Match{
				Operation: "llm.text",
				When:      "hasSecrets(params.text)",
			},
			Action:  config.ActionDeny,
			Message: "contains secrets",
		},
	}

	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, mustDetector(t), false)
	if err != nil {
		t.Fatal(err)
	}

	// AWS key — after normalization, input is lowered to "akiaiosfodnn7example"
	// which would NOT match gitleaks pattern AKIA[0-9A-Z]{16}.
	// hasSecrets must use original-case value.
	result := ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "my key is AKIAIOSFODNN7EXAMPLE"},
		Context:   CallContext{Direction: "request"},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny, got %s — hasSecrets likely received lowered input", result.Decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestEvaluate_HasSecretsWithNormalization ./internal/engine'`
Expected: FAIL — `hasSecrets` receives lowered input and the AWS key pattern doesn't match

- [ ] **Step 3: Add `_originalParams` to CEL environment**

In `internal/cel/env.go`, add a new variable declaration:

```go
cel.Variable("_originalParams", cel.DynType),
```

Modify the `hasSecrets` function binding to read from `_originalParams` via the activation:

```go
// hasSecrets(string) bool — uses _originalParams for original-case detection.
// The string argument identifies the field path (e.g., "params.text" becomes
// just "text" after CEL resolves params.text). We use the argument value as-is
// but look up the original from _originalParams for detection.
```

In `internal/cel/env.go`, update `Program.Eval()` to accept and pass `_originalParams`:

```go
func (p *Program) Eval(params map[string]any, ctx map[string]any, originalParams ...map[string]any) (bool, error) {
	// ...existing setup...
	activation := map[string]any{
		"params":  params,
		"context": ctx,
		"now":     ts,
	}
	if len(originalParams) > 0 && originalParams[0] != nil {
		activation["_originalParams"] = originalParams[0]
	} else {
		activation["_originalParams"] = params
	}
	// ...rest of eval...
}
```

- [ ] **Step 4: Pass `originalParams` from `Evaluate()` to CEL `Eval()`**

In `internal/engine/eval.go`, update the `evalSafe` call to pass originals:

```go
// Change: matched, evalErr := evalSafe(cr.program, celParams, celCtx)
// To:
matched, evalErr := evalSafe(cr.program, celParams, celCtx, originalParams)
```

Update `evalSafe` signature to match:

```go
func evalSafe(prog *keepcel.Program, params map[string]any, ctx map[string]any, originalParams map[string]any) (result bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = false
			err = fmt.Errorf("CEL eval panic: %v", r)
		}
	}()
	return prog.Eval(params, ctx, originalParams)
}
```

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestEvaluate_HasSecretsWithNormalization ./internal/engine'`
Expected: PASS

Run: `make test-unit`
Expected: All PASS (verify `Eval()` variadic change doesn't break existing callers)

- [ ] **Step 6: Commit**

```bash
git add internal/cel/env.go internal/engine/eval.go internal/engine/eval_test.go
git commit -m "feat(engine): thread originalParams to hasSecrets for case-sensitive detection"
```

---

### Task 6: Uppercase literal linter

**Files:**
- Create: `internal/validate/lint.go`
- Create: `internal/validate/lint_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/validate/lint_test.go
package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLintUppercaseLiterals(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantWarn bool
	}{
		{"lowercase ok", "params.name == 'bash'", false},
		{"uppercase warns", "params.name == 'Bash'", true},
		{"all caps warns", "params.name == 'BASH'", true},
		{"numbers ok", "params.count == '123'", false},
		{"mixed content warns", "params.name == 'myTool'", true},
		{"empty string ok", "params.name == ''", false},
		{"no string literal ok", "params.count > 5", false},
		{"double quotes warns", `params.name == "Bash"`, true},
		{"multiple literals mixed", "params.a == 'foo' && params.b == 'Bar'", true},
		{"lowercase path segments ok", "params.name == 'llm.tool_use'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := LintUppercaseLiterals(tt.expr)
			if tt.wantWarn {
				assert.NotEmpty(t, warnings)
			} else {
				assert.Empty(t, warnings)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestLintUppercaseLiterals ./internal/validate'`
Expected: FAIL — package/function doesn't exist

- [ ] **Step 3: Implement the linter**

```go
// internal/validate/lint.go
package validate

import (
	"fmt"
	"sort"
	"unicode"
)

// LintWarning describes a potential issue in a CEL expression.
type LintWarning struct {
	Message string
	Literal string
}

// LintUppercaseLiterals scans a CEL expression for string literals containing
// uppercase characters. Returns warnings for each uppercase literal found.
//
// This is a simple lexer-based scan — it finds quoted strings (single or double)
// and checks for uppercase characters. It does not parse CEL fully.
func LintUppercaseLiterals(expr string) []LintWarning {
	var warnings []LintWarning

	i := 0
	for i < len(expr) {
		if expr[i] == '\'' || expr[i] == '"' {
			quote := expr[i]
			i++ // skip opening quote
			start := i
			for i < len(expr) && expr[i] != quote {
				if expr[i] == '\\' {
					i++ // skip escaped character
				}
				i++
			}
			literal := expr[start:i]
			if i < len(expr) {
				i++ // skip closing quote
			}

			if hasUppercase(literal) {
				warnings = append(warnings, LintWarning{
					Message: fmt.Sprintf("string literal %q contains uppercase characters — inputs are lowered by default, use lowercase or set case_sensitive: true", literal),
					Literal: literal,
				})
			}
		} else {
			i++
		}
	}

	return warnings
}

// hasUppercase returns true if the string contains any uppercase letter.
func hasUppercase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// LintRules checks all rules in a scope for lint warnings.
// Returns a map of rule name to warnings.
func LintRules(rules []RuleLintInput) map[string][]LintWarning {
	results := make(map[string][]LintWarning)
	for _, r := range rules {
		var warnings []LintWarning
		if r.When != "" {
			warnings = append(warnings, LintUppercaseLiterals(r.When)...)
		}
		if r.Operation != "" {
			if hasUppercase(r.Operation) {
				warnings = append(warnings, LintWarning{
					Message: fmt.Sprintf("operation pattern %q contains uppercase characters", r.Operation),
					Literal: r.Operation,
				})
			}
		}
		if len(warnings) > 0 {
			results[r.Name] = warnings
		}
	}
	return results
}

// RuleLintInput is the subset of rule data needed for linting.
type RuleLintInput struct {
	Name      string
	Operation string
	When      string
}

// FormatWarnings returns human-readable warning strings, sorted for deterministic output.
func FormatWarnings(ruleWarnings map[string][]LintWarning) []string {
	var lines []string
	for rule, warnings := range ruleWarnings {
		for _, w := range warnings {
			lines = append(lines, fmt.Sprintf("Warning: rule %q: %s", rule, w.Message))
		}
	}
	sort.Strings(lines)
	return lines
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit ARGS='-run TestLintUppercaseLiterals ./internal/validate'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/validate/lint.go internal/validate/lint_test.go
git commit -m "feat(validate): add uppercase string literal linter for CEL expressions"
```

---

### Task 7: Integrate linter into `validate` command

**Files:**
- Modify: `cmd/keep/cli/validate.go`
- Modify: `cmd/keep/cli/validate_test.go`

- [ ] **Step 1: Write the failing test**

Add a test case to `cmd/keep/cli/validate_test.go` that validates a rule file with uppercase literals and expects warnings in the output.

Create a test fixture at `cmd/keep/cli/testdata/rules-uppercase/warn.yaml`:

```yaml
scope: test-uppercase
mode: enforce
rules:
  - name: block-bash
    match:
      operation: llm.tool_use
      when: "params.name == 'Bash'"
    action: deny
    message: "blocked"
```

```go
func TestValidate_UppercaseWarnings(t *testing.T) {
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate", "testdata/rules-uppercase"})
	err := cmd.Execute()
	require.NoError(t, err) // warnings are non-fatal

	output := buf.String()
	assert.Contains(t, output, "Warning")
	assert.Contains(t, output, "uppercase")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestValidate_UppercaseWarnings ./cmd/keep/cli'`
Expected: FAIL — no warning output

- [ ] **Step 3: Integrate linting into validate command**

Modify `cmd/keep/cli/validate.go` to run lint checks after successful load. This requires accessing the loaded config (rules per scope) to extract `when` and `operation` fields. Use `config.LoadAll` directly (or expose rules via the engine) to get the raw rule data for linting.

```go
func runValidate(cmd *cobra.Command, args []string) error {
	rulesDir := args[0]
	// ... existing code to get profilesDir, packsDir ...

	// Load and compile (existing)
	eng, err := keep.Load(rulesDir, opts...)
	if err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		return err
	}
	defer eng.Close()

	// Lint pass — load config separately for rule access
	loadResult, err := config.LoadAll(rulesDir, profilesDir, packsDir)
	if err == nil {
		for scopeName, rf := range loadResult.Scopes {
			if rf.CaseSensitive {
				continue // skip lint for case-sensitive scopes
			}
			rules := loadResult.ResolvedRules[scopeName]
			var inputs []validate.RuleLintInput
			for _, r := range rules {
				// Resolve aliases/defs before linting
				when := r.Match.When
				if rf.Profile != "" {
					if p, ok := loadResult.Profiles[rf.Profile]; ok {
						when = keepcel.ResolveAliases(when, p.Aliases)
					}
				}
				when = keepcel.ResolveAliases(when, rf.Defs)

				inputs = append(inputs, validate.RuleLintInput{
					Name:      r.Name,
					Operation: r.Match.Operation,
					When:      when,
				})
			}
			warnings := validate.LintRules(inputs)
			for _, lines := range validate.FormatWarnings(warnings) {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "  [%s] %s\n", scopeName, lines)
			}
		}
	}

	scopes := eng.Scopes()
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "OK (%d scopes, %s: 0 errors)\n",
		len(scopes), strings.Join(scopes, ", "))
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit ARGS='-run TestValidate_UppercaseWarnings ./cmd/keep/cli'`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `make test-unit`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/keep/cli/validate.go cmd/keep/cli/validate_test.go cmd/keep/cli/testdata/rules-uppercase/warn.yaml
git commit -m "feat(cli): integrate uppercase literal linter into validate command"
```

---

### Task 8: Update existing test fixtures and examples

**Files:**
- Modify: Various test files and example YAML files that use uppercase string literals

- [ ] **Step 1: Search for uppercase string literals in test rules**

Run: `grep -rn "[A-Z]" testdata/ examples/ --include="*.yaml" | grep -v "^#"`

Review each match. Update any uppercase string literals in `when` expressions or operation patterns to lowercase, unless they're in a `case_sensitive: true` scope or in regex patterns.

- [ ] **Step 2: Run full test suite**

Run: `make test-unit`
Expected: All PASS

- [ ] **Step 3: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: update test fixtures for case-insensitive matching"
```

---

### Task 9: Documentation

**Files:**
- Check and update: `docs/content/concepts/` — any page discussing rule matching
- Check and update: `docs/content/guides/` — any guide showing CEL expressions
- Check and update: `CONTRIBUTING.md` if it discusses rule authoring

- [ ] **Step 1: Search for docs referencing case sensitivity or string matching**

Run: `grep -rn -i "case\|lower\|upper\|string.*match" docs/`

- [ ] **Step 2: Update relevant docs**

Add a note to the rule authoring guide explaining:
- Inputs are lowered by default — write lowercase string literals
- Use `case_sensitive: true` at scope level for exact-case matching
- `matches()` receives lowered input — use case-insensitive regex patterns
- Secret detection and redaction operate on original-case values

- [ ] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs: document case-insensitive matching behavior"
```
