# LLM Library API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose Keep's LLM gateway functionality as a compilable library so consumers like Moat can evaluate Anthropic requests/responses and handle streaming without running a separate HTTP proxy process.

**Architecture:** Extract provider-agnostic evaluation pipeline into `keep/llm` with a `Codec` interface. Move Anthropic-specific decompose/reassemble/SSE logic behind `keep/llm/anthropic` codec. Promote `internal/sse` to `keep/sse` for consumer-accessible SSE parsing/writing. Refactor the existing gateway binary to use the new public API, eliminating duplicated logic.

**Tech Stack:** Go, existing Keep engine (`keep.Engine`), existing Anthropic types and logic (relocated, not rewritten)

---

## File Structure

### New public packages

| File | Responsibility |
|------|---------------|
| `sse/event.go` | Public `Event` type (promoted from `internal/sse/sse.go`) |
| `sse/reader.go` | Public `Reader` (promoted from `internal/sse/reader.go`) |
| `sse/writer.go` | Public `Writer` (promoted from `internal/sse/writer.go`) |
| `llm/codec.go` | `Codec` interface, `DecomposeConfig`, `Result`, `StreamResult` types |
| `llm/pipeline.go` | `EvaluateRequest`, `EvaluateResponse`, `EvaluateStream` functions |
| `llm/anthropic/codec.go` | `Codec` implementation wrapping existing decompose/reassemble/stream logic |
| `llm/anthropic/types.go` | Public Anthropic types (`MessagesRequest`, `MessagesResponse`, etc.) promoted from `internal/gateway/anthropic/types.go` |
| `llm/anthropic/decompose.go` | Decompose logic (moved from `internal/gateway/anthropic/decompose.go`) |
| `llm/anthropic/reassemble.go` | Reassemble logic (moved from `internal/gateway/anthropic/reassemble.go`) |
| `llm/anthropic/stream.go` | SSE reassembly + synthesis (moved from `internal/gateway/anthropic/stream.go`) |

### Modified files

| File | Change |
|------|--------|
| `internal/gateway/proxy.go` | Refactor to use `llm.EvaluateRequest`, `llm.EvaluateResponse`, `llm.EvaluateStream` |
| `internal/gateway/proxy_test.go` | Update imports, verify existing tests still pass |
| `internal/gateway/config/config.go` | `DecomposeConfig` stays here for YAML parsing; add converter to `llm.DecomposeConfig` |
| `cmd/keep-llm-gateway/main.go` | Minimal import changes |

### Deleted files (replaced by public packages)

| File | Replaced by |
|------|------------|
| `internal/sse/sse.go` | `sse/event.go` |
| `internal/sse/reader.go` | `sse/reader.go` |
| `internal/sse/writer.go` | `sse/writer.go` |
| `internal/gateway/anthropic/types.go` | `llm/anthropic/types.go` |
| `internal/gateway/anthropic/decompose.go` | `llm/anthropic/decompose.go` |
| `internal/gateway/anthropic/reassemble.go` | `llm/anthropic/reassemble.go` |
| `internal/gateway/anthropic/stream.go` | `llm/anthropic/stream.go` |

### Test files (moved with their source)

| From | To |
|------|-----|
| `internal/sse/reader_test.go` | `sse/reader_test.go` |
| `internal/sse/writer_test.go` | `sse/writer_test.go` |
| `internal/gateway/anthropic/decompose_test.go` | `llm/anthropic/decompose_test.go` |
| `internal/gateway/anthropic/reassemble_test.go` | `llm/anthropic/reassemble_test.go` |
| `internal/gateway/anthropic/stream_test.go` | `llm/anthropic/stream_test.go` |
| `internal/gateway/anthropic/types_test.go` | `llm/anthropic/types_test.go` |

---

## Task 1: Promote `internal/sse` to `sse/`

The SSE package has no internal Keep dependencies. It's a clean standalone utility. Promoting it first unblocks everything else since both the Anthropic codec and consumers need it.

**Files:**
- Create: `sse/event.go`, `sse/reader.go`, `sse/writer.go`
- Move tests: `sse/reader_test.go`, `sse/writer_test.go`
- Delete: `internal/sse/sse.go`, `internal/sse/reader.go`, `internal/sse/writer.go`, `internal/sse/reader_test.go`, `internal/sse/writer_test.go`
- Modify: all files importing `internal/sse` (update import path)

- [ ] **Step 1: Create `sse/event.go`**

Copy `internal/sse/sse.go` to `sse/event.go`. Change package declaration from `package sse` to `package sse` (same name, new location).

```go
// Package sse implements Server-Sent Events parsing and writing
// per the WHATWG spec (https://html.spec.whatwg.org/multipage/server-sent-events.html).
package sse

// Event represents a single Server-Sent Event.
type Event struct {
	Type  string // "event:" field; empty string means unnamed event
	Data  string // "data:" field; multiple data lines joined with "\n"
	ID    string // "id:" field
	Retry int    // "retry:" field in milliseconds; 0 means not set
}
```

- [ ] **Step 2: Create `sse/reader.go`**

Copy `internal/sse/reader.go` to `sse/reader.go`. No content changes needed — just the file location changes.

- [ ] **Step 3: Create `sse/writer.go`**

Copy `internal/sse/writer.go` to `sse/writer.go`. No content changes needed.

- [ ] **Step 4: Copy test files**

