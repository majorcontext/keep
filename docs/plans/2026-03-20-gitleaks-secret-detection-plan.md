# Gitleaks Secret Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate gitleaks as a library to provide built-in secret detection via `secrets: true` on redact rules and `hasSecrets()` CEL function.

**Architecture:** New `internal/secrets` package wrapping gitleaks `detect.Detector`. Single instance created at engine init, passed to both the evaluator (for redact) and CEL env (for `hasSecrets()`). The gitleaks dependency is contained entirely within `internal/secrets`.

**Tech Stack:** Go, gitleaks v8 (`github.com/zricethezav/gitleaks/v8`), CEL

**Spec:** `docs/plans/2026-03-20-gitleaks-secret-detection-design.md`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/secrets/secrets.go` | `Detector`, `Finding`, `NewDetector`, `Detect`, `Redact` |
| Create | `internal/secrets/secrets_test.go` | Unit tests for Detect and Redact |
| Modify | `go.mod` / `go.sum` | Add gitleaks dependency |
| Modify | `internal/config/config.go:57-60` | Add `Secrets bool` to `RedactSpec` |
| Modify | `internal/config/validate.go:115-148,175-205` | Add `hasSecrets` to `reservedDefNames`, update `validateRedact` |
| Modify | `internal/config/validate_test.go` | Test validation accepts `secrets: true` without patterns |
| Modify | `internal/cel/env.go:38-208` | Add `WithSecretDetector`, register `hasSecrets()` |
| Modify | `internal/cel/content_test.go` | Test `hasSecrets()` |
| Modify | `internal/engine/eval.go:80-84,86-92,97-149,286-298` | Add `secrets` field to `compiledRule`/`Evaluator`, call `detector.Redact` |
| Modify | `internal/engine/eval_test.go:11-22` | Update `makeEvaluator` for new `NewEvaluator` signature |
| Modify | `keep.go:60-96,164-197` | Create detector in `Load`/`Reload`, pass to CEL env and evaluators |
| Modify | `examples/llm-gateway-demo/rules/demo.yaml` | Replace hand-rolled regex with `secrets: true` |

---

### Task 1: Add gitleaks dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add gitleaks module**

```bash
go get github.com/zricethezav/gitleaks/v8@latest
```

- [ ] **Step 2: Verify it resolves**

```bash
go mod tidy
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gitleaks v8 dependency"
```

---

### Task 2: Create `internal/secrets` package

**Files:**
- Create: `internal/secrets/secrets.go`
- Create: `internal/secrets/secrets_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/secrets/secrets_test.go
package secrets

import (
	"strings"
	"testing"
)

func TestDetect_AWSKey(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("access key is AKIAIOSFODNN7EXAMPLE")
	if len(findings) == 0 {
		t.Fatal("expected to detect AWS access key")
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Match, "AKIAIOSFODNN7EXAMPLE") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding to contain the AWS key, got %+v", findings)
	}
}

func TestDetect_GitHubPAT(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	// ghp_ followed by 36 alphanumeric chars
	token := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	findings := d.Detect("token=" + token)
	if len(findings) == 0 {
		t.Fatal("expected to detect GitHub PAT")
	}
}

func TestDetect_StripeKey(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("stripe key sk_live_abcdefghijklmnopqrstuvwx")
	if len(findings) == 0 {
		t.Fatal("expected to detect Stripe key")
	}
}

func TestDetect_NoMatch(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("this is just normal text with no secrets")
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d: %+v", len(findings), findings)
	}
}

func TestDetect_Multiple(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "aws key AKIAIOSFODNN7EXAMPLE and github ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	findings := d.Detect(text)
	if len(findings) < 2 {
		t.Errorf("expected at least 2 findings, got %d: %+v", len(findings), findings)
	}
}

