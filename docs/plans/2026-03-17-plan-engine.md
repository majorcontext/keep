# Engine Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the core policy engine that compiles CEL expressions, evaluates calls against rules, manages rate counters, and produces audit entries. This is Keep's public API.

**Architecture:** The public `keep` package at the repo root exposes `Load`, `Engine`, `Evaluate`, and `ApplyMutations`. Internally, `internal/engine` handles evaluation logic, `internal/cel` manages the custom CEL environment, `internal/rate` provides sliding-window counters, and `internal/redact` handles field mutation. The engine consumes parsed config structs from `internal/config` (sub-project 1).

**Tech Stack:** Go, `github.com/google/cel-go` for CEL, `internal/config` for rule loading.

**Depends on:** Config package (sub-project 1) must be complete.

---

### Task 1: CEL environment -- standard setup

**Files:**
- Create: `internal/cel/env.go`
- Create: `internal/cel/env_test.go`

- [ ] **Step 1: Add cel-go dependency**

Run: `go get github.com/google/cel-go`

- [ ] **Step 2: Write failing tests**

Create `internal/cel/env_test.go`:
- `TestCompile_SimpleComparison` -- `"params.priority == 0"` compiles without error
- `TestCompile_StringMethod` -- `"params.text.contains('hello')"` compiles
- `TestCompile_LogicOperators` -- `"params.a > 1 && params.b < 10"` compiles
- `TestCompile_InvalidExpression` -- `"params.priority ==="` returns compilation error
- `TestEval_SimpleComparison` -- compile + eval `"params.priority == 0"` against `{priority: 0}` returns true
- `TestEval_StringContains` -- compile + eval string contains against matching input
- `TestEval_Collection_Any` -- `"params.to.exists(x, x.endsWith('@test.com'))"` with matching list
- `TestEval_Collection_All` -- `"params.to.all(x, x.endsWith('@test.com'))"` with all matching

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestCompile|TestEval -v ./internal/cel/'`
Expected: FAIL

- [ ] **Step 4: Implement CEL environment**

Create `internal/cel/env.go`:

```go
// Package cel provides Keep's custom CEL environment for rule expressions.
package cel

// Env is Keep's configured CEL environment with custom functions.
type Env struct { /* unexported cel.Env */ }

// NewEnv creates a new CEL environment with Keep's input variables
// (params, context) and all custom functions registered.
func NewEnv() (*Env, error)

// Program is a compiled CEL expression ready for evaluation.
type Program struct { /* unexported */ }

// Compile parses and type-checks a CEL expression string.
func (e *Env) Compile(expr string) (*Program, error)

// Eval evaluates a compiled program against the given params and context.
// Returns the boolean result. Returns an error if evaluation fails or
// the expression does not return a bool.
func (p *Program) Eval(params map[string]any, ctx map[string]any) (bool, error)
```

The environment declares:
- `params` as `map(string, dyn)`
- `context` as a structured type with `agent_id`, `user_id`, `timestamp`, `scope`, `direction`, `labels`

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='./internal/cel/'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cel/ go.mod go.sum
git commit -m "feat(cel): add CEL environment with standard functions"
```

---

### Task 2: CEL custom functions -- temporal

**Files:**
- Modify: `internal/cel/env.go`
- Create: `internal/cel/temporal.go`
- Create: `internal/cel/temporal_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/cel/temporal_test.go`:
- `TestInTimeWindow_Inside` -- 10:00 is inside 09:00-18:00 America/Los_Angeles
- `TestInTimeWindow_Outside` -- 02:00 is outside 09:00-18:00
- `TestInTimeWindow_NoWrap` -- 22:00-06:00 returns false (no midnight wrap)
- `TestDayOfWeek_UTC` -- known timestamp returns correct day
- `TestDayOfWeek_WithTimezone` -- same timestamp, different timezone, may return different day
- `TestInTimeWindow_CEL` -- full CEL expression `"inTimeWindow('09:00', '18:00', 'America/Los_Angeles')"` evaluated with context.timestamp inside window

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestInTimeWindow|TestDayOfWeek'`
Expected: FAIL

- [ ] **Step 3: Implement temporal functions**

Create `internal/cel/temporal.go`:
- `inTimeWindow(start, end, tz string) bool` -- uses `context.timestamp`
- `dayOfWeek() string` -- UTC, returns lowercase day name
- `dayOfWeek(tz string) string` -- in specified timezone