Copy `internal/sse/reader_test.go` to `sse/reader_test.go` and `internal/sse/writer_test.go` to `sse/writer_test.go`. No content changes needed.

- [ ] **Step 5: Run tests in new location**

Run: `cd /workspace && go test ./sse/... -v`
Expected: All tests pass.

- [ ] **Step 6: Update all internal imports**

Find all files importing `"github.com/majorcontext/keep/internal/sse"` and change to `"github.com/majorcontext/keep/sse"`. The files to update:

- `internal/gateway/proxy.go` (line 21)
- `internal/gateway/proxy_test.go` (imports `internal/sse`)
- `internal/gateway/anthropic/stream.go` (line 9)
- `internal/gateway/anthropic/stream_test.go` (imports `internal/sse`)
- `internal/gateway/integration_test.go` (if it imports `internal/sse`)

Use grep to find all: `grep -r "internal/sse" --include="*.go" /workspace`

- [ ] **Step 7: Delete the old `internal/sse/` directory**

```bash
rm -rf /workspace/internal/sse/
```

- [ ] **Step 8: Run full test suite**

Run: `cd /workspace && make test-unit`
Expected: All tests pass. No import errors.

- [ ] **Step 9: Commit**

```bash
git add sse/ internal/sse/ internal/gateway/
git commit -m "refactor(sse): promote internal/sse to public sse/ package

Consumers embedding Keep's LLM gateway need SSE parsing and writing
utilities. Moving to a public package enables direct import without
duplicating the implementation."
```

---

## Task 2: Create `llm/codec.go` — interface and types

Define the provider-agnostic types that the pipeline and codecs share. No implementation yet.

**Files:**
- Create: `llm/codec.go`

- [ ] **Step 1: Create `llm/codec.go`**

```go
// Package llm provides a provider-agnostic pipeline for evaluating
// LLM API requests and responses against Keep policy rules.
//
// The pipeline decomposes provider-specific message formats into flat
// keep.Call objects, evaluates each against the policy engine, and
// reassembles mutations back into the provider format.
//
// Provider support is implemented via the Codec interface. See the
// anthropic sub-package for the Anthropic Messages API codec.
package llm

import (
	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// DecomposeConfig controls which message components are decomposed into
// separate policy calls. Zero value enables the default set (tool_result,
// tool_use, request/response summaries enabled; text disabled).
type DecomposeConfig struct {
	// ToolResult decomposes tool_result blocks (default: true).
	ToolResult *bool
	// ToolUse decomposes tool_use blocks (default: true).
	ToolUse *bool
	// Text decomposes text content blocks (default: false).
	Text *bool
	// RequestSummary emits an llm.request summary call (default: true).
	RequestSummary *bool
	// ResponseSummary emits an llm.response summary call (default: true).
	ResponseSummary *bool
}

// ToolResultEnabled returns whether tool_result decomposition is enabled.
func (d DecomposeConfig) ToolResultEnabled() bool { return d.ToolResult == nil || *d.ToolResult }

// ToolUseEnabled returns whether tool_use decomposition is enabled.
func (d DecomposeConfig) ToolUseEnabled() bool { return d.ToolUse == nil || *d.ToolUse }

// TextEnabled returns whether text decomposition is enabled.
func (d DecomposeConfig) TextEnabled() bool { return d.Text != nil && *d.Text }

// RequestSummaryEnabled returns whether request summary is enabled.
func (d DecomposeConfig) RequestSummaryEnabled() bool {
	return d.RequestSummary == nil || *d.RequestSummary
}

// ResponseSummaryEnabled returns whether response summary is enabled.
func (d DecomposeConfig) ResponseSummaryEnabled() bool {
	return d.ResponseSummary == nil || *d.ResponseSummary
}

// Result is the outcome of evaluating a request or response against policy.
type Result struct {
	// Decision is the aggregate policy decision: Allow, Deny, or Redact.
	Decision keep.Decision
	// Rule is the name of the rule that triggered a Deny or the first Redact.
	// Empty for Allow decisions.
	Rule string
	// Message is the deny message from the triggering rule.
	Message string
	// Body is the (possibly redacted) request or response JSON.
	// For Allow decisions, this is the original body unchanged.
	// For Redact decisions, mutations have been applied.
	// For Deny decisions, this is nil.
	Body []byte
	// Audits contains one AuditEntry per decomposed call that was evaluated.
	Audits []keep.AuditEntry
}

// StreamResult is the outcome of evaluating a streaming response.
type StreamResult struct {
	// Decision is the aggregate policy decision.
	Decision keep.Decision
	// Rule is the name of the triggering rule.
	Rule string
	// Message is the deny message.
	Message string
	// Events contains the SSE events to send to the client.
	// For Allow decisions, these are the original events.
	// For Redact decisions, these are synthesized from the patched response.
	// For Deny decisions, this is nil.
	Events []sse.Event
	// Body is the reassembled (and possibly redacted) response JSON.
	// Useful for logging/debugging the complete response after policy.
	// For Deny decisions, this is nil.
	Body []byte
	// Audits contains one AuditEntry per decomposed call that was evaluated.
	Audits []keep.AuditEntry
}

// Codec decomposes provider-specific LLM messages into keep.Call objects
// and reassembles policy mutations back into the provider format.
//
// Each method pair (Decompose/Reassemble) shares an opaque handle that
// carries parsed state and position mappings. The pipeline passes the
// handle from Decompose to Reassemble without inspecting it.
//
// Implementations must be safe for concurrent use.
type Codec interface {
	// DecomposeRequest breaks a request body into policy calls.
	// Returns the calls and an opaque handle for ReassembleRequest.
	DecomposeRequest(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)

	// DecomposeResponse breaks a response body into policy calls.
	// Returns the calls and an opaque handle for ReassembleResponse.
	DecomposeResponse(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)

	// ReassembleRequest patches mutations into the request using the
	// handle from DecomposeRequest. Returns original body if no mutations.
	ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error)

	// ReassembleResponse patches mutations into the response.
	ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error)

	// ReassembleStream reassembles provider-specific SSE events into a
	// complete response body suitable for DecomposeResponse.
	ReassembleStream(events []sse.Event) (body []byte, err error)

	// SynthesizeEvents creates replacement SSE events from a patched
	// response body (the output of ReassembleResponse after redaction).
	SynthesizeEvents(patchedBody []byte) ([]sse.Event, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /workspace && go build ./llm/...`
