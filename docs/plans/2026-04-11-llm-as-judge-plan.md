# LLM-as-Judge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add LLM-as-judge as a rule action type with pluggable provider interface, shipped Anthropic/OpenAI clients, and test/eval CLI tooling.

**Architecture:** Judge is a new action on rules (`action: judge`). A `JudgeFunc` is injected into the engine via `WithJudge()`. When the evaluator hits a matched judge rule, it calls the func synchronously (blocking). `Engine.Evaluate` gains `context.Context` for timeout control. Shipped providers use stdlib `net/http` only.

**Tech Stack:** Go, stdlib `net/http`, `encoding/json`, `context`, existing Keep infrastructure

**Spec:** `docs/plans/2026-04-11-llm-as-judge-design.md`

---

### Task 1: Judge Package — Types and Interface

**Files:**
- Create: `judge/judge.go`
- Create: `judge/judge_test.go`

This is the foundation. All other tasks depend on these types.

- [ ] **Step 1: Write test for types and interface compliance**

```go
// judge/judge_test.go
package judge_test

import (
	"context"
	"testing"

	"github.com/majorcontext/keep/judge"
)

func TestDecisionConstants(t *testing.T) {
	if judge.Allow != "allow" {
		t.Errorf("Allow = %q, want %q", judge.Allow, "allow")
	}
	if judge.Deny != "deny" {
		t.Errorf("Deny = %q, want %q", judge.Deny, "deny")
	}
}

func TestProviderInterface(t *testing.T) {
	// Verify a simple mock satisfies the interface.
	var p judge.Provider = &mockProvider{
		verdict: judge.Verdict{Decision: judge.Deny, Reason: "test"},
	}

	v, err := p.Judge(context.Background(), judge.Request{
		Prompt:  "Is this safe?",
		Content: "hello world",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Decision != judge.Deny {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Deny)
	}
	if v.Reason != "test" {
		t.Errorf("Reason = %q, want %q", v.Reason, "test")
	}
}

type mockProvider struct {
	verdict judge.Verdict
}

func (m *mockProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	return m.verdict, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./judge/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement judge package**

```go
// judge/judge.go
package judge

import "context"

// Provider sends content to an LLM and returns a structured verdict.
type Provider interface {
	Judge(ctx context.Context, req Request) (Verdict, error)
}

// Request is the input to a judge call.
type Request struct {
	Prompt  string // The judgment prompt from the rule
	Content string // The content to judge
	Model   string // Model identifier (shortcut or full ID)
}

// Verdict is the judge's response.
type Verdict struct {
	Decision Decision // allow or deny
	Reason   string   // LLM's reasoning
	Usage    Usage    // Token consumption
}

// Decision is the judge's binary outcome.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

// Usage tracks token consumption for a judge call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./judge/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add judge/
git commit -m "feat(judge): add provider interface and types"
```

---

### Task 2: Config — JudgeSpec Type and Validation

**Files:**
- Modify: `internal/config/config.go` — add `JudgeSpec`, `ActionJudge`, `Judge` field on `Rule`
- Modify: `internal/config/validate.go` — add judge validation rules
- Modify: `internal/config/validate_test.go` — add judge validation tests

- [ ] **Step 1: Write failing validation tests**

Add to `internal/config/validate_test.go`:

```go
func TestValidateJudgeActionRequiresBlock(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "needs-judge",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
		}},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "requires a judge block") {
		t.Errorf("expected 'requires a judge block' error, got: %v", err)
	}
}

func TestValidateJudgeRequiresPrompt(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "no-prompt",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
			Judge:  &JudgeSpec{Model: "haiku"},
		}},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Errorf("expected 'prompt is required' error, got: %v", err)
	}
}

func TestValidateJudgeRequiresModel(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "no-model",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
			Judge:  &JudgeSpec{Prompt: "Is this safe?"},
		}},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Errorf("expected 'model is required' error, got: %v", err)
	}
}

func TestValidateJudgeInvalidTimeout(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "bad-timeout",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
			Judge:  &JudgeSpec{Model: "haiku", Prompt: "safe?", Timeout: "banana"},
		}},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected 'invalid timeout' error, got: %v", err)
	}
}

func TestValidateJudgeInvalidOnError(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "bad-onerror",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
			Judge:  &JudgeSpec{Model: "haiku", Prompt: "safe?", OnError: "maybe"},
		}},
	}
	err := Validate(rf)
	if err == nil || !strings.Contains(err.Error(), "on_error") {
		t.Errorf("expected on_error validation error, got: %v", err)
	}
}

func TestValidateJudgeValid(t *testing.T) {
	rf := &RuleFile{
		Scope: "test",
		Rules: []Rule{{
			Name:   "good-judge",
			Action: ActionJudge,
			Match:  Match{Operation: "*"},
			Judge:  &JudgeSpec{Model: "haiku", Prompt: "Is this safe?", Timeout: "5s", OnError: "closed"},
		}},
	}
	if err := Validate(rf); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestValidateJudge -v`
Expected: FAIL — `ActionJudge` and `JudgeSpec` undefined

- [ ] **Step 3: Add types to config.go**

Add to `internal/config/config.go`:

```go
// ActionJudge constant
const ActionJudge Action = "judge"

// JudgeSpec defines the LLM judge configuration for a rule.
type JudgeSpec struct {
	Model   string `yaml:"model"`
	Prompt  string `yaml:"prompt"`
	Timeout string `yaml:"timeout,omitempty"`
	OnError string `yaml:"on_error,omitempty"`
}
```

Add `Judge` field to `Rule` struct:

```go
type Rule struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Match       Match       `yaml:"match,omitempty"`
	Action      Action      `yaml:"action"`
	Message     string      `yaml:"message,omitempty"`
	Redact      *RedactSpec `yaml:"redact,omitempty"`
	Judge       *JudgeSpec  `yaml:"judge,omitempty"`
}
```

- [ ] **Step 4: Add validation to validate.go**

In `validateRule`, update the action switch to include `ActionJudge`:

```go
case ActionDeny, ActionLog, ActionRedact, ActionJudge:
	// valid