Register both as CEL custom functions in the environment.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestInTimeWindow|TestDayOfWeek'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cel/
git commit -m "feat(cel): add inTimeWindow and dayOfWeek functions"
```

---

### Task 3: CEL custom functions -- content patterns

**Files:**
- Create: `internal/cel/content.go`
- Create: `internal/cel/content_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/cel/content_test.go`:
- `TestContainsAny_Match` -- `containsAny("hello world", ["hello", "test"])` returns true
- `TestContainsAny_NoMatch` -- `containsAny("hello world", ["foo", "bar"])` returns false
- `TestContainsAny_CaseInsensitive` -- `containsAny("HELLO", ["hello"])` returns true
- `TestContainsPII_SSN` -- `containsPII("my ssn is 123-45-6789")` returns true
- `TestContainsPII_CreditCard` -- `containsPII("card 4111111111111111")` returns true
- `TestContainsPII_Clean` -- `containsPII("nothing sensitive here")` returns false
- `TestContainsPHI_Stub` -- `containsPHI("anything")` returns false (stub for M0)
- `TestEstimateTokens` -- `estimateTokens("hello world")` returns ~3 (11/4)

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestContains|TestEstimateTokens'`
Expected: FAIL

- [ ] **Step 3: Implement content functions**

Create `internal/cel/content.go`:
- `containsAny(field string, terms []string) bool` -- case-insensitive substring match
- `containsPII(field string) bool` -- regex library: SSN (`\d{3}-\d{2}-\d{4}`), credit card (major prefixes + Luhn), US phone
- `containsPHI(field string) bool` -- stub, always returns false, logs warning once
- `estimateTokens(field string) int` -- `len(field) / 4`

Register all as CEL custom functions.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestContains|TestEstimateTokens'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cel/
git commit -m "feat(cel): add containsAny, containsPII, containsPHI, estimateTokens"
```

---

### Task 4: Rate counter store

**Files:**
- Create: `internal/rate/counter.go`
- Create: `internal/rate/counter_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/rate/counter_test.go`:
- `TestCounter_Increment` -- increment a key, count returns 1
- `TestCounter_MultipleIncrements` -- increment same key 5 times, count returns 5
- `TestCounter_WindowExpiry` -- increment, advance time past window, count returns 0
- `TestCounter_SlidingWindow` -- increment at t=0, t=30s, t=90s; at t=90s with 1m window, count is 2 (t=0 expired)
- `TestCounter_IndependentKeys` -- two different keys have independent counts
- `TestCounter_Concurrent` -- 100 goroutines incrementing same key, final count is 100
- `TestCounter_GC` -- after GC, expired entries are removed

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestCounter'`
Expected: FAIL

- [ ] **Step 3: Implement counter store**

Create `internal/rate/counter.go`:

```go
// Package rate provides an in-memory sliding window counter store
// for Keep's rateCount() CEL function.
package rate

// Store is a thread-safe sliding window counter store.
type Store struct { /* unexported */ }

// NewStore creates a new counter store.
func NewStore() *Store

// Increment records a hit for the given key at the current time.
func (s *Store) Increment(key string)

// Count returns the number of hits for key within the given window duration.
func (s *Store) Count(key string, window time.Duration) int

// GC removes expired entries older than maxAge from all keys.
func (s *Store) GC(maxAge time.Duration)
```

Implementation: `sync.Map` of key to `[]time.Time` (sorted timestamps). `Count` scans from the end. `GC` prunes old entries. For tests, inject a clock interface so time can be controlled.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestCounter'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rate/
git commit -m "feat(rate): add in-memory sliding window counter store"
```

---

### Task 5: CEL rateCount function -- wired to counter store

**Files:**
- Create: `internal/cel/rate.go`
- Create: `internal/cel/rate_test.go`
- Modify: `internal/cel/env.go`

- [ ] **Step 1: Write failing tests**

Create `internal/cel/rate_test.go`:
- `TestRateCount_CEL` -- compile and eval `"rateCount('test:key', '1h') > 5"` with a store pre-loaded with 6 hits, returns true
- `TestRateCount_UnderLimit` -- same expression, 3 hits, returns false
- `TestRateCount_WindowParsing` -- `"30s"`, `"5m"`, `"1h"`, `"24h"` all parse correctly
- `TestRateCount_InvalidWindow` -- `"25h"` (exceeds max), `"0s"`, `"abc"` return eval errors

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestRateCount'`
Expected: FAIL

- [ ] **Step 3: Implement rateCount**

