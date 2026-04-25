# Verdict Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache judge verdicts so identical content is not re-judged across turns, eliminating O(N^2) judge calls in multi-turn conversations.

**Architecture:** A `judge.Cache` wraps any `judge.Provider`, keyed on `sha256(model + prompt + content)`. The `Cached` flag flows through `judge.Verdict` → `judgeAdapter` → `engine.JudgeResult` → `JudgeAudit` so cache hits appear in the audit trail. Gateway and relay wire the cache automatically.

**Tech Stack:** Go stdlib only (`crypto/sha256`, `sync`)

---

## File Structure

```
judge/
  judge.go        # Modify: add Cached bool to Verdict
  cache.go        # Create: Cache wrapper
  cache_test.go   # Create: unit tests
internal/engine/
  eval.go         # Modify: add Cached to JudgeResult + JudgeAudit, copy in evaluator
  eval_test.go    # Modify: add test for Cached propagation
keep.go           # Modify: judgeAdapter copies Cached field
cmd/keep-llm-gateway/main.go  # Modify: wrap provider with judge.NewCache()
cmd/keep-mcp-relay/main.go    # Modify: same
examples/judge-demo/demo.sh   # Modify: print_audit shows "(cached)"
```

## Dependency Graph

```
Task 1 (Verdict.Cached) ──┬──→ Task 2 (Cache implementation)
                          ├──→ Task 3 (JudgeResult.Cached + adapter)
                          │         │
                          │         ▼
                          │    Task 4 (JudgeAudit.Cached + evaluator)
                          │
                          └──→ Task 5 (Gateway/relay/demo wiring) — after Task 2
```

Tasks 2, 3 are independent of each other (both depend on Task 1).
Task 4 depends on Task 3. Task 5 depends on Tasks 2 and 4.

---

### Task 1: Add Cached Field to judge.Verdict

**Files:**
- Modify: `judge/judge.go:18-22`

- [ ] **Step 1: Add the Cached field**

In `judge/judge.go`, add `Cached` to the `Verdict` struct:

```go
// Verdict is the judge's response.
type Verdict struct {
	Decision Decision // allow or deny
	Reason   string   // LLM's reasoning
	Usage    Usage    // Token consumption
	Cached   bool     // true if returned from cache
}
```

- [ ] **Step 2: Verify existing tests still pass**

Run: `go test ./judge/... -count=1`
Expected: PASS (additive change, no breakage)

- [ ] **Step 3: Commit**

```bash
git add judge/judge.go
git commit -m "feat(judge): add Cached field to Verdict type"
```

---

### Task 2: Implement judge.Cache

**Files:**
- Create: `judge/cache.go`
- Create: `judge/cache_test.go`

- [ ] **Step 1: Write the tests**

Create `judge/cache_test.go`:

```go
package judge_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/majorcontext/keep/judge"
)

// countingProvider tracks how many times Judge is called.
type countingProvider struct {
	calls   atomic.Int64
	verdict judge.Verdict
}

func (p *countingProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	p.calls.Add(1)
	return p.verdict, nil
}

func TestCacheHit(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Deny, Reason: "bad"}}
	cache := judge.NewCache(inner)

	req := judge.Request{Model: "haiku", Prompt: "safe?", Content: "hello"}

	// First call — miss.
	v1, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if v1.Cached {
		t.Error("first call should not be cached")
	}
	if v1.Decision != judge.Deny {
		t.Errorf("Decision = %q, want deny", v1.Decision)
	}

	// Second call — hit.
	v2, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !v2.Cached {
		t.Error("second call should be cached")
	}
	if v2.Decision != judge.Deny {
		t.Errorf("Decision = %q, want deny", v2.Decision)
	}
	if v2.Reason != "bad" {
		t.Errorf("Reason = %q, want %q", v2.Reason, "bad")
	}

	if inner.calls.Load() != 1 {
		t.Errorf("provider called %d times, want 1", inner.calls.Load())
	}
}

func TestCacheDifferentKeys(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	cache := judge.NewCache(inner)

	req1 := judge.Request{Model: "haiku", Prompt: "safe?", Content: "hello"}
	req2 := judge.Request{Model: "haiku", Prompt: "safe?", Content: "goodbye"}

	cache.Judge(context.Background(), req1)
	cache.Judge(context.Background(), req2)

	if inner.calls.Load() != 2 {
		t.Errorf("provider called %d times, want 2 (different content)", inner.calls.Load())
	}
}

func TestCacheEviction(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	cache := judge.NewCache(inner, judge.WithMaxSize(2))

	req1 := judge.Request{Model: "m", Prompt: "p", Content: "a"}
	req2 := judge.Request{Model: "m", Prompt: "p", Content: "b"}
	req3 := judge.Request{Model: "m", Prompt: "p", Content: "c"}

	cache.Judge(context.Background(), req1) // 1 call
	cache.Judge(context.Background(), req2) // 2 calls
	cache.Judge(context.Background(), req3) // 3 calls, evicts req1

	// req1 should be evicted — re-calling it should trigger a new provider call.
	cache.Judge(context.Background(), req1) // 4 calls
	if inner.calls.Load() != 4 {
		t.Errorf("provider called %d times, want 4 (req1 evicted)", inner.calls.Load())
	}

	// req2 should still be cached.
	cache.Judge(context.Background(), req2)
	if inner.calls.Load() != 4 {
		t.Errorf("provider called %d times, want 4 (req2 cached)", inner.calls.Load())
	}
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	callCount := atomic.Int64{}
	errProvider := &errorThenSuccessProvider{calls: &callCount}
	cache := judge.NewCache(errProvider)

	req := judge.Request{Model: "m", Prompt: "p", Content: "c"}

	// First call errors.
	_, err := cache.Judge(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on first call")
	}

	// Second call should retry (error not cached).
	v, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if v.Decision != judge.Allow {
		t.Errorf("Decision = %q, want allow", v.Decision)
	}
	if callCount.Load() != 2 {
		t.Errorf("provider called %d times, want 2", callCount.Load())
	}
}

type errorThenSuccessProvider struct {
	calls *atomic.Int64
}

func (p *errorThenSuccessProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	n := p.calls.Add(1)
	if n == 1 {
		return judge.Verdict{}, context.DeadlineExceeded
	}
	return judge.Verdict{Decision: judge.Allow, Reason: "ok"}, nil
}

func TestCacheConcurrent(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	cache := judge.NewCache(inner)

	req := judge.Request{Model: "m", Prompt: "p", Content: "c"}

	// Prime the cache.
	cache.Judge(context.Background(), req)

	// Concurrent reads should all hit cache.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cache.Judge(context.Background(), req)
			if err != nil {
				t.Errorf("concurrent call: %v", err)
			}
			if !v.Cached {
				t.Error("concurrent call should be cached")
			}
		}()
	}
	wg.Wait()

	if inner.calls.Load() != 1 {
		t.Errorf("provider called %d times, want 1", inner.calls.Load())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./judge/... -run TestCache -count=1`
Expected: FAIL (judge.NewCache, judge.WithMaxSize not defined)

- [ ] **Step 3: Implement the cache**

Create `judge/cache.go`:

```go
package judge

import (
	"context"
	"crypto/sha256"
	"sync"
)

const defaultMaxSize = 10_000

// Cache wraps a Provider with an in-memory verdict cache.
// Identical requests (same model, prompt, content) return cached verdicts
// without calling the underlying provider. Errors are not cached.
type Cache struct {
	provider Provider
	mu       sync.RWMutex
	entries  map[[32]byte]Verdict
	order    [][32]byte // insertion order for oldest-first eviction
	maxSize  int
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithMaxSize sets the maximum number of cached verdicts.
// When exceeded, the oldest entries are evicted. Default: 10,000.
func WithMaxSize(n int) CacheOption {
	return func(c *Cache) { c.maxSize = n }
}

// NewCache creates a caching wrapper around a Provider.
func NewCache(provider Provider, opts ...CacheOption) *Cache {
	c := &Cache{
		provider: provider,
		entries:  make(map[[32]byte]Verdict),
		maxSize:  defaultMaxSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Judge returns a cached verdict if available, otherwise calls the
// underlying provider and caches the result. Errors are not cached.
func (c *Cache) Judge(ctx context.Context, req Request) (Verdict, error) {
	key := cacheKey(req)

	c.mu.RLock()
	if v, ok := c.entries[key]; ok {
		c.mu.RUnlock()
		v.Cached = true
		return v, nil
	}
	c.mu.RUnlock()

	v, err := c.provider.Judge(ctx, req)
	if err != nil {
		return v, err
	}

	c.mu.Lock()
	// Guard against concurrent misses on the same key both inserting.
	if _, exists := c.entries[key]; !exists {
		// Evict oldest if at capacity.
		for len(c.entries) >= c.maxSize && len(c.order) > 0 {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.entries[key] = v
		c.order = append(c.order, key)
	}
	c.mu.Unlock()

	return v, nil
}

func cacheKey(req Request) [32]byte {
	h := sha256.New()
	h.Write([]byte(req.Model))
	h.Write([]byte{0})
	h.Write([]byte(req.Prompt))
	h.Write([]byte{0})
	h.Write([]byte(req.Content))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./judge/... -run TestCache -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add judge/cache.go judge/cache_test.go
git commit -m "feat(judge): implement verdict cache with oldest-first eviction"
```

---

### Task 3: Propagate Cached Through JudgeResult and Adapter

**Files:**
- Modify: `internal/engine/eval.go:105-111` (JudgeResult struct)
- Modify: `keep.go:412-430` (judgeAdapter)

- [ ] **Step 1: Add Cached to JudgeResult**

In `internal/engine/eval.go`, add `Cached bool` to `JudgeResult`:

```go
// JudgeResult holds the raw result from a judge call at the engine level.
type JudgeResult struct {
	Decision     string
	Reason       string
	InputTokens  int
	OutputTokens int
	Cached       bool
}
```

- [ ] **Step 2: Update judgeAdapter to copy Cached**

In `keep.go`, update `judgeAdapter` to copy `v.Cached`:

```go
func judgeAdapter(fn JudgeFunc) engine.JudgeHandler {
	return func(ctx context.Context, model, prompt, content string) (engine.JudgeResult, error) {
		v, err := fn(ctx, judge.Request{
			Prompt:  prompt,
			Content: content,
			Model:   model,
		})
		if err != nil {
			return engine.JudgeResult{}, err
		}
		return engine.JudgeResult{
			Decision:     string(v.Decision),
			Reason:       v.Reason,
			InputTokens:  v.Usage.InputTokens,
			OutputTokens: v.Usage.OutputTokens,
			Cached:       v.Cached,
		}, nil
	}
}
```

- [ ] **Step 3: Verify tests pass**