```

After the existing redact validation block, add:

```go
// judge validation (mirrors redact pattern)
if rule.Action == ActionJudge && rule.Judge == nil {
	errs = append(errs, fmt.Errorf("rules[%d]: action %q requires a judge block", i, ActionJudge))
} else if rule.Action == ActionJudge && rule.Judge != nil {
	errs = append(errs, validateJudge(i, rule.Judge)...)
}
```

Add the `validateJudge` function (needs `"time"` import):

```go
func validateJudge(i int, spec *JudgeSpec) []error {
	var errs []error
	if spec.Model == "" {
		errs = append(errs, fmt.Errorf("rules[%d]: judge model is required", i))
	}
	if spec.Prompt == "" {
		errs = append(errs, fmt.Errorf("rules[%d]: judge prompt is required", i))
	}
	if spec.Timeout != "" {
		if _, err := time.ParseDuration(spec.Timeout); err != nil {
			errs = append(errs, fmt.Errorf("rules[%d]: judge invalid timeout %q: %w", i, spec.Timeout, err))
		}
	}
	if spec.OnError != "" && spec.OnError != string(ErrorModeClosed) && spec.OnError != string(ErrorModeOpen) {
		errs = append(errs, fmt.Errorf("rules[%d]: judge on_error %q is invalid (must be %q or %q)", i, spec.OnError, ErrorModeClosed, ErrorModeOpen))
	}
	return errs
}
```

Also update the action error message in `validateRule` to include `ActionJudge`:

```go
default:
	errs = append(errs, fmt.Errorf("rules[%d]: action %q is invalid (must be %q, %q, %q, or %q)", i, rule.Action, ActionDeny, ActionLog, ActionRedact, ActionJudge))