Create `internal/cel/rate.go`:
- Parse window string (integer + unit: s/m/h)
- Register `rateCount(key string, window string) int` as CEL custom function
- The function requires a `*rate.Store` injected into the CEL environment at creation
- On each call, increment the counter and return the count

Modify `internal/cel/env.go`:
- `NewEnv` takes `...EnvOption` including `WithRateStore(*rate.Store)`

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestRateCount'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cel/ internal/rate/
git commit -m "feat(cel): add rateCount function wired to counter store"
```

---

### Task 6: CEL profile alias resolution

**Files:**
- Create: `internal/cel/alias.go`
- Create: `internal/cel/alias_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/cel/alias_test.go`:
- `TestResolveAliases_Simple` -- `"priority == 0"` with linear profile becomes `"params.priority == 0"`
- `TestResolveAliases_Multiple` -- `"team in ['A'] && priority > 1"` resolves both
- `TestResolveAliases_NoProfile` -- expression unchanged when no aliases
- `TestResolveAliases_ExplicitParams` -- `"params.priority == 0"` unchanged (already qualified)
- `TestResolveAliases_BuiltinNotReplaced` -- `"size(params.to) > 5"` -- `size` is not an alias
- `TestResolveAliases_NestedInFunction` -- `"containsAny(title, ['a'])"` resolves `title` to `params.title`

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestResolveAliases'`
Expected: FAIL

- [ ] **Step 3: Implement alias resolution**

Create `internal/cel/alias.go`:

```go
// ResolveAliases rewrites a CEL expression string, replacing unqualified
// identifiers that match alias names with their params.* paths.
func ResolveAliases(expr string, aliases map[string]string) string
```

Implementation approach: use the CEL parser to get the AST, walk it, and rewrite identifiers that match alias keys. If the identifier is already `params.*` or `context.*`, or is a known function/macro name, skip it. Serialize the rewritten AST back to a string.