Run: `go test ./... -count=1 2>&1 | tail -5`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/engine/eval.go keep.go
git commit -m "feat(judge): propagate Cached field through JudgeResult and adapter"
```

---

### Task 4: Add Cached to JudgeAudit and Evaluator

**Files:**
- Modify: `internal/engine/eval.go:89-97` (JudgeAudit struct)
- Modify: `internal/engine/eval.go:484-490` (audit population after successful judge)
- Modify: `internal/engine/eval_test.go` (new test)

- [ ] **Step 1: Write failing test**

Add to `internal/engine/eval_test.go`:

```go
func TestEvaluateJudgeCachedAudit(t *testing.T) {
	rules := []config.Rule{{
		Name:   "cached-rule",
		Match:  config.Match{Operation: "llm.text"},
		Action: config.ActionJudge,
		Judge: &config.JudgeSpec{
			Model:   "haiku",
			Prompt:  "safe?",
			Timeout: "5s",
			OnError: "closed",
		},
	}}

	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatalf("NewEnv: %v", err)
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	ev.SetJudgeFunc(func(_ context.Context, model, prompt, content string) (JudgeResult, error) {
		return JudgeResult{
			Decision: "allow",
			Reason:   "looks good",
			Cached:   true,
		}, nil
	})

	result := ev.Evaluate(context.Background(), Call{
		Operation: "llm.text",
		Params:    map[string]any{"text": "hello"},
	})

	for _, r := range result.Audit.RulesEvaluated {
		if r.Name == "cached-rule" && r.Judge != nil {
			if !r.Judge.Cached {
				t.Error("JudgeAudit.Cached should be true")
			}
			return
		}
	}
	t.Error("cached-rule not found in audit")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestEvaluateJudgeCachedAudit -count=1`
Expected: FAIL (JudgeAudit has no Cached field)

- [ ] **Step 3: Add Cached to JudgeAudit and populate it**

In `internal/engine/eval.go`, update `JudgeAudit`:

```go
type JudgeAudit struct {
	Model     string     `json:"model"`
	Verdict   string     `json:"verdict"`
	Reason    string     `json:"reason"`
	Cached    bool       `json:"cached,omitempty"`
	LatencyMS int64      `json:"latency_ms"`
	Usage     JudgeUsage `json:"usage"`
	Error     string     `json:"error,omitempty"`
}
```

Then at the audit population after a successful judge call (around line 484-490), add `audit.Cached = jr.Cached`:

```go
			audit.Verdict = jr.Decision
			audit.Reason = jr.Reason
			audit.Cached = jr.Cached
			audit.Usage = JudgeUsage{
				InputTokens:  jr.InputTokens,
				OutputTokens: jr.OutputTokens,
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestEvaluateJudgeCachedAudit -count=1`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -count=1 2>&1 | tail -5`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/engine/eval.go internal/engine/eval_test.go
git commit -m "feat(judge): add Cached field to JudgeAudit and populate from JudgeResult"
```

---

### Task 5: Wire Cache in Gateway, Relay, and Demo

**Files:**
- Modify: `cmd/keep-llm-gateway/main.go:47-66`
- Modify: `cmd/keep-mcp-relay/main.go` (same pattern)
- Modify: `examples/judge-demo/demo.sh` (print_audit)

- [ ] **Step 1: Update gateway main.go**

In `cmd/keep-llm-gateway/main.go`, wrap the provider with `judge.NewCache()`. Add import `"github.com/majorcontext/keep/judge"`. Change the two wiring blocks:

For anthropic (around line 56-57):
```go
				p := anthropicjudge.New(apiKey, jopts...)
				cached := judge.NewCache(p)
				engineOpts = append(engineOpts, keep.WithJudge(cached.Judge))
```

For openai (around line 63-64):
```go
				p := openaijudge.New(apiKey, jopts...)
				cached := judge.NewCache(p)
				engineOpts = append(engineOpts, keep.WithJudge(cached.Judge))
```

- [ ] **Step 2: Update relay main.go**

Same change in `cmd/keep-mcp-relay/main.go`. Add import `"github.com/majorcontext/keep/judge"`. Wrap both provider constructions with `judge.NewCache()`.

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/keep-llm-gateway && go build ./cmd/keep-mcp-relay && echo OK`
Expected: OK

- [ ] **Step 4: Update demo print_audit to show cached**

In `examples/judge-demo/demo.sh`, update the judge verdict rendering in the `print_audit` function. Find the line that prints the verdict and add a "(cached)" indicator:

Change:
```python
            print(f'    {magenta}\u2728 {model}{reset}: {vc}{verdict}{reset} ({ms}ms) \u2014 {dim}{reason}{reset}')
```

To:
```python
            cached = ' (cached)' if j.get('cached') else ''
            print(f'    {magenta}\u2728 {model}{reset}: {vc}{verdict}{reset} ({ms}ms){cached} \u2014 {dim}{reason}{reset}')
```

- [ ] **Step 5: Run full test suite**

Run: `make test-unit 2>&1 | tail -5`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/keep-llm-gateway/main.go cmd/keep-mcp-relay/main.go examples/judge-demo/demo.sh
git commit -m "feat(judge): wire verdict cache in gateway, relay, and demo"
```