Expected: Clean compile (no implementation yet, just types and interface).

- [ ] **Step 3: Commit**

```bash
git add llm/
git commit -m "feat(llm): add Codec interface and pipeline types

Defines the provider-agnostic types for the LLM evaluation pipeline:
DecomposeConfig, Result, StreamResult, and the Codec interface that
provider packages implement."
```

---

## Task 3: Create `llm/pipeline.go` — provider-agnostic evaluation loop

This is the core logic currently duplicated in `proxy.go` lines 218-301 (request) and 355-431 (response). Extract into three functions.

**Files:**
- Create: `llm/pipeline.go`
- Create: `llm/pipeline_test.go`

- [ ] **Step 1: Write pipeline tests using a mock codec**

Create `llm/pipeline_test.go`. Use a simple mock codec that returns predetermined calls and handles. Test the three scenarios: allow, deny, and redact.

```go
package llm

import (
	"encoding/json"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// mockCodec is a test double that returns predetermined calls.
type mockCodec struct {
	calls   []keep.Call
	body    []byte
	events  []sse.Event
	synthEvents []sse.Event
}

func (m *mockCodec) DecomposeRequest(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error) {
	return m.calls, body, nil
}

func (m *mockCodec) DecomposeResponse(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error) {
	return m.calls, body, nil
}

func (m *mockCodec) ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error) {
	return handle.([]byte), nil
}

func (m *mockCodec) ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error) {
	return handle.([]byte), nil
}

func (m *mockCodec) ReassembleStream(events []sse.Event) ([]byte, error) {
	return m.body, nil
}

func (m *mockCodec) SynthesizeEvents(patchedBody []byte) ([]sse.Event, error) {
	return m.synthEvents, nil
}

func TestEvaluateRequest_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: allow-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.request", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateRequest(engine, codec, []byte(`{}`), "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
	if len(result.Audits) != 1 {
		t.Errorf("got %d audits, want 1", len(result.Audits))
	}
}

func TestEvaluateRequest_Deny(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: deny-all
    match: {operation: "*"}
    action: deny
    message: blocked
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.request", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateRequest(engine, codec, []byte(`{}`), "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("got decision %q, want deny", result.Decision)
	}
	if result.Rule != "deny-all" {
		t.Errorf("got rule %q, want deny-all", result.Rule)
	}
	if result.Body != nil {
		t.Error("deny result should have nil body")
	}
}

func TestEvaluateResponse_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	body := []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[]}`)
	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.response", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateResponse(engine, codec, body, "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
}