Alternative simpler approach for M0: token-level rewriting using `cel-go`'s lexer. Walk tokens, replace `IDENT` tokens that match alias names and aren't preceded by `.` (which would mean they're a field access, not a bare identifier).

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestResolveAliases'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cel/
git commit -m "feat(cel): add profile alias resolution for CEL expressions"
```

---

### Task 7: Redaction engine

**Files:**
- Create: `internal/redact/redact.go`
- Create: `internal/redact/redact_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/redact/redact_test.go`:
- `TestRedact_SinglePattern` -- AWS key pattern, replaces match with `[REDACTED:AWS_KEY]`
- `TestRedact_MultiplePatterns` -- two patterns applied sequentially to same field
- `TestRedact_NoMatch` -- pattern doesn't match, field unchanged, no mutations returned
- `TestRedact_FieldPath` -- targets `params.content`, returns mutation with correct path
- `TestApplyMutations_Simple` -- apply one mutation to a params map, field updated
- `TestApplyMutations_NestedPath` -- apply mutation to `params.input.command`
- `TestApplyMutations_MissingPath` -- mutation targets non-existent field, silently skipped
- `TestApplyMutations_OriginalUnmodified` -- original map not changed

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestRedact|TestApplyMutations'`
Expected: FAIL

- [ ] **Step 3: Implement redaction**

Create `internal/redact/redact.go`:

```go
// Package redact handles regex-based field redaction for Keep's redact action.
package redact

// Mutation describes a single field change.
type Mutation struct {
	Path     string
	Original string
	Replaced string
}

// Apply runs compiled patterns against the string value at the given field path
// in params. Returns the list of mutations (empty if no patterns matched).
// Does not modify params.
func Apply(params map[string]any, target string, patterns []CompiledPattern) []Mutation

// CompiledPattern is a pre-compiled redact pattern.
type CompiledPattern struct {
	Regex   *regexp.Regexp
	Replace string
}

// CompilePatterns compiles a list of config redact patterns into CompiledPatterns.
func CompilePatterns(patterns []config.RedactPattern) ([]CompiledPattern, error)

// ApplyMutations returns a new params map with mutations applied.
// The original map is not modified.
func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any
```

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestRedact|TestApplyMutations'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/redact/
git commit -m "feat(redact): add regex-based field redaction and mutation application"
```

---

### Task 8: Glob matching for operation names

**Files:**
- Create: `internal/engine/glob.go`
- Create: `internal/engine/glob_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/engine/glob_test.go`:
- `TestGlob_Exact` -- `"create_issue"` matches `"create_issue"`, not `"delete_issue"`
- `TestGlob_Star` -- `"create_*"` matches `"create_issue"`, `"create_comment"`, not `"delete_issue"`
- `TestGlob_Question` -- `"llm.tool_?"` matches `"llm.tool_a"`, not `"llm.tool_use"` (? = single char)
- `TestGlob_StarAll` -- `"*"` matches everything
- `TestGlob_DotStar` -- `"llm.*"` matches `"llm.request"`, `"llm.tool_use"`, not `"create_issue"`
- `TestGlob_Empty` -- empty pattern matches everything (no operation filter)

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestGlob'`
Expected: FAIL

- [ ] **Step 3: Implement glob matching**

Create `internal/engine/glob.go`:

```go
// GlobMatch returns true if the operation name matches the glob pattern.
// Supports * (any sequence) and ? (any single character).
// An empty pattern matches everything.
func GlobMatch(pattern, name string) bool
```

Use `path.Match` from the standard library (it supports `*` and `?` glob semantics). Wrap it to handle the empty-pattern-matches-all case.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestGlob'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): add glob matching for operation names"
```

---

### Task 9: Core evaluation loop

**Files:**
- Create: `internal/engine/eval.go`
- Create: `internal/engine/eval_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/engine/eval_test.go`. These tests construct engine internals directly (not via `keep.Load`) to test the evaluation loop in isolation:

- `TestEval_AllowNoRules` -- empty rule list, returns allow
- `TestEval_DenyMatchesOperation` -- deny rule with operation glob, call matches, returns deny
- `TestEval_DenyMatchesWhen` -- deny rule with when expression, call matches, returns deny with rule name and message
- `TestEval_DenyShortCircuit` -- two deny rules, first matches, second is never evaluated
- `TestEval_Log` -- log rule matches, returns allow with audit entry recording the log
- `TestEval_Redact` -- redact rule matches, returns redact with mutations
- `TestEval_RedactAccumulates` -- two redact rules, both match, mutations from both accumulated
- `TestEval_OperationMismatch` -- rule operation doesn't match call, rule skipped
- `TestEval_WhenFalse` -- rule when expression evaluates to false, rule skipped
- `TestEval_AuditAlwaysPopulated` -- every result has an audit entry with rules_evaluated

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestEval'`
Expected: FAIL

- [ ] **Step 3: Implement evaluation loop**

Create `internal/engine/eval.go`:

```go
// Evaluator runs the rule evaluation loop.
type Evaluator struct {
	celEnv  *cel.Env
	rules   []compiledRule  // pack rules + inline, in order
	mode    config.Mode
	onError config.ErrorMode
}

// compiledRule pairs a parsed rule with its compiled CEL program (if any)
// and compiled redact patterns (if any).
type compiledRule struct {
	rule     config.Rule
	program  *cel.Program    // nil if no when clause
	patterns []redact.CompiledPattern // nil if not a redact rule
}

// Evaluate runs the call through all rules and returns the result.
func (ev *Evaluator) Evaluate(call Call) EvalResult
```

The loop follows the PRD evaluation flow:
1. For each rule in order
2. Check operation glob -- skip if no match (mark as `skipped` in RuleResult)
3. Evaluate `when` (if present) -- skip if false
4. If matched: check action
   - deny: return immediately with deny result
   - log: record in audit, continue
   - redact: run redaction, accumulate mutations, continue
5. After all rules: return allow (with mutations if any redact fired)
6. Build `AuditEntry` with `rules_evaluated` list

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestEval'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): implement core evaluation loop"
```

---

### Task 10: Error handling in evaluation

**Files:**
- Modify: `internal/engine/eval.go`
- Create: `internal/engine/eval_error_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/engine/eval_error_test.go`:
- `TestEval_CELError_FailClosed` -- expression that causes a type error, on_error=closed, returns deny with error message
- `TestEval_CELError_FailOpen` -- same expression, on_error=open, returns allow with error logged in audit
- `TestEval_AuditOnly_ErrorAllowed` -- mode=audit_only, expression error, always allows
- `TestEval_AuditOnly_DenyNotEnforced` -- mode=audit_only, deny rule matches, returns allow (but audit records the would-be deny)
- `TestEval_Timeout` -- expression that somehow takes too long (may need to mock the timeout mechanism)

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestEval_CELError|TestEval_AuditOnly|TestEval_Timeout'`
Expected: FAIL

- [ ] **Step 3: Implement error handling**

Modify `internal/engine/eval.go`:
- Wrap CEL evaluation in a recover block for panics
- Check mode: if `audit_only`, evaluate all rules but always return allow (with audit)
- On CEL error: check `on_error` setting
  - `closed`: return deny with error context in message
  - `open`: return allow, log error in audit entry
- Add a context-based timeout for expression evaluation (5ms)

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestEval_CELError|TestEval_AuditOnly|TestEval_Timeout'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): add error handling with fail-open/closed and audit_only"
```

---

### Task 11: Public API -- keep.go

**Files:**
- Create: `keep.go`
- Create: `keep_test.go`
- Create: `testdata/rules/linear.yaml`
- Create: `testdata/rules/anthropic.yaml`
- Create: `testdata/profiles/linear.yaml`
- Create: `testdata/packs/linear-safe-defaults.yaml`

- [ ] **Step 1: Write failing tests**

Create `keep_test.go` -- integration tests for the public API:
- `TestLoad_ValidRules` -- loads rules dir, returns engine with expected scopes
- `TestLoad_InvalidRules` -- invalid CEL expression, returns error
- `TestLoad_WithOptions` -- `WithProfilesDir`, `WithPacksDir` options work
- `TestEngine_Scopes` -- returns correct scope names
- `TestEvaluate_Allow` -- call that passes all rules
- `TestEvaluate_Deny` -- call that hits a deny rule, returns deny with rule name and message
- `TestEvaluate_Redact` -- call that hits a redact rule, returns mutations
- `TestEvaluate_UnknownScope` -- returns error
- `TestApplyMutations` -- applies mutations correctly
- `TestReload` -- modify rules on disk, call Reload, new rules active

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestLoad|TestEngine|TestEvaluate|TestApplyMutations|TestReload -v ./'`
Expected: FAIL

- [ ] **Step 3: Implement public API**

Create `keep.go`:

```go
// Package keep is an API-level policy engine for AI agents.
package keep

// All public types: Call, Context, EvalResult, Decision, Mutation,
// AuditEntry, RuleResult, Engine, Option.
// See PRD v0.5 Go API surface for the complete type definitions.

func Load(rulesDir string, opts ...Option) (*Engine, error) {
	// 1. Parse options (profilesDir, packsDir)
	// 2. Call config.LoadAll
	// 3. For each scope: compile CEL expressions (with alias resolution),
	//    compile redact patterns, build evaluator
	// 4. Create rate store
	// 5. Return Engine
}

func (e *Engine) Evaluate(call Call, scope string) (EvalResult, error) {
	// 1. Look up scope evaluator
	// 2. Delegate to evaluator.Evaluate
	// 3. Return result
}

func (e *Engine) Reload() error {
	// 1. Re-run Load with same options
	// 2. Swap evaluators atomically (sync.RWMutex)
	// 3. Keep existing rate store (counters survive reload)
}

func (e *Engine) Scopes() []string {
	// Return sorted scope names
}

func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any {
	// Delegate to internal/redact.ApplyMutations
}
```

- [ ] **Step 4: Create testdata**

Rule files, profile, and starter pack fixtures for the integration tests. Use the Linear and Anthropic examples from the PRD.

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestLoad|TestEngine|TestEvaluate|TestApplyMutations|TestReload -v ./'`
Expected: PASS

- [ ] **Step 6: Run all tests**

Run: `make test-unit`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add keep.go keep_test.go testdata/
git commit -m "feat: add public API -- Load, Engine, Evaluate, ApplyMutations"
```

---

### Task 12: Rate counter GC goroutine

**Files:**
- Modify: `keep.go`
- Modify: `internal/rate/counter.go`
- Create: `internal/rate/gc_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/rate/gc_test.go`:
- `TestStore_StartGC` -- start GC, add entries, advance time, verify expired entries cleaned up
- `TestStore_StopGC` -- start then stop GC, verify goroutine exits cleanly

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestStore_StartGC|TestStore_StopGC'`
Expected: FAIL

- [ ] **Step 3: Implement GC lifecycle**

Add to `internal/rate/counter.go`:

```go
// StartGC begins periodic garbage collection. Runs every interval,
// removing entries older than maxAge. Call StopGC to stop.
func (s *Store) StartGC(interval, maxAge time.Duration)

// StopGC stops the periodic garbage collection goroutine.
func (s *Store) StopGC()
```

Modify `keep.go`:
- `Load` starts GC on the rate store (60s interval, 24h max age)
- `Engine` has a `Close()` method that stops GC

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestStore_StartGC|TestStore_StopGC'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add keep.go internal/rate/
git commit -m "feat(rate): add periodic GC for expired counter entries"
```