func TestRedact_ReplacesWithRuleID(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "my key is AKIAIOSFODNN7EXAMPLE ok"
	redacted, findings := d.Redact(text)
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	if strings.Contains(redacted, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("expected key to be redacted, got: %s", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED:") {
		t.Errorf("expected [REDACTED:...] placeholder, got: %s", redacted)
	}
}

func TestRedact_ReturnsFindings(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	_, findings := d.Redact("key is AKIAIOSFODNN7EXAMPLE")
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	f := findings[0]
	if f.RuleID == "" {
		t.Error("expected non-empty RuleID")
	}
	if !strings.Contains(f.Match, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("expected Match to contain the key, got: %s", f.Match)
	}
}

func TestRedact_NoMatch(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "nothing secret here"
	redacted, findings := d.Redact(text)
	if redacted != text {
		t.Errorf("expected unchanged text, got: %s", redacted)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
}

func TestNilDetector(t *testing.T) {
	var d *Detector
	findings := d.Detect("AKIAIOSFODNN7EXAMPLE")
	if findings != nil {
		t.Errorf("expected nil from nil detector, got %+v", findings)
	}
	redacted, findings := d.Redact("AKIAIOSFODNN7EXAMPLE")
	if redacted != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected original text from nil detector, got: %s", redacted)
	}
	if findings != nil {
		t.Errorf("expected nil findings from nil detector, got %+v", findings)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run Test ./internal/secrets'`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Write the implementation**

```go
// internal/secrets/secrets.go
package secrets

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zricethezav/gitleaks/v8/detect"
)

// Detector wraps the gitleaks detector for secret detection.
type Detector struct {
	d *detect.Detector
}

// Finding represents a detected secret.
type Finding struct {
	RuleID      string // e.g. "aws-access-key"
	Description string // human-readable description from gitleaks
	Match       string // the matched text
}

// NewDetector creates a Detector with gitleaks default config (~160 patterns).
func NewDetector() (*Detector, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("secrets: init gitleaks: %w", err)
	}
	return &Detector{d: d}, nil
}

// Detect scans text and returns all secret findings.
// Safe to call on nil receiver (returns nil).
func (d *Detector) Detect(text string) []Finding {
	if d == nil {
		return nil
	}
	gitleaksFindings := d.d.DetectString(text)
	if len(gitleaksFindings) == 0 {
		return nil
	}
	findings := make([]Finding, len(gitleaksFindings))
	for i, f := range gitleaksFindings {
		findings[i] = Finding{
			RuleID:      f.RuleID,
			Description: f.Description,
			Match:       f.Match,
		}
	}
	return findings
}

// Redact scans text, replaces each detected secret with [REDACTED:<RuleID>],
// and returns the redacted string plus the findings.
// Safe to call on nil receiver (returns original text, nil).
func (d *Detector) Redact(text string) (string, []Finding) {
	if d == nil {
		return text, nil
	}
	findings := d.Detect(text)
	if len(findings) == 0 {
		return text, nil
	}

	// Sort findings by length of match descending so longer matches are
	// replaced first, preventing partial replacements.
	sort.Slice(findings, func(i, j int) bool {
		return len(findings[i].Match) > len(findings[j].Match)
	})

	redacted := text
	for _, f := range findings {
		placeholder := fmt.Sprintf("[REDACTED:%s]", f.RuleID)
		redacted = strings.ReplaceAll(redacted, f.Match, placeholder)
	}
	return redacted, findings
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit ARGS='-run Test ./internal/secrets'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat(secrets): add gitleaks-powered secret detection package"
```

---

### Task 3: Config changes — add `Secrets` field and update validation

**Files:**
- Modify: `internal/config/config.go:57-60`
- Modify: `internal/config/validate.go:115-148,175-205`
- Modify: `internal/config/validate_test.go`

- [ ] **Step 1: Write failing validation test**

Add to `internal/config/validate_test.go`:

```go
func TestValidate_RedactSecretsOnly(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{
				Name:   "redact-secrets",
				Match:  Match{Operation: "llm.text"},
				Action: ActionRedact,
				Redact: &RedactSpec{
					Target:  "params.text",
					Secrets: true,
				},
			},
		},
	}
	if err := Validate(rf); err != nil {
		t.Errorf("expected secrets-only redact rule to be valid, got: %v", err)
	}
}

func TestValidate_RedactNoSecretsNoPatterns(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{
				Name:   "bad-redact",
				Match:  Match{Operation: "llm.text"},
				Action: ActionRedact,
				Redact: &RedactSpec{
					Target: "params.text",
				},
			},
		},
	}
	if err := Validate(rf); err == nil {
		t.Error("expected error for redact rule with no secrets and no patterns")
	}
}

func TestValidate_DefNameHasSecrets(t *testing.T) {
	rf := &RuleFile{
		Scope: "test-scope",
		Rules: []Rule{
			{Name: "r1", Match: Match{Operation: "op"}, Action: ActionLog},
		},
		Defs: map[string]string{"hassecrets": "'shadowed'"},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "shadows") {
		t.Errorf("expected shadow error for hasSecrets def, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestValidate_RedactSecrets ./internal/config'`
Expected: FAIL — `Secrets` field doesn't exist, validation rejects empty patterns

- [ ] **Step 3: Update `RedactSpec` in `config.go`**

In `internal/config/config.go`, change:

```go
// RedactSpec defines what to redact and how.
type RedactSpec struct {
	Target   string          `yaml:"target"`
	Patterns []RedactPattern `yaml:"patterns"`
}
```

to:

```go
// RedactSpec defines what to redact and how.
type RedactSpec struct {
	Target   string          `yaml:"target"`
	Secrets  bool            `yaml:"secrets,omitempty"`
	Patterns []RedactPattern `yaml:"patterns,omitempty"`
}
```

- [ ] **Step 4: Update `validateRedact` in `validate.go`**

Change the patterns validation block (lines 187-202) from:

```go
	// patterns must be non-empty and not exceed maxPatternsPerRedact
	if len(spec.Patterns) == 0 {
		errs = append(errs, fmt.Errorf("rules[%d]: redact patterns must not be empty", i))
	} else if len(spec.Patterns) > maxPatternsPerRedact {
```

to:

```go
	// Must have secrets: true or non-empty patterns (or both)
	if !spec.Secrets && len(spec.Patterns) == 0 {
		errs = append(errs, fmt.Errorf("rules[%d]: redact requires secrets: true or non-empty patterns", i))
	} else if len(spec.Patterns) > maxPatternsPerRedact {
```

- [ ] **Step 5: Add `hasSecrets` to `reservedDefNames` in `validate.go`**

Add to the `reservedDefNames` map (after `"dayOfWeek"` on line 134). Use lowercase since def names are validated against `[a-z][a-z0-9_]*`:

```go
	"hassecrets":    true,
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestValidate ./internal/config'`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(config): add secrets field to RedactSpec, update validation"
```

---

### Task 4: CEL `hasSecrets()` function

**Files:**
- Modify: `internal/cel/env.go:22-34,38-208`
- Modify: `internal/cel/content_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/cel/content_test.go`:

```go
func TestHasSecrets_True(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, env, "hasSecrets(params.text)")
	got, err := prog.Eval(map[string]any{"text": "key is AKIAIOSFODNN7EXAMPLE"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected hasSecrets to return true for AWS key")
	}
}

func TestHasSecrets_False(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	env, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, env, "hasSecrets(params.text)")
	got, err := prog.Eval(map[string]any{"text": "nothing secret here"}, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if got {
		t.Error("expected hasSecrets to return false for clean text")
	}
}
```

Add import `"github.com/majorcontext/keep/internal/secrets"` to the test file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestHasSecrets ./internal/cel'`
Expected: FAIL — `WithSecretDetector` and `hasSecrets` don't exist

- [ ] **Step 3: Add `WithSecretDetector` option and `hasSecrets` function to `env.go`**

Add to imports:

```go
	"github.com/majorcontext/keep/internal/secrets"
```

Update `envConfig` struct:

```go
type envConfig struct {
	rateStore      *rate.Store
	secretDetector *secrets.Detector
}
```

Add option function:

```go
// WithSecretDetector configures the CEL environment with a secret detector,
// enabling the hasSecrets(field) function.
func WithSecretDetector(d *secrets.Detector) EnvOption {
	return func(cfg *envConfig) {
		cfg.secretDetector = d
	}
}
```

Add the `hasSecrets` function registration inside `NewEnv`, after the `dayOfWeek` block:

```go
		// hasSecrets(string) bool — returns true if gitleaks detects secrets
		cel.Function("hasSecrets",
			cel.Overload("hasSecrets_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					s, ok := val.(types.String)
					if !ok {
						return types.Bool(false)
					}
					findings := cfg.secretDetector.Detect(string(s))
					return types.Bool(len(findings) > 0)
				}),
			),
		),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestHasSecrets ./internal/cel'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cel/env.go internal/cel/content_test.go
git commit -m "feat(cel): add hasSecrets() function powered by gitleaks"
```

---

### Task 5: Wire detector into engine evaluator

**Files:**
- Modify: `internal/engine/eval.go:80-92,97-149,286-298`
- Modify: `internal/engine/eval_test.go`

- [ ] **Step 1: Write failing integration test**

Add to `internal/engine/eval_test.go`:

```go
func TestEval_RedactSecrets(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	celEnv, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "redact-secrets",
			Match:  config.Match{Operation: "llm.text"},
			Action: config.ActionRedact,
			Redact: &config.RedactSpec{
				Target:  "params.text",
				Secrets: true,
			},
		},
	}
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det)
	if err != nil {
		t.Fatal(err)
	}
	result := ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7EXAMPLE"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
	if len(result.Mutations) == 0 {
		t.Error("expected mutations")
	}
}

func TestEval_HasSecretsInWhen(t *testing.T) {
	det, err := secrets.NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	celEnv, err := keepcel.NewEnv(keepcel.WithSecretDetector(det))
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:    "deny-secrets",
			Match:   config.Match{Operation: "llm.text", When: "hasSecrets(params.text)"},
			Action:  config.ActionDeny,
			Message: "secrets detected",
		},
	}
	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, det)
	if err != nil {
		t.Fatal(err)
	}

	// Should deny when text contains a secret.
	result := ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7EXAMPLE"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Deny {
		t.Errorf("expected Deny, got %s", result.Decision)
	}

	// Should allow when text is clean.
	result = ev.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "nothing secret here"},
		Context:   CallContext{Timestamp: time.Now()},
	})
	if result.Decision != Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
}
```

Add import `"github.com/majorcontext/keep/internal/secrets"` to the test file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestEval_RedactSecrets ./internal/engine'`
Expected: FAIL — `NewEvaluator` doesn't accept a detector parameter

- [ ] **Step 3: Add detector to `Evaluator` and `NewEvaluator`**

In `internal/engine/eval.go`:

Add import:

```go
	"github.com/majorcontext/keep/internal/secrets"
```

Add `secrets` field to `Evaluator`:

```go
type Evaluator struct {
	rules   []compiledRule
	mode    config.Mode
	onError config.ErrorMode
	scope   string
	secrets *secrets.Detector
}
```

Update `NewEvaluator` signature to accept detector:

```go
func NewEvaluator(
	celEnv *keepcel.Env,
	scope string,
	mode config.Mode,
	onError config.ErrorMode,
	rules []config.Rule,
	aliases map[string]string,
	defs map[string]string,
	detector *secrets.Detector,
) (*Evaluator, error) {
```

In `NewEvaluator`, when compiling redact rules (the `if r.Action == config.ActionRedact` block), change to handle `secrets: true` with optional patterns:

```go
		if r.Action == config.ActionRedact && r.Redact != nil {
			if len(r.Redact.Patterns) > 0 {
				patterns, err := redact.CompilePatterns(r.Redact.Patterns)
				if err != nil {
					return nil, fmt.Errorf("rule %q: compile redact patterns: %w", r.Name, err)
				}
				cr.patterns = patterns
			}
		}
```

Set the `secrets` field on the returned `Evaluator`:

```go
	return &Evaluator{
		rules:   compiled,
		mode:    mode,
		onError: onError,
		scope:   scope,
		secrets: detector,
	}, nil
```

- [ ] **Step 4: Add secrets redaction to `Evaluate` method**

In the `case config.ActionRedact:` block (around line 286), change:

```go
		case config.ActionRedact:
			if cr.rule.Redact != nil && !auditOnly {
				m := redact.Apply(celParams, cr.rule.Redact.Target, cr.patterns)
```

to:

```go
		case config.ActionRedact:
			if cr.rule.Redact != nil && !auditOnly {
				// Run gitleaks secret detection first if enabled.
				if cr.rule.Redact.Secrets && ev.secrets != nil {
					target := cr.rule.Redact.Target
					keys := strings.Split(strings.TrimPrefix(target, "params."), ".")
					if val := getNestedString(celParams, keys); val != "" {
						redacted, findings := ev.secrets.Redact(val)
						if len(findings) > 0 {
							sm := []redact.Mutation{{
								Path:     target,
								Original: val,
								Replaced: redacted,
							}}
							if firstRedactRule == "" {
								firstRedactRule = cr.rule.Name
							}
							celParams = redact.ApplyMutations(celParams, sm)
							mutations = append(mutations, sm...)
						}
					}
				}
				// Then run custom regex patterns on the (possibly already-redacted) text.
				m := redact.Apply(celParams, cr.rule.Redact.Target, cr.patterns)
```

Add the `getNestedString` helper at the bottom of the file:

```go
// getNestedString retrieves a string value from a nested map by key path.
func getNestedString(params map[string]any, keys []string) string {
	current := params
	for i, key := range keys {
		v, ok := current[key]
		if !ok {
			return ""
		}
		if i == len(keys)-1 {
			s, ok := v.(string)
			if !ok {
				return ""
			}
			return s
		}
		nested, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		current = nested
	}
	return ""
}
```

- [ ] **Step 5: Update all existing `NewEvaluator` call sites**

In `keep.go` line 190, update:

```go
		ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases, rf.Defs)
```

to:

```go
		ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases, rf.Defs, nil)
```

(Pass `nil` for now — Task 6 will wire the real detector.)

In `internal/engine/eval_test.go`, update `makeEvaluator` to pass nil detector:

```go
func makeEvaluator(t *testing.T, rules []config.Rule) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewEvaluator(env, "test-scope", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestEval ./internal/engine'`
Expected: PASS (all existing + new tests)

Also run: `make test-unit`
Expected: PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add internal/engine/eval.go internal/engine/eval_test.go keep.go keep_test.go
git commit -m "feat(engine): wire secret detector into evaluator for redact rules"
```

---

### Task 6: Wire detector into `keep.Load` and `Reload`

**Files:**
- Modify: `keep.go:34-46,60-96,134-155,164-197`

- [ ] **Step 1: Write failing test**

Add to `keep_test.go` (or whichever file tests `Load`):

```go
func TestLoad_SecretsRule(t *testing.T) {
	// Create a temp rules dir with a secrets rule
	dir := t.TempDir()
	ruleYAML := `
scope: test
mode: enforce
rules:
  - name: redact-secrets
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true
`
	os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(ruleYAML), 0644)

	eng, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, err := eng.Evaluate(Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "key is AKIAIOSFODNN7EXAMPLE"},
		Context:   CallContext{Timestamp: time.Now(), Scope: "test"},
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != Redact {
		t.Errorf("expected Redact, got %s", result.Decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit ARGS='-run TestLoad_SecretsRule'`
Expected: FAIL — detector is nil, no redaction happens

- [ ] **Step 3: Wire detector into Load and Reload**

In `keep.go`, add import:

```go
	"github.com/majorcontext/keep/internal/secrets"
```

Add `secrets` field to `Engine`:

```go
type Engine struct {
	mu         sync.RWMutex
	evaluators map[string]*engine.Evaluator
	rateStore  *rate.Store
	secrets    *secrets.Detector
	cfg        engineConfig
}
```

In `Load()`, after creating the rate store (line 75), add:

```go
	// 2b. Create secrets detector.
	detector, err := secrets.NewDetector()
	if err != nil {
		return nil, fmt.Errorf("keep: init secrets detector: %w", err)
	}
```

Update the CEL env creation to pass the detector:

```go
	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(store), keepcel.WithSecretDetector(detector))
```

Update `buildEvaluators` to accept and pass the detector:

```go
func buildEvaluators(lr *config.LoadResult, celEnv *keepcel.Env, cfg engineConfig, detector *secrets.Detector) (map[string]*engine.Evaluator, error) {
```

And in the `NewEvaluator` call inside `buildEvaluators`:

```go
		ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases, rf.Defs, detector)
```

Update both call sites of `buildEvaluators` (in `Load` and `Reload`):

In `Load`:
```go
	evaluators, err := buildEvaluators(loadResult, celEnv, cfg, detector)
```

Store detector on engine:
```go
	return &Engine{
		evaluators: evaluators,
		rateStore:  store,
		secrets:    detector,
		cfg:        cfg,
	}, nil
```

In `Reload`, update CEL env and buildEvaluators calls:
```go
	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(e.rateStore), keepcel.WithSecretDetector(e.secrets))
```
```go
	evaluators, err := buildEvaluators(loadResult, celEnv, e.cfg, e.secrets)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add keep.go
git commit -m "feat: wire secret detector into engine Load and Reload"
```

---

### Task 7: Update demo rules and verify end-to-end

**Files:**
- Modify: `examples/llm-gateway-demo/rules/demo.yaml`

- [ ] **Step 1: Update demo rules to use `secrets: true`**

Replace the two hand-rolled redact rules in `examples/llm-gateway-demo/rules/demo.yaml`:

```yaml
  # Redact secrets from user text before they reach the model
  - name: redact-secrets-in-text
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true

  # Redact secrets from tool results before they reach the model
  - name: redact-secrets-in-tool-results
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      secrets: true
```

- [ ] **Step 2: Build and run all tests**

Run: `make build && make test-unit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add examples/llm-gateway-demo/rules/demo.yaml
git commit -m "feat(demo): use gitleaks secret detection instead of hand-rolled regex"
```