func TestEvaluateStream_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	events := []sse.Event{
		{Type: "message_start", Data: "{}"},
		{Type: "message_stop", Data: "{}"},
	}
	respBody, _ := json.Marshal(map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "content": []any{}})
	codec := &mockCodec{
		calls:  []keep.Call{{Operation: "llm.response", Context: keep.CallContext{Scope: "test"}}},
		body:   respBody,
		events: events,
	}

	result, err := EvaluateStream(engine, codec, events, "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
	if len(result.Events) != 2 {
		t.Errorf("got %d events, want 2 (original)", len(result.Events))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./llm/... -run TestEvaluate -v`
Expected: FAIL — `EvaluateRequest`, `EvaluateResponse`, `EvaluateStream` are undefined.

- [ ] **Step 3: Implement `llm/pipeline.go`**

```go
package llm

import (
	"fmt"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// EvaluateRequest decomposes a provider-specific request body into policy
// calls, evaluates each against the engine, and reassembles mutations.
//
// On deny: returns Result with Decision=Deny, Body=nil.
// On redact: returns Result with Decision=Redact, Body=patched JSON.
// On allow: returns Result with Decision=Allow, Body=original JSON.
func EvaluateRequest(engine *keep.Engine, codec Codec, body []byte, scope string, cfg DecomposeConfig) (*Result, error) {
	calls, handle, err := codec.DecomposeRequest(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose request: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &Result{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	if outcome.redacted {
		outBody, err = codec.ReassembleRequest(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble request: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &Result{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// EvaluateResponse decomposes a provider-specific response body into policy
// calls, evaluates each, and reassembles mutations.
func EvaluateResponse(engine *keep.Engine, codec Codec, body []byte, scope string, cfg DecomposeConfig) (*Result, error) {
	calls, handle, err := codec.DecomposeResponse(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose response: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &Result{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	if outcome.redacted {
		outBody, err = codec.ReassembleResponse(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble response: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &Result{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// EvaluateStream reassembles SSE events into a complete response, evaluates
// policy, and returns either the original events (clean) or synthesized
// events (redacted).
func EvaluateStream(engine *keep.Engine, codec Codec, events []sse.Event, scope string, cfg DecomposeConfig) (*StreamResult, error) {
	// Reassemble SSE events into a complete response.
	body, err := codec.ReassembleStream(events)
	if err != nil {
		return nil, fmt.Errorf("llm: reassemble stream: %w", err)
	}

	// Decompose the reassembled response.
	calls, handle, err := codec.DecomposeResponse(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose stream response: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &StreamResult{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	outEvents := events
	if outcome.redacted {
		// Reassemble the patched response body, then synthesize events from it.
		outBody, err = codec.ReassembleResponse(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble stream response: %w", err)
		}
		outEvents, err = codec.SynthesizeEvents(outBody)
		if err != nil {
			return nil, fmt.Errorf("llm: synthesize events: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &StreamResult{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Events:   outEvents,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// evalOutcome tracks the aggregate result across multiple call evaluations.
type evalOutcome struct {
	denied   bool
	redacted bool
	rule     string
	message  string
}

// evaluateCalls runs each call through the engine and collects results.
// Short-circuits on the first deny.
func evaluateCalls(engine *keep.Engine, calls []keep.Call, scope string) ([]keep.EvalResult, evalOutcome, error) {
	results := make([]keep.EvalResult, 0, len(calls))
	var outcome evalOutcome

	for _, call := range calls {
		result, err := engine.Evaluate(call, scope)
		if err != nil {
			return results, outcome, fmt.Errorf("llm: evaluate: %w", err)
		}
		results = append(results, result)

		switch result.Decision {
		case keep.Deny:
			outcome.denied = true
			outcome.rule = result.Rule
			outcome.message = result.Message
			return results, outcome, nil
		case keep.Redact:
			if !outcome.redacted {
				outcome.redacted = true
				outcome.rule = result.Rule
			}
		}
	}

	return results, outcome, nil
}

// collectAudits extracts audit entries from evaluation results.
func collectAudits(results []keep.EvalResult) []keep.AuditEntry {
	audits := make([]keep.AuditEntry, len(results))
	for i, r := range results {
		audits[i] = r.Audit
	}
	return audits
}
```

- [ ] **Step 4: Run tests**

Run: `cd /workspace && go test ./llm/... -v -race`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add llm/
git commit -m "feat(llm): implement provider-agnostic evaluation pipeline

EvaluateRequest, EvaluateResponse, and EvaluateStream provide the
decompose → evaluate → reassemble loop without HTTP transport. The
pipeline delegates to a Codec interface for provider-specific logic."
```

---

## Task 4: Move Anthropic types to `llm/anthropic/`

Move the Anthropic-specific types and logic from `internal/gateway/anthropic/` to the public `llm/anthropic/` package. This is a mechanical move — no logic changes.

**Files:**
- Create: `llm/anthropic/types.go`, `llm/anthropic/decompose.go`, `llm/anthropic/reassemble.go`, `llm/anthropic/stream.go`
- Move tests: `llm/anthropic/types_test.go`, `llm/anthropic/decompose_test.go`, `llm/anthropic/reassemble_test.go`, `llm/anthropic/stream_test.go`
- Delete: `internal/gateway/anthropic/` (entire directory)
- Modify: `internal/gateway/proxy.go`, `internal/gateway/proxy_test.go` (update imports)

- [ ] **Step 1: Copy source files**

Copy each file from `internal/gateway/anthropic/` to `llm/anthropic/`, changing:
- Package declaration: `package anthropic` (stays the same)
- Import of `"github.com/majorcontext/keep/internal/sse"` → `"github.com/majorcontext/keep/sse"`
- Import of `"github.com/majorcontext/keep/internal/gateway/config"` — the decompose functions use `config.DecomposeConfig`. These must now accept `llm.DecomposeConfig` instead. See step 2.

Files to copy:
- `types.go` + `types_test.go` — no import changes needed (no sse or config imports)
- `decompose.go` + `decompose_test.go` — change `config.DecomposeConfig` → accept `llm.DecomposeConfig` parameter (the methods are identical: `ToolResultEnabled()`, `ToolUseEnabled()`, etc.)
- `reassemble.go` + `reassemble_test.go` — no config import, uses `keep.EvalResult`
- `stream.go` + `stream_test.go` — change `internal/sse` → `sse`

- [ ] **Step 2: Update `decompose.go` imports**

In the moved `llm/anthropic/decompose.go`, replace:

```go
import (
	"fmt"
	"strings"

	keep "github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/gateway/config"
)
```

With:

```go
import (
	"fmt"
	"strings"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
)
```

And change all `config.DecomposeConfig` → `llm.DecomposeConfig` in function signatures. The method names are identical (`ToolResultEnabled()`, etc.) so the function bodies don't change.

- [ ] **Step 3: Update `stream.go` imports**

In the moved `llm/anthropic/stream.go`, replace:

```go
import (
	...
	"github.com/majorcontext/keep/internal/sse"
)
```

With:

```go
import (
	...
	"github.com/majorcontext/keep/sse"
)
```

- [ ] **Step 4: Update `decompose_test.go` imports**

Replace `"github.com/majorcontext/keep/internal/gateway/config"` with `"github.com/majorcontext/keep/llm"` and update `config.DecomposeConfig` → `llm.DecomposeConfig` in the test file.

- [ ] **Step 5: Update `stream_test.go` imports**

Replace `"github.com/majorcontext/keep/internal/sse"` with `"github.com/majorcontext/keep/sse"`.

- [ ] **Step 6: Run tests in new location**

Run: `cd /workspace && go test ./llm/anthropic/... -v -race`
Expected: All tests pass.

- [ ] **Step 7: Update `internal/gateway/proxy.go` imports**

Replace:

```go
"github.com/majorcontext/keep/internal/gateway/anthropic"
```

With:

```go
"github.com/majorcontext/keep/llm/anthropic"
```

Also in `proxy_test.go`, `integration_test.go`, and `verbose_test.go` if they import the anthropic package.

- [ ] **Step 8: Delete `internal/gateway/anthropic/`**

```bash
rm -rf /workspace/internal/gateway/anthropic/
```

- [ ] **Step 9: Run full test suite**

Run: `cd /workspace && make test-unit`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add llm/anthropic/ internal/gateway/
git commit -m "refactor(llm): move Anthropic types to public llm/anthropic package

Promotes decompose, reassemble, stream, and types from
internal/gateway/anthropic to llm/anthropic so consumers can use
Anthropic LLM evaluation without importing internal packages."
```

---

## Task 5: Implement `llm/anthropic/codec.go`

Implement the `llm.Codec` interface for Anthropic, wrapping the existing decompose/reassemble/stream functions.

**Files:**
- Create: `llm/anthropic/codec.go`
- Create: `llm/anthropic/codec_test.go`

- [ ] **Step 1: Write codec tests**

Create `llm/anthropic/codec_test.go` with integration-style tests that exercise the full codec contract:

```go
package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
	"github.com/majorcontext/keep/sse"
)

func TestCodec_DecomposeRequest(t *testing.T) {
	codec := NewCodec()
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`)

	calls, handle, err := codec.DecomposeRequest(body, "test", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if handle == nil {
		t.Fatal("handle should not be nil")
	}
	// Default config: request summary + no text blocks = 1 call
	if len(calls) != 1 {
		t.Errorf("got %d calls, want 1 (request summary only, text disabled by default)", len(calls))
	}
	if calls[0].Operation != "llm.request" {
		t.Errorf("got operation %q, want llm.request", calls[0].Operation)
	}
}

func TestCodec_DecomposeResponse(t *testing.T) {
	codec := NewCodec()
	body := []byte(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-20250514",
		"content": [
			{"type": "tool_use", "id": "tu_1", "name": "read_file", "input": {"path": "/etc/passwd"}}
		],
		"stop_reason": "tool_use"
	}`)

	calls, handle, err := codec.DecomposeResponse(body, "test", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if handle == nil {
		t.Fatal("handle should not be nil")
	}
	// Default: response summary + 1 tool_use = 2 calls
	if len(calls) != 2 {
		t.Errorf("got %d calls, want 2", len(calls))
	}
	if calls[1].Operation != "llm.tool_use" {
		t.Errorf("got operation %q, want llm.tool_use", calls[1].Operation)
	}
}

func TestCodec_ReassembleRequest_NoMutations(t *testing.T) {
	codec := NewCodec()
	body := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`)

	_, handle, err := codec.DecomposeRequest(body, "test", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// No mutations — should return original body.
	results := []keep.EvalResult{{Decision: keep.Allow}}
	out, err := codec.ReassembleRequest(handle, results)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(body) {
		t.Errorf("expected original body when no mutations")
	}
}

func TestCodec_ReassembleStream(t *testing.T) {
	codec := NewCodec()

	startData, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "claude-sonnet-4-20250514", "content": []any{},
		},
	})
	blockStartData, _ := json.Marshal(map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	deltaData, _ := json.Marshal(map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": "Hello!"},
	})
	stopData, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": 0})
	msgDelta, _ := json.Marshal(map[string]any{
		"type": "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
	})
	msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})

	events := []sse.Event{
		{Type: "message_start", Data: string(startData)},
		{Type: "content_block_start", Data: string(blockStartData)},
		{Type: "content_block_delta", Data: string(deltaData)},
		{Type: "content_block_stop", Data: string(stopData)},
		{Type: "message_delta", Data: string(msgDelta)},
		{Type: "message_stop", Data: string(msgStop)},
	}

	body, err := codec.ReassembleStream(events)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the reassembled body contains the text.
	var resp MessagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello!" {
		t.Errorf("unexpected reassembled content: %+v", resp.Content)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./llm/anthropic/... -run TestCodec -v`
Expected: FAIL — `NewCodec` is undefined.

- [ ] **Step 3: Implement `llm/anthropic/codec.go`**

The codec wraps the existing functions, managing the handle plumbing:

```go
package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
	"github.com/majorcontext/keep/sse"
)

// Codec implements llm.Codec for the Anthropic Messages API.
type Codec struct{}

// NewCodec creates an Anthropic codec.
func NewCodec() *Codec {
	return &Codec{}
}

// requestHandle carries parsed state between decompose and reassemble.
type requestHandle struct {
	req      *MessagesRequest
	body     []byte
	blockMap []BlockPosition
	cfg      llm.DecomposeConfig
}

// responseHandle carries parsed state for response reassembly.
type responseHandle struct {
	resp     *MessagesResponse
	body     []byte
	blockMap []BlockPosition
	cfg      llm.DecomposeConfig
}

func (c *Codec) DecomposeRequest(body []byte, scope string, cfg llm.DecomposeConfig) ([]keep.Call, any, error) {
	var req MessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("anthropic: parse request: %w", err)
	}

	calls := DecomposeRequest(&req, scope, cfg)
	blockMap := WalkRequestBlocks(&req, cfg)

	return calls, &requestHandle{
		req:      &req,
		body:     body,
		blockMap: blockMap,
		cfg:      cfg,
	}, nil
}

func (c *Codec) DecomposeResponse(body []byte, scope string, cfg llm.DecomposeConfig) ([]keep.Call, any, error) {
	var resp MessagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("anthropic: parse response: %w", err)
	}

	calls := DecomposeResponse(&resp, scope, cfg)
	blockMap := WalkResponseBlocks(&resp, cfg)

	return calls, &responseHandle{
		resp:     &resp,
		body:     body,
		blockMap: blockMap,
		cfg:      cfg,
	}, nil
}

func (c *Codec) ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error) {
	h, ok := handle.(*requestHandle)
	if !ok {
		return nil, fmt.Errorf("anthropic: invalid request handle type %T", handle)
	}

	blockResults, hasRedaction := buildBlockResults(h.blockMap, results, h.cfg.RequestSummaryEnabled())
	if !hasRedaction {
		return h.body, nil
	}

	patched := ReassembleRequest(h.req, blockResults)
	return json.Marshal(patched)
}

func (c *Codec) ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error) {
	h, ok := handle.(*responseHandle)
	if !ok {
		return nil, fmt.Errorf("anthropic: invalid response handle type %T", handle)
	}

	blockResults, hasRedaction := buildBlockResults(h.blockMap, results, h.cfg.ResponseSummaryEnabled())
	if !hasRedaction {
		return h.body, nil
	}

	patched := ReassembleResponse(h.resp, blockResults)
	return json.Marshal(patched)
}

func (c *Codec) ReassembleStream(events []sse.Event) ([]byte, error) {
	resp, err := ReassembleFromEvents(events)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reassemble stream: %w", err)
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal reassembled response: %w", err)
	}

	return body, nil
}

func (c *Codec) SynthesizeEvents(patchedBody []byte) ([]sse.Event, error) {
	var resp MessagesResponse
	if err := json.Unmarshal(patchedBody, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: parse patched response: %w", err)
	}
	return SynthesizeEvents(&resp), nil
}

// buildBlockResults maps evaluation results back to block positions.
// summaryOffset accounts for the optional summary call at index 0.
func buildBlockResults(blockMap []BlockPosition, results []keep.EvalResult, hasSummary bool) ([]BlockResult, bool) {
	offset := 0
	if hasSummary && len(results) > 0 {
		offset = 1
	}

	hasRedaction := false
	blockResults := make([]BlockResult, 0, len(blockMap))

	for i, pos := range blockMap {
		resultIdx := i + offset
		if resultIdx >= len(results) {
			break
		}
		result := results[resultIdx]
		if result.Decision == keep.Redact {
			hasRedaction = true
		}
		blockResults = append(blockResults, BlockResult{
			MessageIndex: pos.MessageIndex,
			BlockIndex:   pos.BlockIndex,
			Result:       result,
		})
	}

	return blockResults, hasRedaction
}
```

- [ ] **Step 4: Run codec tests**

Run: `cd /workspace && go test ./llm/anthropic/... -run TestCodec -v -race`
Expected: All pass.

- [ ] **Step 5: Run full test suite**

Run: `cd /workspace && make test-unit`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add llm/anthropic/codec.go llm/anthropic/codec_test.go
git commit -m "feat(llm): implement Anthropic codec for llm.Codec interface

Wraps existing decompose/reassemble/stream functions behind the Codec
interface, managing handle plumbing for the evaluation pipeline."
```

---

## Task 6: Add end-to-end pipeline test with real Anthropic codec

Verify the full pipeline (pipeline + real codec + real engine) works together.

**Files:**
- Create: `llm/pipeline_integration_test.go`

- [ ] **Step 1: Write integration test**

```go
package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
	"github.com/majorcontext/keep/llm/anthropic"
	"github.com/majorcontext/keep/sse"
)

func TestPipeline_Anthropic_DenyToolUse(t *testing.T) {
	rules := []byte(`
scope: gateway
mode: enforce
rules:
  - name: no-dangerous-tools
    match:
      operation: "llm.tool_use"
      when: "params.name == 'rm_rf'"
    action: deny
    message: "dangerous tool blocked"
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := anthropic.NewCodec()
	respBody, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant",
		"model": "claude-sonnet-4-20250514",
		"content": []any{
			map[string]any{"type": "tool_use", "id": "tu_1", "name": "rm_rf", "input": map[string]any{"path": "/"}},
		},
		"stop_reason": "tool_use",
	})

	result, err := llm.EvaluateResponse(engine, codec, respBody, "gateway", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("got %q, want deny", result.Decision)
	}
	if result.Rule != "no-dangerous-tools" {
		t.Errorf("got rule %q, want no-dangerous-tools", result.Rule)
	}
}

func TestPipeline_Anthropic_AllowRequest(t *testing.T) {
	rules := []byte(`
scope: gateway
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := anthropic.NewCodec()
	reqBody := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "hello"}]
	}`)

	result, err := llm.EvaluateRequest(engine, codec, reqBody, "gateway", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got %q, want allow", result.Decision)
	}
	if result.Body == nil {
		t.Error("body should not be nil for allow")
	}
}

func TestPipeline_Anthropic_StreamAllow(t *testing.T) {
	rules := []byte(`
scope: gateway
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := anthropic.NewCodec()

	startData, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "claude-sonnet-4-20250514", "content": []any{},
		},
	})
	blockStart, _ := json.Marshal(map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	delta, _ := json.Marshal(map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": "Hi!"},
	})
	blockStop, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": 0})
	msgDelta, _ := json.Marshal(map[string]any{
		"type": "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
	})
	msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})

	events := []sse.Event{
		{Type: "message_start", Data: string(startData)},
		{Type: "content_block_start", Data: string(blockStart)},
		{Type: "content_block_delta", Data: string(delta)},
		{Type: "content_block_stop", Data: string(blockStop)},
		{Type: "message_delta", Data: string(msgDelta)},
		{Type: "message_stop", Data: string(msgStop)},
	}

	result, err := llm.EvaluateStream(engine, codec, events, "gateway", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got %q, want allow", result.Decision)
	}
	if len(result.Events) != len(events) {
		t.Errorf("got %d events, want %d (originals replayed)", len(result.Events), len(events))
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd /workspace && go test ./llm/... -v -race`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add llm/
git commit -m "test(llm): add end-to-end pipeline tests with Anthropic codec"
```

---

## Task 7: Refactor `proxy.go` to use the pipeline

Replace the inline evaluation logic with calls to `llm.EvaluateRequest`, `llm.EvaluateResponse`, and `llm.EvaluateStream`. The proxy becomes a thin HTTP adapter.

**Files:**
- Modify: `internal/gateway/proxy.go`
- Modify: `internal/gateway/config/config.go` (add converter method)

- [ ] **Step 1: Add `DecomposeConfig` converter**

In `internal/gateway/config/config.go`, add a method to convert to the public type:

```go
import "github.com/majorcontext/keep/llm"

// ToLLM converts the YAML-parsed decompose config to the public llm type.
func (d DecomposeConfig) ToLLM() llm.DecomposeConfig {
	return llm.DecomposeConfig{
		ToolResult:      d.ToolResult,
		ToolUse:         d.ToolUse,
		Text:            d.Text,
		RequestSummary:  d.RequestSummary,
		ResponseSummary: d.ResponseSummary,
	}
}
```

- [ ] **Step 2: Refactor `Proxy` struct**

Add a `codec` field and `llmCfg` field to `Proxy`. Initialize in `NewProxy`:

```go
import (
	"github.com/majorcontext/keep/llm"
	llmanthropic "github.com/majorcontext/keep/llm/anthropic"
)

type Proxy struct {
	engine    *keep.Engine
	scope     string
	upstream  *url.URL
	codec     llm.Codec
	llmCfg    llm.DecomposeConfig
	logger    *audit.Logger
	debug     *slog.Logger
	verbose   *VerboseWriter
	passthru  *httputil.ReverseProxy
	client    *http.Client
}
```

In `NewProxy`, set `codec: llmanthropic.NewCodec()` and `llmCfg: cfg.Decompose.ToLLM()`.

- [ ] **Step 3: Replace `evaluateRequestPolicy` body**

Replace the inline decompose/evaluate/reassemble loop (lines 218-301) with a call to `llm.EvaluateRequest`. The verbose logging and audit logging hooks wrap the result:

```go
func (p *Proxy) evaluateRequestPolicy(w http.ResponseWriter, body []byte) (*requestPolicyResult, error) {
	result, err := llm.EvaluateRequest(p.engine, p.codec, body, p.scope, p.llmCfg)
	if err != nil {
		writeInternalError(w, "policy evaluation error")
		return nil, err
	}

	// Log all audit entries.
	if p.logger != nil {
		for _, a := range result.Audits {
			p.logger.Log(a)
		}
	}

	if result.Decision == keep.Deny {
		p.logDebug("request denied", "rule", result.Rule, "message", result.Message)
		if p.verbose != nil {
			p.verbose.RequestDenied(result.Rule, result.Message)
		}
		writePolicyDeny(w, result.Rule, result.Message)
		return nil, fmt.Errorf("policy denied: %s", result.Rule)
	}

	if p.verbose != nil {
		if result.Decision == keep.Redact {
			p.verbose.RequestAfterPolicy(result.Body, result.Rule)
		} else {
			p.verbose.RequestAllowed()
		}
	}

	return &requestPolicyResult{forwardBody: result.Body}, nil
}
```

- [ ] **Step 4: Replace response evaluation in `handleNonStreamingResponse`**

Replace lines 355-431 (response decompose/evaluate/reassemble) with:

```go
result, err := llm.EvaluateResponse(p.engine, p.codec, respBody, p.scope, p.llmCfg)
if err != nil {
	writeInternalError(w, "response policy evaluation error")
	return
}
if p.logger != nil {
	for _, a := range result.Audits {
		p.logger.Log(a)
	}
}
if result.Decision == keep.Deny {
	p.logDebug("response denied", "rule", result.Rule, "message", result.Message)
	if p.verbose != nil {
		p.verbose.ResponseDenied(result.Rule, result.Message)
	}
	writePolicyDeny(w, result.Rule, result.Message)
	return
}
finalBody := result.Body
// verbose logging...
```

- [ ] **Step 5: Replace streaming evaluation in `handleStreamingResponse`**

Replace lines 512-593 (SSE buffer + evaluate + synthesize) with:

```go
streamResult, err := llm.EvaluateStream(p.engine, p.codec, events, p.scope, p.llmCfg)
if err != nil {
	writeInternalError(w, "response policy evaluation error")
	return
}
if p.logger != nil {
	for _, a := range streamResult.Audits {
		p.logger.Log(a)
	}
}
if streamResult.Decision == keep.Deny {
	writePolicyDeny(w, streamResult.Rule, streamResult.Message)
	return
}
// Verbose logging — streamResult.Body has the reassembled (possibly redacted) JSON.
if p.verbose != nil {
	if streamResult.Decision == keep.Redact {
		p.verbose.ResponseAfterPolicy(streamResult.Body, streamResult.Rule)
	} else {
		p.verbose.ResponseAllowed()
	}
}
outEvents := streamResult.Events
```

- [ ] **Step 6: Remove unused imports and helper functions**

After refactoring, these can be removed from `proxy.go`:
- `buildRequestBlockMap` function
- The `blockPosition` type

Note: the `anthropic` import stays in `proxy.go` — `readRequest` parses into `*anthropic.MessagesRequest` to check `req.Stream` (needed to choose between streaming and non-streaming code paths). Update the import from `"github.com/majorcontext/keep/internal/gateway/anthropic"` to `"github.com/majorcontext/keep/llm/anthropic"`.

Keep `readRequest` — it handles HTTP-specific concerns (MaxBytesReader, error responses). The codec receives the body bytes; the proxy still reads them from the HTTP request. Update `handleMessages` to call `evaluateRequestPolicy(w, body)` instead of `evaluateRequestPolicy(w, req, body)` since the `req` parameter is no longer needed there.

- [ ] **Step 7: Run all gateway tests**

Run: `cd /workspace && go test ./internal/gateway/... -v -race`
Expected: All existing tests pass.

- [ ] **Step 8: Run full test suite**

Run: `cd /workspace && make test-unit`
Expected: All tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/gateway/ llm/
git commit -m "refactor(gateway): use llm pipeline for policy evaluation

The HTTP proxy now delegates decompose/evaluate/reassemble to the
llm.EvaluateRequest/Response/Stream functions, eliminating duplicated
evaluation logic. The proxy retains HTTP-specific concerns: body reading,
header forwarding, SSE buffering from upstream, and error responses."
```

---

## Task 8: Clean up — remove `internal/gateway/config.DecomposeConfig` duplication

Now that `llm.DecomposeConfig` is the canonical type, see if we can simplify the gateway config to parse YAML directly into `llm.DecomposeConfig` (or keep the thin converter if YAML tags differ from the public struct).

**Files:**
- Modify: `internal/gateway/config/config.go`

- [ ] **Step 1: Evaluate the converter approach**

The YAML config uses `yaml:"tool_result,omitempty"` tags while the public `llm.DecomposeConfig` has no tags. Options:

a) Add `yaml` tags to `llm.DecomposeConfig` — makes the public API slightly YAML-aware (undesirable).
b) Keep the gateway `DecomposeConfig` for YAML parsing with `ToLLM()` converter — clean separation.

**Decision: keep option (b).** The converter is 6 lines and preserves clean separation between config parsing and the public API. No changes needed — this is already what we have from Task 7.

- [ ] **Step 2: Verify no dead code remains**

```bash
grep -rn "internal/gateway/anthropic" --include="*.go" /workspace
grep -rn "internal/sse" --include="*.go" /workspace
```

Expected: Zero matches (all references updated in previous tasks).

- [ ] **Step 3: Run lint**

Run: `cd /workspace && make lint`
Expected: Clean.

- [ ] **Step 4: Run full test suite with race detector**

Run: `cd /workspace && make test-unit`
Expected: All pass.

- [ ] **Step 5: Commit (if any cleanup was needed)**

```bash
git add -A
git commit -m "chore: remove dead internal package references"
```

---

## Summary of public API after all tasks

```go
// Consumer imports:
import (
    "github.com/majorcontext/keep"           // Engine, Call, EvalResult, Decision
    "github.com/majorcontext/keep/sse"        // Event, Reader, Writer
    "github.com/majorcontext/keep/llm"        // Pipeline functions, Codec, DecomposeConfig, Result
    "github.com/majorcontext/keep/llm/anthropic" // Anthropic codec + types
)

// Typical usage:
engine, _ := keep.LoadFromBytes(rules)
codec := anthropic.NewCodec()
cfg := llm.DecomposeConfig{} // zero value = sensible defaults

// Request evaluation
result, _ := llm.EvaluateRequest(engine, codec, reqBody, "scope", cfg)

// Response evaluation
result, _ = llm.EvaluateResponse(engine, codec, respBody, "scope", cfg)

// Streaming evaluation (consumer provides SSE events from their own transport)
reader := sse.NewReader(upstreamBody)
var events []sse.Event
for { ev, err := reader.Next(); /* collect */ }
streamResult, _ := llm.EvaluateStream(engine, codec, events, "scope", cfg)
// streamResult.Events ready to write back

// Adding a new provider (future):
// 1. Create llm/openai/codec.go implementing llm.Codec
// 2. codec := openai.NewCodec()
// 3. Same pipeline functions work unchanged
```
