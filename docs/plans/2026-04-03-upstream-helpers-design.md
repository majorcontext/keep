# Upstream Evaluation Helpers from Moat

**Date:** 2026-04-03
**Status:** Approved
**Origin:** `internal/keep/evaluate.go` in `github.com/majorcontext/moat`

## Context

Moat has internal helpers for safe evaluation and call normalization that both Moat and Gatekeeper need. These depend only on Keep's own types (`*Engine`, `Call`, `EvalResult`, `Decision`) and stdlib. They belong in the Keep library so any consumer gets them.

## What Ships

Three functions added to the top-level `keep` package. No new subpackage.

### SafeEvaluate

```go
func SafeEvaluate(eng *Engine, call Call, scope string) (EvalResult, error)
```

Wraps `Engine.Evaluate` with `defer/recover`. Policy files are user-authored — a bad regex or nil pointer shouldn't crash the host process.

- On panic: returns `EvalResult{Decision: Deny}` and an error describing the panic (fail-closed)
- On normal error (e.g., unknown scope): returns the error from `Engine.Evaluate`
- On success: returns the result unchanged

Mirrors `Engine.Evaluate` signature exactly — drop-in replacement where callers want safety.

### NewHTTPCall

```go
func NewHTTPCall(method, host, path string) Call
```

Constructs a `Call` for HTTP request policy evaluation.

- `Operation`: `"GET api.github.com/repos"` (method uppercased, space-separated from host+path; path is expected to include a leading slash)
- `Params`: `{"method": "GET", "host": "api.github.com", "path": "/repos"}`
- `Context.Timestamp`: `time.Now()`
- `Context.Scope`: not set — caller's responsibility

Does **not** set scope. Scope naming is a deployment decision (Moat uses `"http-"+host`, others may differ). Callers assign `call.Context.Scope` after construction if their CEL rules reference `context.scope`.

### NewMCPCall

```go
func NewMCPCall(tool string, params map[string]any) Call
```

Constructs a `Call` for MCP tool-use policy evaluation.

- `Operation`: the tool name as-is (e.g., `"delete_issue"`)
- `Params`: passed through directly (may be nil)
- `Context.Timestamp`: `time.Now()`
- `Context.Scope`: not set — caller's responsibility

Same scope convention as `NewHTTPCall`.

## File Layout

- `helpers.go` — implementations, alongside `keep.go` in the top-level package
- `helpers_test.go` — tests

## Tests

Ported from Moat's `internal/keep/evaluate_test.go`, adapted for the new signatures:

| Test | What it covers |
|------|---------------|
| `TestNewHTTPCall` | Operation format, params populated, method uppercased |
| `TestNewHTTPCallMethodCase` | Lowercase method input normalized to uppercase |
| `TestNewMCPCall` | Operation set to tool name, params passed through |
| `TestNewMCPCallNilParams` | Nil params handled without panic |
| `TestSafeEvaluate` | Normal evaluation returns correct decision |
| `TestSafeEvaluateUnknownScope` | Unknown scope error propagated |
| `TestSafeEvaluatePanicRecovery` | Nil engine triggers panic recovery, returns Deny + error |

## Moat Migration Path

After Keep releases with these functions:

1. Moat bumps `go.mod` to new Keep version
2. Replace `internalkeep.SafeEvaluate(eng, call, scope)` with `keep.SafeEvaluate(eng, call, scope)` — direct swap
3. Replace `internalkeep.NormalizeHTTPCall(method, host, path)` with:
   ```go
   call := keep.NewHTTPCall(method, host, path)
   call.Context.Scope = "http-" + host
   ```
4. Replace `internalkeep.NormalizeMCPCall(tool, params, scope)` with:
   ```go
   call := keep.NewMCPCall(tool, params)
   call.Context.Scope = scope
   ```
5. Delete `internal/keep/` or re-export for a transition period
