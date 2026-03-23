---
title: "Using Keep as a Go library"
navTitle: "Go library"
description: "Embed Keep's policy engine directly in your Go application for inline policy checks."
keywords: ["keep", "go", "library", "embed", "api", "sdk"]
---

This guide walks through embedding Keep's policy engine in a Go application. By the end, you will load rules, evaluate calls, handle decisions, and wire it into an HTTP middleware.

## Prerequisites

- Go 1.25 or later
- A directory of Keep rule files (see [Writing rules](./03-writing-rules.md))

## Install

```bash
go get github.com/majorcontext/keep
```

## Load rules

`keep.Load` reads YAML rule files from a directory, compiles CEL expressions and redact patterns, and returns a ready-to-use engine:

```go
engine, err := keep.Load("./rules")
if err != nil {
    log.Fatalf("load rules: %v", err)
}
defer engine.Close()
```

Pass options to configure additional directories or override mode:

```go
engine, err := keep.Load("./rules",
    keep.WithProfilesDir("./profiles"),
    keep.WithPacksDir("./packs"),
    keep.WithForceEnforce(),
)
```

| Option | Effect |
|--------|--------|
| `WithProfilesDir(dir)` | Load profile YAML files that define field aliases |
| `WithPacksDir(dir)` | Load starter pack YAML files with reusable rules |
| `WithForceEnforce()` | Override every scope's mode to `enforce` |

## Evaluate calls

Build a `keep.Call` and pass it to `Evaluate` with a scope name:

```go
result, err := engine.Evaluate(keep.Call{
    Operation: "create_issue",
    Params:    map[string]any{"priority": 1, "title": "Fix login bug"},
    Context:   keep.CallContext{AgentID: "my-agent"},
}, "linear-tools")
if err != nil {
    log.Fatalf("evaluate: %v", err)
}
```

A `Call` has three fields:

- `Operation` -- the action being performed (e.g. `"create_issue"`, `"llm.tool_result"`)
- `Params` -- arbitrary key-value parameters the rules inspect
- `Context` -- metadata like `AgentID`, `UserID`, `Timestamp`, and `Labels`

The second argument to `Evaluate` is the scope name declared in your rule files. If the scope does not exist, `Evaluate` returns an error listing available scopes.

## Handle decisions

`result.Decision` is one of three values:

```go
switch result.Decision {
case keep.Allow:
    // Proceed with the call.

case keep.Deny:
    // Block the call. result.Rule and result.Message explain why.
    log.Printf("denied by rule %q: %s", result.Rule, result.Message)

case keep.Redact:
    // Allow the call but apply mutations first.
    params = keep.ApplyMutations(params, result.Mutations)
}
```

`ApplyMutations` returns a new map with redacted values. The original map is not modified.

Every evaluation populates `result.Audit` with the timestamp, scope, operation, rules evaluated, and decision -- useful for structured logging regardless of outcome.

## Lifecycle

### Close

`Close` stops the rate counter garbage collection goroutine. Call it when the engine is no longer needed to prevent goroutine leaks:

```go
defer engine.Close()
```

### Reload

`Reload` re-reads all rule files from disk and recompiles evaluators. The rate counter store is preserved across reloads, so rate-limiting state is not lost:

```go
if err := engine.Reload(); err != nil {
    log.Printf("reload failed: %v", err)
}
```

This lets you pick up rule changes without restarting the process -- useful with file watchers or a config reload signal handler.

### Listing scopes

`Scopes` returns the sorted list of loaded scope names:

```go
fmt.Println(engine.Scopes()) // [anthropic-gateway linear-tools]
```

## Thread safety

The engine is safe for concurrent use. Multiple goroutines can call `Evaluate` simultaneously. `Reload` acquires a write lock internally, so concurrent evaluations block briefly during a reload and resume with the new rules.

## Complete example

This HTTP middleware evaluates every request against a policy scope before forwarding it:

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"

    "github.com/majorcontext/keep"
)

func main() {
    engine, err := keep.Load("./rules")
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("OK"))
    })

    http.Handle("/", policyMiddleware(engine, "my-scope", handler))
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func policyMiddleware(eng *keep.Engine, scope string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        call := keep.Call{
            Operation: r.Method + " " + r.URL.Path,
            Params:    map[string]any{"method": r.Method, "path": r.URL.Path},
            Context:   keep.CallContext{AgentID: r.Header.Get("X-Agent-ID")},
        }

        result, err := eng.Evaluate(call, scope)
        if err != nil {
            http.Error(w, "policy error", http.StatusInternalServerError)
            return
        }

        switch result.Decision {
        case keep.Deny:
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusForbidden)
            json.NewEncoder(w).Encode(map[string]string{
                "error": result.Message,
                "rule":  result.Rule,
            })
            return
        case keep.Redact:
            // For HTTP middleware, redaction typically applies to response bodies.
            // Handle based on your application's needs.
        }

        next.ServeHTTP(w, r)
    })
}
```

## Related guides

- [Writing rules](./03-writing-rules.md) -- rule file syntax and structure
- [Expressions](../concepts/02-expressions.md) -- CEL expression reference
