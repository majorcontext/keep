# Verdict Cache

**Date:** 2026-04-25
**Status:** Approved
**Goal:** Cache judge verdicts so identical content is not re-judged across turns in a conversation

## Context

The gateway decomposes every message in a request into separate `llm.text` spans. In a multi-turn conversation, turn N includes all messages from turns 1 through N-1. Each text span hits every judge rule. Without caching, the same content is re-judged on every turn — O(N^2) judge calls over a conversation. This wastes tokens, adds latency, and can cause timeouts on large context blocks.

## Design

### Cache Wrapper

A `judge.Cache` wraps any `judge.Provider` and returns a `Provider` with the same interface. Caching is transparent to consumers.

```go
// judge/cache.go

type Cache struct {
    provider Provider
    mu       sync.RWMutex
    entries  map[[32]byte]Verdict
    order    [][32]byte  // insertion order for eviction
    maxSize  int
}

func NewCache(provider Provider, opts ...CacheOption) *Cache
func (c *Cache) Judge(ctx context.Context, req Request) (Verdict, error)

type CacheOption func(*Cache)
func WithMaxSize(n int) CacheOption
```

### Cache Key

`sha256(model + "\x00" + prompt + "\x00" + content)`

Null byte separators prevent collisions between adjacent fields. The key includes all three inputs to the judge — if any differ, the verdict is re-evaluated.

### Eviction

Oldest-first eviction when `len(entries) > maxSize`. Default max size: 10,000 entries. Configurable via `WithMaxSize(n)`.

Not true LRU (no promotion on hit). Simpler and sufficient — verdict entries are small (~200 bytes each) and the working set in a conversation is bounded by message count.

### No TTL

Same content + same prompt + same model = same verdict. The policy judgment is deterministic for a given input. The cache lives as long as the engine.

On `engine.Reload()`, the cache persists. Changed rules produce different prompts, which produce different cache keys. Stale entries from old prompts evict naturally via oldest-first eviction.

### Concurrency

`sync.RWMutex` — cache lookups take read lock, inserts take write lock. The judge call itself happens outside the lock (only the key is computed under read lock, then released before the HTTP call).

**Cache stampede:** Concurrent misses on the same key will all call the provider independently. The last writer wins. This is acceptable — stampede only happens within a single turn's concurrent spans, and subsequent turns hit the cache. Singleflight is a future optimization if needed.

### Audit Trail

`JudgeAudit` gains a `Cached bool` field:

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

**Propagation path:** `judge.Verdict` gains a `Cached bool` field. The `Cache.Judge` method sets `Cached: true` on cache hits. The existing `judgeAdapter` in `keep.go` copies `v.Cached` to `engine.JudgeResult.Cached`. The evaluator copies `jr.Cached` to `audit.Cached`. This is an additive, backward-compatible change to `judge.Verdict`.

Cache hit latency is measured by the existing timer in `eval.go` which wraps the `judgeFunc` call. Cache hits return near-instantly, so `LatencyMS` will be ~0 — this is intentional and makes hits visibly fast in the audit trail.

### Wiring

The cache wraps the provider at the `keep.go` / `main.go` level:

```go
// Library
provider := anthropic.New(os.Getenv("ANTHROPIC_API_KEY"))
cached := judge.NewCache(provider)
engine, _ := keep.Load("./rules", keep.WithJudge(cached.Judge))

// Gateway/relay main.go — wrap before passing to WithJudge
p := anthropicjudge.New(apiKey)
cached := judge.NewCache(p)
engineOpts = append(engineOpts, keep.WithJudge(cached.Judge))
```

### What Changes

- **New:** `judge/cache.go` — Cache type, NewCache, CacheOption, WithMaxSize
- **New:** `judge/cache_test.go` — unit tests
- **Modify:** `judge/judge.go` — `Verdict` gains `Cached bool`
- **Modify:** `internal/engine/eval.go` — `JudgeAudit` gains `Cached bool`, `JudgeResult` gains `Cached bool`, evaluator copies `jr.Cached` to `audit.Cached`
- **Modify:** `keep.go` — `judgeAdapter` copies `v.Cached` to `JudgeResult.Cached`
- **Modify:** `cmd/keep-llm-gateway/main.go` — wrap provider with `judge.NewCache()`
- **Modify:** `cmd/keep-mcp-relay/main.go` — same
- **Modify:** `examples/judge-demo/demo.sh` — `print_audit` shows "(cached)" for cache hits

### What Doesn't Change

- `judge.Provider` interface — unchanged
- `judge.Request` type — unchanged
- `judge.Verdict` — gains `Cached bool` (additive, backward-compatible)
- Engine internals — cache is external, injected via the same `WithJudge()` option
- `keep test` fixtures — mock judge has no cache
- `keep eval` — evaluates fresh each time (no cache)

## Out of Scope

- **Persistent cache** — in-memory only, no disk or Redis
- **Cache invalidation API** — not needed; prompt changes = different key, engine restart = fresh cache
- **Cache metrics** — hit/miss counters are a future enhancement
- **Negative caching (errors)** — errors are not cached; only successful verdicts
