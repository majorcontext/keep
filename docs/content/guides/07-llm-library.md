---
title: "Evaluating LLM messages as a library"
navTitle: "LLM evaluation library"
description: "Evaluate Anthropic Messages API requests and responses against Keep policy rules from your own Go code, without running the HTTP gateway."
keywords: ["keep", "llm", "library", "go", "anthropic", "streaming", "sse", "codec"]
---

This guide walks through using Keep's LLM evaluation pipeline as an importable Go library. By the end you will evaluate Anthropic Messages API payloads against policy rules -- including streaming responses -- without running `keep-llm-gateway`.

Use this when you want gateway-level decompose-evaluate-reassemble logic embedded in your own proxy, middleware, or agent harness. If you only need raw `keep.Call` evaluation, see [Using Keep as a Go library](./06-go-library.md). If you want a standalone HTTP proxy, see [Running the LLM gateway](./02-llm-gateway.md).

## Prerequisites

- Go 1.25 or later
- A directory of Keep rule files with an LLM-oriented scope (see [Writing rules](./03-writing-rules.md))

## Install

```bash
go get github.com/majorcontext/keep
```

The `llm`, `llm/anthropic`, and `sse` packages are all part of the Keep module.

## Core concepts

The pipeline has three steps:

1. **Decompose** -- break a provider-specific JSON body into flat `keep.Call` objects (one per content block, plus optional summaries).
2. **Evaluate** -- run each call through the policy engine.
3. **Reassemble** -- patch mutations back into the original JSON structure.

The `llm` package provides three functions that perform all three steps:

| Function | Input | Output | Use case |
|----------|-------|--------|----------|
| `llm.EvaluateRequest` | request JSON (`[]byte`) | `*llm.Result` | Evaluate before forwarding to upstream |
| `llm.EvaluateResponse` | response JSON (`[]byte`) | `*llm.Result` | Evaluate after receiving from upstream |
| `llm.EvaluateStream` | SSE events (`[]sse.Event`) | `*llm.StreamResult` | Evaluate a buffered streaming response |

Each function takes a `Codec` that handles the provider-specific format. For Anthropic:

```go
import (
    "github.com/majorcontext/keep/llm"
    "github.com/majorcontext/keep/llm/anthropic"
)

codec := anthropic.NewCodec()
```

The codec is stateless and safe for concurrent use. Create one at startup and reuse it.

## Non-streaming evaluation

### Request

Read the request body, evaluate it, and check the result:

```go
result, err := llm.EvaluateRequest(engine, codec, body, "my-scope", llm.DecomposeConfig{})
if err != nil {
    // Decomposition or evaluation error.
    return err
}

switch result.Decision {
case keep.Deny:
    // Block the request. result.Rule and result.Message explain why.
    return fmt.Errorf("denied by %s: %s", result.Rule, result.Message)

case keep.Redact:
    // Forward result.Body (redacted JSON) instead of the original.
    body = result.Body

case keep.Allow:
    // Forward body unchanged. result.Body == original body.
}
```

### Response

The response path is identical:

```go
result, err := llm.EvaluateResponse(engine, codec, respBody, "my-scope", llm.DecomposeConfig{})
if err != nil {
    return err
}

switch result.Decision {
case keep.Deny:
    // Return an error to the caller instead of the response.
case keep.Redact:
    respBody = result.Body
case keep.Allow:
    // Pass through.
}
```

### Audit entries

Every evaluation populates `result.Audits` with one entry per decomposed call. Log them regardless of decision:

```go
for _, a := range result.Audits {
    slog.Info("policy",
        "operation", a.Operation,
        "decision", a.Decision,
        "rule", a.Rule,
    )
}
```

## Streaming evaluation

Streaming responses arrive as Server-Sent Events. The pipeline buffers the full stream, reassembles it into a complete response, evaluates policy, and returns either the original events (clean) or synthesized events (redacted).

### 1. Buffer events

Use `sse.NewReader` to parse events from the upstream response body:

```go
import (
    "io"
    "github.com/majorcontext/keep/sse"
)

reader := sse.NewReader(resp.Body)
var events []sse.Event
for {
    ev, err := reader.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    events = append(events, ev)
    // Anthropic streams end with a message_stop event.
    // Stop here rather than waiting for EOF -- the upstream
    // may keep the connection open after the last event.
    if ev.Type == "message_stop" {
        break
    }
}
```

### 2. Evaluate

```go
result, err := llm.EvaluateStream(engine, codec, events, "my-scope", llm.DecomposeConfig{})
if err != nil {
    return err
}

if result.Decision == keep.Deny {
    // Return an error to the caller.
    return fmt.Errorf("denied by %s: %s", result.Rule, result.Message)
}
```

`result.Events` contains the events to send to the client:
- **Allow** -- the original events, unchanged.
- **Redact** -- new events synthesized from the patched response body.

`result.RawBody` is the reassembled response JSON _before_ policy evaluation. Use it for pre-policy logging without re-reassembling the stream yourself.

### 3. Replay events

Use `sse.NewWriter` to stream events to the client over an HTTP response:

```go
writer, err := sse.NewWriter(w) // w is an http.ResponseWriter
if err != nil {
    return err // ResponseWriter does not support streaming
}
writer.SetHeaders()
w.WriteHeader(http.StatusOK)

for _, ev := range result.Events {
    if err := writer.WriteEvent(ev); err != nil {
        return err
    }
}
```

`sse.NewWriter` requires the `http.ResponseWriter` to implement `http.Flusher`. Standard `net/http` response writers do; test code can use `httptest.NewRecorder()`.

## DecomposeConfig

`DecomposeConfig` controls which message components become separate policy calls. The zero value enables sensible defaults:

```go
cfg := llm.DecomposeConfig{} // use defaults
```

| Field | Default | What it controls |
|-------|---------|-----------------|
| `ToolResult` | `true` | Emit `llm.tool_result` calls for tool result blocks |
| `ToolUse` | `true` | Emit `llm.tool_use` calls for tool use blocks |
| `Text` | `false` | Emit `llm.text` calls for text content blocks |
| `RequestSummary` | `true` | Emit the `llm.request` summary call |
| `ResponseSummary` | `true` | Emit the `llm.response` summary call |

Fields are `*bool`. A nil pointer means "use the default." To override:

```go
t := true
cfg := llm.DecomposeConfig{
    Text: &t, // enable text decomposition
}
```

Enable `Text` when rules need to inspect message content -- PII detection, content filtering, or prompt injection checks. Leave it off when rules only target tool interactions, since text blocks are the most numerous content type.

Disabling a block type means no calls are emitted for it and no rules can match. Those blocks pass through unmodified.

See [LLM decomposition](../concepts/05-llm-decomposition.md) for the full decomposition model, call types, and parameters.

## Custom codecs

The `llm.Codec` interface decouples the pipeline from any specific LLM provider. To support a provider beyond Anthropic, implement six methods:

```go
type Codec interface {
    DecomposeRequest(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)
    DecomposeResponse(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)
    ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error)
    ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error)
    ReassembleStream(events []sse.Event) ([]byte, error)
    SynthesizeEvents(patchedBody []byte) ([]sse.Event, error)
}
```

Each Decompose/Reassemble pair shares an opaque `handle` (the `any` value) that carries parsed state and position mappings. The pipeline passes it through without inspecting it.

Implementations must be safe for concurrent use.

## Complete example

A minimal HTTP proxy that evaluates Anthropic Messages API requests and non-streaming responses:

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"

    "github.com/majorcontext/keep"
    "github.com/majorcontext/keep/llm"
    "github.com/majorcontext/keep/llm/anthropic"
)

func main() {
    engine, err := keep.Load("./rules")
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    codec := anthropic.NewCodec()
    cfg := llm.DecomposeConfig{}

    http.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
        // 1. Read and evaluate request.
        body, err := io.ReadAll(r.Body)
        if err != nil {
            http.Error(w, "read error", http.StatusBadRequest)
            return
        }

        reqResult, err := llm.EvaluateRequest(engine, codec, body, "my-scope", cfg)
        if err != nil {
            http.Error(w, "policy error", http.StatusInternalServerError)
            return
        }
        if reqResult.Decision == keep.Deny {
            w.WriteHeader(http.StatusForbidden)
            json.NewEncoder(w).Encode(map[string]string{
                "error": reqResult.Message,
                "rule":  reqResult.Rule,
            })
            return
        }

        // 2. Forward to upstream.
        upstream, err := http.Post(
            "https://api.anthropic.com/v1/messages",
            "application/json",
            bytes.NewReader(reqResult.Body),
        )
        if err != nil {
            http.Error(w, "upstream error", http.StatusBadGateway)
            return
        }
        defer upstream.Body.Close()

        // 3. Read and evaluate response.
        respBody, _ := io.ReadAll(upstream.Body)

        respResult, err := llm.EvaluateResponse(engine, codec, respBody, "my-scope", cfg)
        if err != nil {
            http.Error(w, "policy error", http.StatusInternalServerError)
            return
        }
        if respResult.Decision == keep.Deny {
            w.WriteHeader(http.StatusForbidden)
            json.NewEncoder(w).Encode(map[string]string{
                "error": respResult.Message,
                "rule":  respResult.Rule,
            })
            return
        }

        // 4. Return (possibly redacted) response.
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(upstream.StatusCode)
        w.Write(respResult.Body)
    })

    fmt.Println("listening on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

This example omits streaming, header forwarding, and error detail for brevity. For a production-ready proxy with streaming support, see the [gateway source](https://github.com/majorcontext/keep/blob/main/internal/gateway/proxy.go).

## Related guides

- [Using Keep as a Go library](./06-go-library.md) -- raw engine evaluation with `keep.Call`
- [Running the LLM gateway](./02-llm-gateway.md) -- the standalone HTTP proxy
- [LLM decomposition](../concepts/05-llm-decomposition.md) -- decomposition model, call types, and parameters
- [Writing rules](./03-writing-rules.md) -- rule file syntax for LLM operations