```

Also add a lint warning in `LintAll` for orphaned `judge:` blocks on non-judge actions (mirrors the design spec requirement). If `rule.Judge != nil && rule.Action != ActionJudge`, emit a lint warning:

```go
// In LintAll or the linter function
if rule.Judge != nil && rule.Action != ActionJudge {
	warnings = append(warnings, fmt.Sprintf("rules[%d] %q: judge block on non-judge action %q (ignored)", i, rule.Name, rule.Action))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: ALL PASS (new and existing)

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(config): add judge action type and validation"
```

---

### Task 3: Engine — Add context.Context and JudgeFunc

**Files:**
- Modify: `internal/engine/eval.go` — add `context.Context` to `Evaluate`, add `JudgeAudit` to `RuleResult`, add `judgeFunc` to `Evaluator`, add judge action dispatch
- Modify: `internal/engine/eval_test.go` — update all `Evaluate(call)` → `Evaluate(ctx, call)`
- Modify: all other `internal/engine/*_test.go` — same signature update
- Modify: `keep.go` — update `Engine.Evaluate` signature, add `WithJudge`, add `JudgeFunc` type
- Modify: `helpers.go` — update `SafeEvaluate` signature
- Modify: `helpers_test.go` — update test calls
- Modify: `keep_test.go` — update all `eng.Evaluate(call, scope)` → `eng.Evaluate(ctx, call, scope)`
- Modify: `helpers_bench_test.go` and `keep_bench_test.go` — update bench calls
- Modify: `fuzz_test.go` — update if it calls Evaluate
- Modify: `llm/pipeline.go` — update `engine.Evaluate` call
- Modify: `internal/relay/handler.go` — update `engine.Evaluate` calls
- Modify: `cmd/keep/cli/test.go` — update `eng.Evaluate` call

This is the largest task. It touches many files but the changes are mechanical (add `ctx` parameter) except for the judge dispatch logic.

**Important:** The `context.Context` parameter propagation and judge action dispatch are the core changes. All other file modifications are mechanical signature updates. Do the core changes first, then fix callers.

- [ ] **Step 1: Write failing test for judge action in engine**

Add to `internal/engine/eval_test.go`:

```go
func TestEvaluateJudgeActionDeny(t *testing.T) {
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge: &config.JudgeSpec{
			Model:   "haiku",
			Prompt:  "Is this safe?",
			Timeout: "5s",
			OnError: string(config.ErrorModeClosed),
		},
	}}

	judgeFunc := func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{Decision: "deny", Reason: "unsafe content"}, nil
	}

	ev, err := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	ev.SetJudgeFunc(judgeFunc)

	result := ev.Evaluate(context.Background(), Call{Operation: "anything"})
	if result.Decision != Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, Deny)
	}
	if result.Rule != "safety-check" {
		t.Errorf("Rule = %q, want %q", result.Rule, "safety-check")
	}
}
```

Note: the exact `JudgeFunc` signature for the evaluator may need adjustment during implementation. The key test behavior: a judge rule that returns deny should short-circuit with Deny decision.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestEvaluateJudgeAction -v`
Expected: FAIL — `SetJudgeFunc` undefined, `Evaluate` doesn't accept context

- [ ] **Step 3: Add JudgeFunc type and context to keep.go**

In `keep.go`, add the import for `judge` package and the `JudgeFunc` type:

```go
import (
	"context"
	// ... existing imports ...
	"github.com/majorcontext/keep/judge"
)

// JudgeFunc is called when a rule with action: judge matches.
type JudgeFunc func(ctx context.Context, req judge.Request) (judge.Verdict, error)

// WithJudge sets the judge function for evaluating judge rules.
func WithJudge(fn JudgeFunc) Option {
	return func(c *engineConfig) { c.judgeFunc = fn }
}
```

Add `judgeFunc` to `engineConfig`:

```go
type engineConfig struct {
	// ... existing fields ...
	judgeFunc JudgeFunc
}
```

Update `Engine.Evaluate` signature:

```go
func (e *Engine) Evaluate(ctx context.Context, call Call, scope string) (EvalResult, error) {
	// ... existing body, pass ctx to ev.Evaluate ...
	result := ev.Evaluate(ctx, call)
	// ...
}
```

- [ ] **Step 4: Update internal engine Evaluator**

In `internal/engine/eval.go`:

Add `JudgeAudit` to types:

```go
// JudgeAudit records the result of a judge call.
type JudgeAudit struct {
	Model     string     `json:"model"`
	Verdict   string     `json:"verdict"`
	Reason    string     `json:"reason"`
	LatencyMS int64      `json:"latency_ms"`
	Usage     JudgeUsage `json:"usage"`
	Error     string     `json:"error,omitempty"`
}

// JudgeUsage tracks token consumption for a judge call.
type JudgeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
```

Add `Judge` field to `RuleResult`:

```go
type RuleResult struct {
	// ... existing fields ...
	Judge *JudgeAudit `json:"judge,omitempty"`
}
```

Add judge func type and setter to `Evaluator`:

```go
// JudgeResult holds the raw result from a judge call at the engine level.
type JudgeResult struct {
	Decision     string
	Reason       string
	InputTokens  int
	OutputTokens int
}

// JudgeHandler is the function signature for judge evaluation.
// It receives the context, model name, prompt, and content.
type JudgeHandler func(ctx context.Context, model, prompt, content string) (JudgeResult, error)
```

Add field to `Evaluator`:

```go
type Evaluator struct {
	// ... existing fields ...
	judgeFunc JudgeHandler
}

func (ev *Evaluator) SetJudgeFunc(fn JudgeHandler) {
	ev.judgeFunc = fn
}
```

Update `Evaluate` to accept `context.Context`:

```go
func (ev *Evaluator) Evaluate(ctx context.Context, call Call) EvalResult {
```

Add judge action case in the action switch (after `case config.ActionRedact:`):

```go
case config.ActionJudge:
	if cr.rule.Judge == nil {
		continue
	}
	// Extract content from params for judging
	content := extractJudgeContent(call.Params)

	// Parse timeout
	timeout := 5 * time.Second
	if cr.rule.Judge.Timeout != "" {
		if d, err := time.ParseDuration(cr.rule.Judge.Timeout); err == nil {
			timeout = d
		}
	}
	onError := config.ErrorModeClosed
	if cr.rule.Judge.OnError != "" {
		onError = config.ErrorMode(cr.rule.Judge.OnError)
	}

	var judgeAudit JudgeAudit
	judgeAudit.Model = cr.rule.Judge.Model

	if ev.judgeFunc == nil {
		// No judge configured — fail based on on_error
		judgeAudit.Error = "no judge provider configured"
		rulesEvaluated[len(rulesEvaluated)-1].Judge = &judgeAudit
		if onError == config.ErrorModeClosed {
			if !auditOnly {
				return buildDenyResult(call, ev.scope, cr.rule.Name,
					"judge not configured (fail-closed)", rulesEvaluated, params)
			}
			if !auditDenied {
				auditDenied = true
				auditDenyRule = cr.rule.Name
				auditDenyMessage = "judge not configured (fail-closed)"
			}
		}
		continue
	}

	judgeCtx, cancel := context.WithTimeout(ctx, timeout)
	start := time.Now()
	jr, judgeErr := ev.judgeFunc(judgeCtx, cr.rule.Judge.Model, cr.rule.Judge.Prompt, content)
	cancel()
	judgeAudit.LatencyMS = time.Since(start).Milliseconds()
	judgeAudit.Reason = jr.Reason
	judgeAudit.Usage = JudgeUsage{InputTokens: jr.InputTokens, OutputTokens: jr.OutputTokens}

	if judgeErr != nil {
		judgeAudit.Error = judgeErr.Error()
		judgeAudit.Verdict = "error"
		rulesEvaluated[len(rulesEvaluated)-1].Judge = &judgeAudit
		if onError == config.ErrorModeClosed {
			if !auditOnly {
				return buildDenyResult(call, ev.scope, cr.rule.Name,
					fmt.Sprintf("judge error (fail-closed): %v", judgeErr), rulesEvaluated, params)
			}
			if !auditDenied {
				auditDenied = true
				auditDenyRule = cr.rule.Name
				auditDenyMessage = fmt.Sprintf("judge error: %v", judgeErr)
			}
		}
		continue
	}

	judgeAudit.Verdict = jr.Decision
	rulesEvaluated[len(rulesEvaluated)-1].Judge = &judgeAudit

	if jr.Decision == "deny" {
		if !auditOnly {
			return buildDenyResult(call, ev.scope, cr.rule.Name,
				fmt.Sprintf("judge denied: %s", jr.Reason), rulesEvaluated, params)
		}
		if !auditDenied {
			auditDenied = true
			auditDenyRule = cr.rule.Name
			auditDenyMessage = fmt.Sprintf("judge denied: %s", jr.Reason)
		}
	}
	// decision == "allow": continue evaluating
```

Add helper functions:

```go
func extractJudgeContent(params map[string]any) string {
	// Try common content fields in priority order
	if text, ok := params["text"].(string); ok {
		return text
	}
	if content, ok := params["content"].(string); ok {
		return content
	}
	// Fall back to JSON serialization of all params
	b, _ := json.Marshal(params)
	return string(b)
}
```

Extract the deny-return pattern into a helper to avoid duplicating the `EvalResult` construction (this pattern appears 3+ times already in `Evaluate`):

```go
func buildDenyResult(call Call, scope, rule, message string, rulesEvaluated []RuleResult, params evalParams) EvalResult {
	return EvalResult{
		Decision: Deny,
		Rule:     rule,
		Message:  message,
		Audit: AuditEntry{
			Timestamp:      call.Context.Timestamp,
			Scope:          scope,
			Operation:      call.Operation,
			AgentID:        call.Context.AgentID,
			UserID:         call.Context.UserID,
			Direction:      call.Context.Direction,
			Decision:       Deny,
			Rule:           rule,
			Message:        message,
			RulesEvaluated: rulesEvaluated,
			ParamsSummary:  paramsSummary(params.original),
			Enforced:       true,
		},
	}
}
```

- [ ] **Step 5: Update keep.go to wire JudgeFunc through**

In `keep.go`, update `Engine.Evaluate` to pass context through, and update `buildEvaluators` / `buildEngine` to pass the `judgeFunc` to evaluators. The evaluator's `SetJudgeFunc` should be called after construction:

```go
func (e *Engine) Evaluate(ctx context.Context, call Call, scope string) (EvalResult, error) {
	e.mu.RLock()
	ev, ok := e.evaluators[scope]
	e.mu.RUnlock()

	if !ok {
		return EvalResult{}, fmt.Errorf("keep: scope %q not found (available: %s)", scope, strings.Join(e.Scopes(), ", "))
	}

	result := ev.Evaluate(ctx, call)
	if e.cfg.auditHook != nil {
		e.cfg.auditHook(result.Audit)
	}
	return result, nil
}
```

In `buildEvaluators`, after creating each evaluator, set the judge func. Extract the adapter into a helper so `Reload()` can reuse it:

```go
// judgeAdapter converts a keep-level JudgeFunc to an engine-level JudgeHandler.
func judgeAdapter(fn JudgeFunc) engine.JudgeHandler {
	return func(ctx context.Context, model, prompt, content string) (engine.JudgeResult, error) {
		v, err := fn(ctx, judge.Request{
			Prompt: prompt, Content: content, Model: model,
		})
		if err != nil {
			return engine.JudgeResult{}, err
		}
		return engine.JudgeResult{
			Decision:     string(v.Decision),
			Reason:       v.Reason,
			InputTokens:  v.Usage.InputTokens,
			OutputTokens: v.Usage.OutputTokens,
		}, nil
	}
}
```

In `buildEvaluators`:

```go
if cfg.judgeFunc != nil {
	ev.SetJudgeFunc(judgeAdapter(cfg.judgeFunc))
}
```

**Important:** Also update `Engine.Reload()` to set the judge func on newly created evaluators. The `Reload()` method calls `buildEvaluators` — ensure the `judgeFunc` from `e.cfg` is passed through so reloaded evaluators retain judge support.

Also re-export `JudgeAudit` and `JudgeUsage` types from `keep.go` so library consumers can inspect audit data:

```go
// Re-exported engine types for library consumers.
type JudgeAudit = engine.JudgeAudit
type JudgeUsage = engine.JudgeUsage
```

- [ ] **Step 6: Update SafeEvaluate in helpers.go**

```go
func SafeEvaluate(ctx context.Context, eng *Engine, call Call, scope string) (result EvalResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keep: panic during evaluation: %v", r)
			result = EvalResult{Decision: Deny}
		}
	}()
	return eng.Evaluate(ctx, call, scope)
}
```

- [ ] **Step 7: Mechanically update all callers**

Update every call site to add `context.Background()` (or appropriate context) as first arg:

**Files to update (search for `.Evaluate(` in each):**

- `helpers_test.go` — `SafeEvaluate(ctx, eng, call, scope)` and `eng.Evaluate(ctx, call, scope)`
- `helpers_bench_test.go` — `eng.Evaluate(ctx, call, scope)`
- `keep_test.go` — all `eng.Evaluate(call, scope)` → `eng.Evaluate(context.Background(), call, scope)`
- `keep_bench_test.go` — all benchmark calls
- `internal/engine/eval_test.go` — all `ev.Evaluate(call)` → `ev.Evaluate(context.Background(), call)`
- `internal/engine/bench_test.go` — all benchmark calls
- `internal/engine/llm_toolcall_test.go` — same pattern
- `internal/engine/llm_combined_test.go` — same pattern
- `internal/engine/llm_secret_test.go` — same pattern
- `internal/engine/mcp_toolcall_test.go` — same pattern
- `internal/engine/eval_error_test.go` — same pattern
- `internal/engine/llm_pii_test.go` — same pattern
- `llm/pipeline.go:178` — `engine.Evaluate(ctx, call, scope)` (needs context threaded from caller)
- `internal/relay/handler.go:51,124` — `h.engine.Evaluate(ctx, call, scope)` (has context from HTTP request)
- `cmd/keep/cli/test.go:121` — `eng.Evaluate(context.Background(), call, scope)`

**Approach:** Use find-and-replace. For engine internal tests: `ev.Evaluate(` → `ev.Evaluate(context.Background(), `. For keep_test.go: `eng.Evaluate(` → `eng.Evaluate(context.Background(), `. For relay handler: use the existing request context.

- [ ] **Step 8: Run full test suite**

Run: `make test-unit`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat(engine): add context.Context to Evaluate and judge action dispatch"
```

---

### Task 4: Judge Engine Tests — Full Coverage

**Files:**
- Modify: `internal/engine/eval_test.go` — add comprehensive judge tests

- [ ] **Step 1: Write judge test cases**

```go
func TestEvaluateJudgeActionAllow(t *testing.T) {
	// Judge returns allow → evaluation continues
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?", Timeout: "5s"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	ev.SetJudgeFunc(func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{Decision: "allow", Reason: "looks fine"}, nil
	})
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	if result.Decision != Allow {
		t.Errorf("Decision = %q, want %q", result.Decision, Allow)
	}
}

func TestEvaluateJudgeNoProvider(t *testing.T) {
	// No judgeFunc, on_error:closed → deny
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?", OnError: "closed"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	// No SetJudgeFunc called
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	if result.Decision != Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, Deny)
	}
}

func TestEvaluateJudgeNoProviderFailOpen(t *testing.T) {
	// No judgeFunc, on_error:open → allow
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?", OnError: "open"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	if result.Decision != Allow {
		t.Errorf("Decision = %q, want %q", result.Decision, Allow)
	}
}

func TestEvaluateJudgeErrorFailClosed(t *testing.T) {
	// Judge returns error, on_error:closed → deny
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?", OnError: "closed"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	ev.SetJudgeFunc(func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{}, fmt.Errorf("provider unavailable")
	})
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	if result.Decision != Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, Deny)
	}
}

func TestEvaluateJudgeErrorFailOpen(t *testing.T) {
	// Judge returns error, on_error:open → allow
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?", OnError: "open"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	ev.SetJudgeFunc(func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{}, fmt.Errorf("provider unavailable")
	})
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	if result.Decision != Allow {
		t.Errorf("Decision = %q, want %q", result.Decision, Allow)
	}
}

func TestEvaluateJudgeAuditTrail(t *testing.T) {
	// Verify judge audit data is populated
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "Is this safe?"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	ev.SetJudgeFunc(func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		if model != "haiku" {
			t.Errorf("model = %q, want %q", model, "haiku")
		}
		if prompt != "Is this safe?" {
			t.Errorf("prompt = %q, want %q", prompt, "Is this safe?")
		}
		return JudgeResult{Decision: "allow", Reason: "all clear"}, nil
	})
	result := ev.Evaluate(context.Background(), Call{Operation: "test", Params: map[string]any{"text": "hello"}})

	// Find the judge rule result
	var found bool
	for _, rr := range result.Audit.RulesEvaluated {
		if rr.Name == "safety-check" && rr.Judge != nil {
			found = true
			if rr.Judge.Model != "haiku" {
				t.Errorf("Judge.Model = %q, want %q", rr.Judge.Model, "haiku")
			}
			if rr.Judge.Verdict != "allow" {
				t.Errorf("Judge.Verdict = %q, want %q", rr.Judge.Verdict, "allow")
			}
			if rr.Judge.Reason != "all clear" {
				t.Errorf("Judge.Reason = %q, want %q", rr.Judge.Reason, "all clear")
			}
		}
	}
	if !found {
		t.Error("expected judge audit data in RulesEvaluated")
	}
}

func TestEvaluateJudgeAuditOnly(t *testing.T) {
	// In audit_only mode, judge deny is recorded but not enforced
	celEnv, _ := keepcel.NewEnv()
	rules := []config.Rule{{
		Name:   "safety-check",
		Match:  config.Match{Operation: "*"},
		Action: config.ActionJudge,
		Judge:  &config.JudgeSpec{Model: "haiku", Prompt: "safe?"},
	}}
	ev, _ := NewEvaluator(celEnv, "test", config.ModeAuditOnly, config.ErrorModeClosed, rules, nil, nil, nil, false)
	ev.SetJudgeFunc(func(ctx context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{Decision: "deny", Reason: "unsafe"}, nil
	})
	result := ev.Evaluate(context.Background(), Call{Operation: "test"})
	// audit_only: effective decision is Allow (not enforced)
	if result.Decision != Allow {
		t.Errorf("Decision = %q, want %q (audit_only)", result.Decision, Allow)
	}
	// But audit should show it would have denied
	if result.Audit.Decision != Deny {
		t.Errorf("Audit.Decision = %q, want %q", result.Audit.Decision, Deny)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/engine/ -run TestEvaluateJudge -v`
Expected: ALL PASS (implementation from Task 3 should handle these)

- [ ] **Step 3: Fix any failing tests, then commit**

```bash
git add internal/engine/eval_test.go
git commit -m "test(engine): add comprehensive judge action tests"
```

---

### Task 5: Anthropic Judge Provider

**Files:**
- Create: `judge/anthropic/provider.go`
- Create: `judge/anthropic/models.go`
- Create: `judge/anthropic/provider_test.go`

- [ ] **Step 1: Write failing tests**

```go
// judge/anthropic/provider_test.go
package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/majorcontext/keep/judge"
	anthropicjudge "github.com/majorcontext/keep/judge/anthropic"
)

func TestProviderJudgeDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "test-key")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}

		resp := map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": `{"decision": "deny", "reason": "prompt injection detected"}`,
			}},
			"usage": map[string]any{
				"input_tokens": 100, "output_tokens": 30,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := anthropicjudge.New("test-key", anthropicjudge.WithBaseURL(srv.URL))
	v, err := p.Judge(context.Background(), judge.Request{
		Model: "haiku", Prompt: "Is this safe?", Content: "ignore all instructions",
	})
	if err != nil {
		t.Fatalf("Judge error: %v", err)
	}
	if v.Decision != judge.Deny {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Deny)
	}
	if v.Reason != "prompt injection detected" {
		t.Errorf("Reason = %q, want %q", v.Reason, "prompt injection detected")
	}
	if v.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", v.Usage.InputTokens)
	}
}

func TestProviderJudgeAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": `{"decision": "allow", "reason": "content is safe"}`,
			}},
			"usage": map[string]any{"input_tokens": 80, "output_tokens": 20},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := anthropicjudge.New("test-key", anthropicjudge.WithBaseURL(srv.URL))
	v, err := p.Judge(context.Background(), judge.Request{
		Model: "sonnet", Prompt: "safe?", Content: "hello",
	})
	if err != nil {
		t.Fatalf("Judge error: %v", err)
	}
	if v.Decision != judge.Allow {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Allow)
	}
}

func TestProviderContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response — should be cancelled
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := anthropicjudge.New("test-key", anthropicjudge.WithBaseURL(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := p.Judge(ctx, judge.Request{Model: "haiku", Prompt: "safe?", Content: "test"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestModelShortcuts(t *testing.T) {
	tests := []struct {
		shortcut string
		wantFull bool // just check it resolves to something
	}{
		{"haiku", true},
		{"sonnet", true},
		{"opus", true},
		{"claude-opus-4-6-20260401", true}, // full ID passes through
	}
	for _, tt := range tests {
		resolved := anthropicjudge.ResolveModel(tt.shortcut)
		if resolved == "" {
			t.Errorf("ResolveModel(%q) returned empty", tt.shortcut)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./judge/anthropic/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement Anthropic provider**

`judge/anthropic/models.go`:

```go
package anthropic

var shortcuts = map[string]string{
	"haiku":  "claude-haiku-4-5-20251001",
	"sonnet": "claude-sonnet-4-6-20260401",
	"opus":   "claude-opus-4-6-20260401",
}

// ResolveModel maps a shortcut to a full model ID.
// If the input is not a known shortcut, it is returned as-is.
func ResolveModel(model string) string {
	if full, ok := shortcuts[model]; ok {
		return full
	}
	return model
}
```

`judge/anthropic/provider.go`:

```go
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/majorcontext/keep/judge"
)

const defaultBaseURL = "https://api.anthropic.com"
const apiVersion = "2023-06-01"

// Provider implements judge.Provider for the Anthropic Messages API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Option configures the provider.
type Option func(*Provider)

// WithBaseURL sets a custom base URL (for testing).
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// New creates an Anthropic judge provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Provider) Judge(ctx context.Context, req judge.Request) (judge.Verdict, error) {
	model := ResolveModel(req.Model)

	body := map[string]any{
		"model":      model,
		"max_tokens": 256,
		"messages": []map[string]any{{
			"role": "user",
			"content": fmt.Sprintf(
				"You are a policy judge. Evaluate the following content against this criteria:\n\n"+
					"Criteria: %s\n\nContent: %s\n\n"+
					"Respond with ONLY a JSON object: {\"decision\": \"allow\" or \"deny\", \"reason\": \"brief explanation\"}",
				req.Prompt, req.Content),
		}},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("judge request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return judge.Verdict{}, fmt.Errorf("judge API error (status %d): %s", resp.StatusCode, respBody)
	}

	return parseResponse(respBody)
}

func parseResponse(body []byte) (judge.Verdict, error) {
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return judge.Verdict{}, fmt.Errorf("empty response from judge")
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &verdict); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse verdict JSON: %w", err)
	}

	d := judge.Allow
	if verdict.Decision == "deny" {
		d = judge.Deny
	}

	return judge.Verdict{
		Decision: d,
		Reason:   verdict.Reason,
		Usage: judge.Usage{
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
		},
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./judge/anthropic/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add judge/anthropic/
git commit -m "feat(judge): add Anthropic provider with model shortcuts"
```

---

### Task 6: OpenAI Judge Provider

**Files:**
- Create: `judge/openai/provider.go`
- Create: `judge/openai/models.go`
- Create: `judge/openai/provider_test.go`

Same pattern as Anthropic but targeting Chat Completions API.

- [ ] **Step 1: Write failing tests**

```go
// judge/openai/provider_test.go
package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/majorcontext/keep/judge"
	openaijudge "github.com/majorcontext/keep/judge/openai"
)

func TestProviderJudgeDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}
		resp := map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"decision": "deny", "reason": "harmful content"}`,
				},
			}},
			"usage": map[string]any{
				"prompt_tokens": 90, "completion_tokens": 25,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := openaijudge.New("test-key", openaijudge.WithBaseURL(srv.URL))
	v, err := p.Judge(context.Background(), judge.Request{
		Model: "gpt-4o-mini", Prompt: "safe?", Content: "bad content",
	})
	if err != nil {
		t.Fatalf("Judge error: %v", err)
	}
	if v.Decision != judge.Deny {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Deny)
	}
	if v.Usage.InputTokens != 90 {
		t.Errorf("InputTokens = %d, want 90", v.Usage.InputTokens)
	}
}

func TestModelShortcuts(t *testing.T) {
	tests := []string{"gpt-4o", "gpt-4o-mini", "o3"}
	for _, s := range tests {
		if resolved := openaijudge.ResolveModel(s); resolved == "" {
			t.Errorf("ResolveModel(%q) returned empty", s)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./judge/openai/ -v`

- [ ] **Step 3: Implement OpenAI provider**

Same structure as Anthropic but with:
- `Authorization: Bearer <key>` header
- Chat Completions API at `/v1/chat/completions`
- Response shape: `choices[0].message.content`
- Usage: `prompt_tokens`, `completion_tokens`

`judge/openai/models.go`:

```go
package openai

var shortcuts = map[string]string{
	"gpt-4o":      "gpt-4o-2024-11-20",
	"gpt-4o-mini": "gpt-4o-mini-2024-07-18",
	"o3":          "o3-2025-04-16",
}

func ResolveModel(model string) string {
	if full, ok := shortcuts[model]; ok {
		return full
	}
	return model
}
```

`judge/openai/provider.go` — follow same pattern as Anthropic provider but with OpenAI API format.

- [ ] **Step 4: Run tests**

Run: `go test ./judge/openai/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add judge/openai/
git commit -m "feat(judge): add OpenAI provider with model shortcuts"
```

---

### Task 7: Gateway and Relay Config — Judge Block

**Files:**
- Modify: `internal/gateway/config/config.go` — add `JudgeConfig` to gateway config
- Modify: `internal/gateway/config/config_test.go` — test parsing judge config
- Modify: `internal/relay/config/config.go` — add `JudgeConfig` to relay config
- Modify: `internal/relay/config/config_test.go` — test parsing
- Modify: `cmd/keep-llm-gateway/main.go` — construct provider from config, pass to engine
- Modify: `cmd/keep-mcp-relay/main.go` — same

- [ ] **Step 1: Write failing test for gateway config parsing**

Add to `internal/gateway/config/config_test.go`:

```go
func TestParseJudgeConfig(t *testing.T) {
	yaml := `
listen: :8080
rules_dir: ./rules
provider: anthropic
upstream: https://api.anthropic.com
judge:
  provider: anthropic
  api_key_env: JUDGE_API_KEY
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Judge == nil {
		t.Fatal("Judge config is nil")
	}
	if cfg.Judge.Provider != "anthropic" {
		t.Errorf("Judge.Provider = %q, want %q", cfg.Judge.Provider, "anthropic")
	}
	if cfg.Judge.APIKeyEnv != "JUDGE_API_KEY" {
		t.Errorf("Judge.APIKeyEnv = %q, want %q", cfg.Judge.APIKeyEnv, "JUDGE_API_KEY")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Add JudgeConfig to gateway and relay configs**

```go
// Shared type (can go in either config package or a common location)
type JudgeConfig struct {
	Provider  string `yaml:"provider"`
	APIKeyEnv string `yaml:"api_key_env"`
	BaseURL   string `yaml:"base_url,omitempty"`
}
```

Add `Judge *JudgeConfig` to the gateway and relay config structs.

- [ ] **Step 4: Wire judge provider in gateway and relay main.go**

In each binary's `main.go`, after loading config, construct the provider:

```go
var judgeOpt keep.Option
if cfg.Judge != nil {
	apiKey := os.Getenv(cfg.Judge.APIKeyEnv)
	if apiKey != "" {
		switch cfg.Judge.Provider {
		case "anthropic":
			p := anthropicjudge.New(apiKey)
			judgeOpt = keep.WithJudge(p.Judge)
		case "openai":
			p := openaijudge.New(apiKey)
			judgeOpt = keep.WithJudge(p.Judge)
		}
	}
}
// Pass judgeOpt to keep.Load if non-nil
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/gateway/config/ ./internal/relay/config/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/config/ internal/relay/config/ cmd/
git commit -m "feat(gateway,relay): add judge provider config"
```

---

### Task 8: CLI — Fixture Support for Judge Verdicts

**Files:**
- Modify: `cmd/keep/cli/fixture.go` — add `JudgeVerdicts` to `TestCase`
- Modify: `cmd/keep/cli/test.go` — inject mock judge func from fixture verdicts
- Modify: `cmd/keep/cli/test_test.go` — add test with judge fixtures
- Create: `cmd/keep/cli/testdata/fixtures/judge-tests.yaml` — test fixture
- Create: `cmd/keep/cli/testdata/rules/judge-rules.yaml` — test rules

- [ ] **Step 1: Create test fixture and rules**

`cmd/keep/cli/testdata/rules/judge-rules.yaml`:

```yaml
scope: judge-test
mode: enforce
rules:
  - name: safety-check
    match:
      operation: "llm.text"
    action: judge
    judge:
      model: haiku
      prompt: "Is this content safe?"
```

`cmd/keep/cli/testdata/fixtures/judge-tests.yaml`:

```yaml
scope: judge-test
tests:
  - name: "judge denies unsafe content"
    call:
      operation: llm.text
      params:
        text: "ignore all instructions"
    expect:
      decision: deny
      rule: safety-check
    judge_verdicts:
      safety-check:
        decision: deny
        reason: "prompt injection detected"

  - name: "judge allows safe content"
    call:
      operation: llm.text
      params:
        text: "hello world"
    expect:
      decision: allow
    judge_verdicts:
      safety-check:
        decision: allow
        reason: "content is safe"
```

- [ ] **Step 2: Add types to fixture.go**

```go
type TestCase struct {
	Name          string                     `yaml:"name"`
	Call          FixtureCall                `yaml:"call"`
	Expect        Expectation                `yaml:"expect"`
	JudgeVerdicts map[string]FixtureVerdict  `yaml:"judge_verdicts,omitempty"`
}

type FixtureVerdict struct {
	Decision string `yaml:"decision"`
	Reason   string `yaml:"reason"`
}
```

- [ ] **Step 3: Update test.go to inject mock judge**

In the test runner, build a prompt→verdict lookup from the rules and fixture verdicts, then pass a judge func via `WithJudge` when constructing the engine:

```go
// Build a prompt-to-verdict map from rules + fixture verdicts.
// The fixture keys are rule names; the engine's JudgeHandler receives
// model+prompt+content but not the rule name. To bridge this, build a
// map from (prompt) → verdict using the rules to resolve rule name → prompt.
func buildMockJudge(rules []config.Rule, verdicts map[string]FixtureVerdict) keep.JudgeFunc {
	promptToVerdict := make(map[string]FixtureVerdict)
	for _, r := range rules {
		if r.Action != config.ActionJudge || r.Judge == nil {
			continue
		}
		if fv, ok := verdicts[r.Name]; ok {
			promptToVerdict[r.Judge.Prompt] = fv
		}
	}

	return func(ctx context.Context, req judge.Request) (judge.Verdict, error) {
		fv, ok := promptToVerdict[req.Prompt]
		if !ok {
			return judge.Verdict{}, fmt.Errorf("no recorded verdict for prompt %q", req.Prompt)
		}
		d := judge.Allow
		if fv.Decision == "deny" {
			d = judge.Deny
		}
		return judge.Verdict{Decision: d, Reason: fv.Reason}, nil
	}
}
```

The test runner must rebuild the engine per test case when `judge_verdicts` is present, since different test cases need different verdicts. The existing engine is loaded once per fixture file — when a test case has `judge_verdicts`, load it again with `keep.WithJudge(buildMockJudge(rules, tc.JudgeVerdicts))`.

If a fixture has a rule with `action: judge` but no entry in `judge_verdicts`, the mock returns an error, which triggers on_error behavior. The test should validate that users add verdicts by checking before running: if any rule has `action: judge` and the test case lacks a verdict entry for it, fail with a clear message.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/keep/cli/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/keep/cli/
git commit -m "feat(cli): add judge verdict support to test fixtures"
```

---

### Task 9: CLI — `keep eval` Command

**Files:**
- Create: `cmd/keep/cli/eval.go` — eval command implementation (registers via `init()`, same as test.go/validate.go)
- Create: `cmd/keep/cli/eval_test.go` — tests

The eval command registers itself via `init()` in `cmd/keep/cli/eval.go`, same pattern as the existing test and validate commands. No changes to `cmd/keep/main.go` needed.

**Dataset format** (matches design spec, includes `label` field):

```go
type EvalEntry struct {
	Input    EvalInput `json:"input"`
	Scope    string    `json:"scope"`
	Expected string    `json:"expected"` // "allow" or "deny"
	Label    string    `json:"label"`    // category label for confusion matrix
}

type EvalInput struct {
	Operation string         `json:"operation"`
	Params    map[string]any `json:"params"`
}
```

- [ ] **Step 1: Write failing test**

```go
// cmd/keep/cli/eval_test.go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalComputeMetrics(t *testing.T) {
	// Test the metrics computation directly
	results := []evalResult{
		{expected: "deny", actual: "deny"},   // TP
		{expected: "deny", actual: "deny"},   // TP
		{expected: "allow", actual: "allow"}, // TN
		{expected: "allow", actual: "deny"},  // FP
		{expected: "deny", actual: "allow"},  // FN
	}
	m := computeMetrics(results)
	if m.Total != 5 {
		t.Errorf("Total = %d, want 5", m.Total)
	}
	if m.Correct != 3 {
		t.Errorf("Correct = %d, want 3", m.Correct)
	}
	// Accuracy: 3/5 = 60%
	if m.Accuracy < 0.59 || m.Accuracy > 0.61 {
		t.Errorf("Accuracy = %.2f, want ~0.60", m.Accuracy)
	}
	// Precision: TP/(TP+FP) = 2/3
	if m.Precision < 0.66 || m.Precision > 0.67 {
		t.Errorf("Precision = %.2f, want ~0.67", m.Precision)
	}
	// Recall: TP/(TP+FN) = 2/3
	if m.Recall < 0.66 || m.Recall > 0.67 {
		t.Errorf("Recall = %.2f, want ~0.67", m.Recall)
	}
	if m.FalsePositives != 1 {
		t.Errorf("FalsePositives = %d, want 1", m.FalsePositives)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1", m.FalseNegatives)
	}
}

func TestEvalParseDataset(t *testing.T) {
	dataset := `[
		{"input": {"operation": "llm.text", "params": {"text": "bad"}}, "scope": "test", "expected": "deny", "label": "harmful"},
		{"input": {"operation": "llm.text", "params": {"text": "good"}}, "scope": "test", "expected": "allow", "label": "safe"}
	]`
	tmp := filepath.Join(t.TempDir(), "dataset.json")
	os.WriteFile(tmp, []byte(dataset), 0644)

	entries, err := parseDataset(tmp)
	if err != nil {
		t.Fatalf("parseDataset: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	if entries[0].Expected != "deny" {
		t.Errorf("entries[0].Expected = %q, want %q", entries[0].Expected, "deny")
	}
	if entries[1].Label != "safe" {
		t.Errorf("entries[1].Label = %q, want %q", entries[1].Label, "safe")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/keep/cli/ -run TestEval -v`
Expected: FAIL — `computeMetrics`, `parseDataset` undefined

- [ ] **Step 3: Implement eval command**

The implementer should:
1. Read the existing CLI command patterns (`cmd/keep/cli/validate.go`, `cmd/keep/cli/test.go`)
2. Implement `eval.go` following the same Cobra command pattern with `init()` registration
3. Implement `parseDataset`, `computeMetrics`, and the eval runner
4. Support flags: `--rule`, `--provider`, `--model`, `--concurrency`, `--timeout`, `--output json`

The eval command:
- Loads rules from the specified directory
- Parses the JSON dataset
- Constructs a judge provider from `--provider` flag
- For each dataset entry: builds a `Call`, evaluates with the engine, compares decision to expected
- Computes and prints accuracy, precision, recall, confusion matrix

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/keep/cli/ -run TestEval -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/keep/cli/eval.go cmd/keep/cli/eval_test.go
git commit -m "feat(cli): add keep eval command for judge quality measurement"
```

---

### Task 10: Integration Test and Final Verification

**Files:**
- Modify: existing test files as needed

- [ ] **Step 1: Run full test suite**

Run: `make test-unit`
Expected: ALL PASS

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: Clean

- [ ] **Step 3: Run benchmarks to verify no regression**

Run: `go test -bench=. -benchmem ./internal/engine/`
Expected: No significant regression for non-judge benchmarks

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git commit -m "fix: address integration issues from judge implementation"
```

---

### Dependency Graph

```
Task 1 (judge types) ──┬── Task 5 (Anthropic provider)
                       ├── Task 6 (OpenAI provider)
                       └── Task 3 (engine) ── Task 4 (engine tests)
                                │                    │
Task 2 (config) ────────────────┘                    ├── Task 7 (gateway/relay config)
                                                     ├── Task 8 (CLI fixtures)
                                                     └── Task 9 (keep eval)
                                                          │
                                                     Task 10 (integration)
```

Tasks 1 and 2 are independent and can be parallelized. Tasks 5, 6 depend only on Task 1. Task 3 depends on both Task 1 and Task 2. Tasks 7, 8, 9 can be parallelized after Task 3.
