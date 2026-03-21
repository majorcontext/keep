# Streaming Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add buffer-then-replay SSE streaming support to the LLM gateway, including response policy evaluation with redaction synthesis.

**Architecture:** When `stream: true`, the proxy forwards to Anthropic with streaming, buffers all SSE events via `sse.Reader`, reassembles them into a `MessagesResponse` for policy evaluation, then either replays original events (clean), synthesizes new events (redacted), or returns a JSON error (denied). Two new functions in `internal/gateway/anthropic/stream.go` handle the Anthropic-specific event reassembly and synthesis. The proxy branching logic lives in a new `handleStreamingResponse` method.

**Tech Stack:** Go stdlib, `internal/sse` package (Reader/Writer), existing `internal/gateway/anthropic` types

**Spec:** `docs/plans/2026-03-20-streaming-integration-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/gateway/anthropic/stream.go` | `ReassembleFromEvents` (SSE events → MessagesResponse), `SynthesizeEvents` (MessagesResponse → SSE events) |
| `internal/gateway/anthropic/stream_test.go` | Tests for reassembly and synthesis |
| `internal/gateway/proxy.go` | Add `handleStreamingResponse` method, remove stream rejection, branch at step 7 |
| `internal/gateway/proxy_test.go` | Replace `TestProxy_StreamingRejected`, add streaming allow/deny/redact tests |
| `internal/gateway/testdata/rules/test-gateway.yaml` | Add `redact-response-text-secrets` rule for streaming redact test |

---

### Task 1: ReassembleFromEvents

**Files:**
- Create: `internal/gateway/anthropic/stream.go`
- Create: `internal/gateway/anthropic/stream_test.go`

The Anthropic streaming format sends these event types in order:
- `message_start` — `{"type":"message_start","message":{...}}` — skeleton message with `id`, `type`, `role`, `model`, empty `content`, `usage` with input_tokens
- `content_block_start` — `{"type":"content_block_start","index":N,"content_block":{"type":"text","text":""}}` or `{"type":"content_block_start","index":N,"content_block":{"type":"tool_use","id":"...","name":"...","input":{}}}`
- `content_block_delta` — `{"type":"content_block_delta","index":N,"delta":{"type":"text_delta","text":"chunk"}}` or `{"type":"content_block_delta","index":N,"delta":{"type":"input_json_delta","partial_json":"..."}}`
- `content_block_stop` — `{"type":"content_block_stop","index":N}`
- `message_delta` — `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":N}}`
- `message_stop` — `{"type":"message_stop"}`

Also: `ping` events may appear at any time and should be ignored.

- [ ] **Step 1: Write failing tests for ReassembleFromEvents**

In `stream_test.go`:

```go
package anthropic

import (
	"strings"
	"testing"

	"github.com/majorcontext/keep/internal/sse"
)

func TestReassembleFromEvents_TextOnly(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if resp.ID != "msg_01" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg_01")
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, want %q", resp.Role, "assistant")
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-20250514")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "text")
	}
	if resp.Content[0].Text != "Hello world" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello world")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", resp.Usage.OutputTokens)
	}
}

func TestReassembleFromEvents_ToolUse(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":30,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{}}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\": \"SF\"}"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("Type = %q, want %q", block.Type, "tool_use")
	}
	if block.ID != "toolu_01" {
		t.Errorf("ID = %q, want %q", block.ID, "toolu_01")
	}
	if block.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", block.Name, "get_weather")
	}
	loc, ok := block.Input["location"]
	if !ok || loc != "SF" {
		t.Errorf("Input[location] = %v, want %q", loc, "SF")
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_use")
	}
}

func TestReassembleFromEvents_MultiBlock(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_03","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":20,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_02","name":"bash","input":{}}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\": \"ls\"}"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":1}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Let me check." {
		t.Errorf("Content[0] = %+v, want text 'Let me check.'", resp.Content[0])
	}
	if resp.Content[1].Type != "tool_use" || resp.Content[1].Name != "bash" {
		t.Errorf("Content[1] = %+v, want tool_use 'bash'", resp.Content[1])
	}
	cmd, _ := resp.Content[1].Input["command"]
	if cmd != "ls" {
		t.Errorf("Input[command] = %v, want %q", cmd, "ls")
	}
}

func TestReassembleFromEvents_PingIgnored(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_04","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`},
		{Type: "ping", Data: `{"type":"ping"}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if resp.Content[0].Text != "Hi" {
		t.Errorf("Text = %q, want %q", resp.Content[0].Text, "Hi")
	}
}

func TestReassembleFromEvents_NoMessageStart(t *testing.T) {
	events := []sse.Event{
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	}

	_, err := ReassembleFromEvents(events)
	if err == nil {
		t.Fatal("expected error for missing message_start")
	}
}

func TestReassembleFromEvents_EmptyContent(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_empty","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if len(resp.Content) != 0 {
		t.Errorf("len(Content) = %d, want 0", len(resp.Content))
	}
}

func TestReassembleFromEvents_MissingMessageStop(t *testing.T) {
	// Stream ends without message_stop — should still produce a valid response.
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_trunc","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if resp.Content[0].Text != "partial" {
		t.Errorf("Text = %q, want %q", resp.Content[0].Text, "partial")
	}
}

func TestReassembleFromEvents_MalformedJSON(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{not json}`},
	}

	_, err := ReassembleFromEvents(events)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse message_start") {
		t.Errorf("error = %q, want it to mention 'parse message_start'", err.Error())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./internal/gateway/anthropic/ -run TestReassembleFromEvents -v`
Expected: compilation error — `ReassembleFromEvents` undefined

- [ ] **Step 3: Implement ReassembleFromEvents**

In `stream.go`:

```go
package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/majorcontext/keep/internal/sse"
)

// ReassembleFromEvents builds a MessagesResponse from a sequence of Anthropic
// streaming SSE events. It accumulates text deltas and input_json_delta fragments
// into complete content blocks.
func ReassembleFromEvents(events []sse.Event) (*MessagesResponse, error) {
	var resp *MessagesResponse
	var blocks []ContentBlock
	// inputJSON accumulates partial JSON strings for tool_use blocks, keyed by block index.
	inputJSON := make(map[int]*strings.Builder)
	// textBuf accumulates text delta strings per block index.
	textBuf := make(map[int]*strings.Builder)

	for _, ev := range events {
		switch ev.Type {
		case "message_start":
			var envelope struct {
				Message MessagesResponse `json:"message"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &envelope); err != nil {
				return nil, fmt.Errorf("parse message_start: %w", err)
			}
			resp = &envelope.Message

		case "content_block_start":
			var payload struct {
				Index        int          `json:"index"`
				ContentBlock ContentBlock `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse content_block_start: %w", err)
			}
			// Grow blocks slice to accommodate the index.
			for len(blocks) <= payload.Index {
				blocks = append(blocks, ContentBlock{})
			}
			blocks[payload.Index] = payload.ContentBlock
			if payload.ContentBlock.Type == "tool_use" {
				inputJSON[payload.Index] = &strings.Builder{}
			}
			if payload.ContentBlock.Type == "text" {
				textBuf[payload.Index] = &strings.Builder{}
			}

		case "content_block_delta":
			var payload struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse content_block_delta: %w", err)
			}
			switch payload.Delta.Type {
			case "text_delta":
				if buf, ok := textBuf[payload.Index]; ok {
					buf.WriteString(payload.Delta.Text)
				}
			case "input_json_delta":
				if buf, ok := inputJSON[payload.Index]; ok {
					buf.WriteString(payload.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			// No action needed — block is finalized when we assemble.

		case "message_delta":
			var payload struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage *Usage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse message_delta: %w", err)
			}
			if resp != nil {
				resp.StopReason = payload.Delta.StopReason
				if payload.Usage != nil {
					if resp.Usage == nil {
						resp.Usage = &Usage{}
					}
					resp.Usage.OutputTokens = payload.Usage.OutputTokens
				}
			}

		case "message_stop", "ping":
			// No action needed.
		}
	}

	if resp == nil {
		return nil, errors.New("no message_start event found")
	}

	// Finalize text blocks.
	for idx, buf := range textBuf {
		if idx < len(blocks) {
			blocks[idx].Text = buf.String()
		}
	}

	// Finalize tool_use input JSON.
	for idx, buf := range inputJSON {
		if idx < len(blocks) {
			raw := buf.String()
			if raw != "" {
				var input map[string]any
				if err := json.Unmarshal([]byte(raw), &input); err != nil {
					return nil, fmt.Errorf("parse tool_use input at index %d: %w", idx, err)
				}
				blocks[idx].Input = input
			}
		}
	}

	resp.Content = blocks
	return resp, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace && go test ./internal/gateway/anthropic/ -run TestReassembleFromEvents -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/anthropic/stream.go internal/gateway/anthropic/stream_test.go
git commit -m "feat(gateway): add ReassembleFromEvents for SSE stream reassembly"
```

---

### Task 2: SynthesizeEvents

**Files:**
- Modify: `internal/gateway/anthropic/stream.go`
- Modify: `internal/gateway/anthropic/stream_test.go`

- [ ] **Step 1: Write failing tests for SynthesizeEvents**

Append to `stream_test.go`:

```go
func TestSynthesizeEvents_TextOnly(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_01",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello world"},
		},
		Usage: &Usage{InputTokens: 25, OutputTokens: 10},
	}

	events := SynthesizeEvents(resp)

	// Verify event types in order.
	wantTypes := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(wantTypes))
	}
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}

	// Verify message_start has null stop_reason (not empty string).
	if strings.Contains(events[0].Data, `"stop_reason":""`) {
		t.Error("message_start should have null stop_reason, not empty string")
	}
}

func TestSynthesizeEvents_ToolUse(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_02",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "tool_use", ID: "toolu_01", Name: "bash", Input: map[string]any{"command": "ls"}},
		},
		Usage: &Usage{InputTokens: 30, OutputTokens: 15},
	}

	events := SynthesizeEvents(resp)

	if len(events) != 6 {
		t.Fatalf("len(events) = %d, want 6", len(events))
	}
	if events[2].Type != "content_block_delta" {
		t.Fatalf("events[2].Type = %q, want content_block_delta", events[2].Type)
	}
	if !strings.Contains(events[2].Data, `"partial_json"`) {
		t.Errorf("expected input_json_delta in event data, got: %s", events[2].Data)
	}
}

func TestSynthesizeEvents_MultiBlock(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_03",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me check."},
			{Type: "tool_use", ID: "toolu_02", Name: "bash", Input: map[string]any{"command": "ls"}},
		},
		Usage: &Usage{InputTokens: 20, OutputTokens: 20},
	}

	events := SynthesizeEvents(resp)

	// message_start + (start+delta+stop)*2 + message_delta + message_stop = 1 + 6 + 2 = 9
	if len(events) != 9 {
		t.Fatalf("len(events) = %d, want 9", len(events))
	}
}

func TestSynthesizeEvents_RoundTrip(t *testing.T) {
	original := &MessagesResponse{
		ID:         "msg_rt",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello world"},
		},
		Usage: &Usage{InputTokens: 25, OutputTokens: 10},
	}

	events := SynthesizeEvents(original)
	rebuilt, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if rebuilt.ID != original.ID {
		t.Errorf("ID = %q, want %q", rebuilt.ID, original.ID)
	}
	if rebuilt.StopReason != original.StopReason {
		t.Errorf("StopReason = %q, want %q", rebuilt.StopReason, original.StopReason)
	}
	if len(rebuilt.Content) != len(original.Content) {
		t.Fatalf("len(Content) = %d, want %d", len(rebuilt.Content), len(original.Content))
	}
	if rebuilt.Content[0].Text != original.Content[0].Text {
		t.Errorf("Text = %q, want %q", rebuilt.Content[0].Text, original.Content[0].Text)
	}
}

func TestSynthesizeEvents_ToolUseRoundTrip(t *testing.T) {
	original := &MessagesResponse{
		ID:         "msg_rt2",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me look that up."},
			{Type: "tool_use", ID: "toolu_03", Name: "get_weather", Input: map[string]any{"location": "SF"}},
		},
		Usage: &Usage{InputTokens: 30, OutputTokens: 20},
	}

	events := SynthesizeEvents(original)
	rebuilt, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(rebuilt.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(rebuilt.Content))
	}
	if rebuilt.Content[0].Text != "Let me look that up." {
		t.Errorf("Content[0].Text = %q", rebuilt.Content[0].Text)
	}
	if rebuilt.Content[1].Name != "get_weather" {
		t.Errorf("Content[1].Name = %q", rebuilt.Content[1].Name)
	}
	loc, _ := rebuilt.Content[1].Input["location"]
	if loc != "SF" {
		t.Errorf("Input[location] = %v, want %q", loc, "SF")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./internal/gateway/anthropic/ -run TestSynthesizeEvents -v`
Expected: compilation error — `SynthesizeEvents` undefined

- [ ] **Step 3: Implement SynthesizeEvents**

Append to `stream.go`:

```go
// SynthesizeEvents produces a minimal valid Anthropic SSE event sequence
// from a MessagesResponse. Each content block gets one start + one delta + one stop.
// This is used when the response was redacted and the original events can't be replayed.
func SynthesizeEvents(resp *MessagesResponse) []sse.Event {
	var events []sse.Event

	// message_start: use a raw map so stop_reason serializes as null (not "").
	startMsg := map[string]any{
		"id":          resp.ID,
		"type":        resp.Type,
		"role":        resp.Role,
		"model":       resp.Model,
		"content":     []any{},
		"stop_reason": nil, // null in JSON
	}
	if resp.Usage != nil {
		startMsg["usage"] = map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": 0,
		}
	}
	startData, _ := json.Marshal(map[string]any{
		"type":    "message_start",
		"message": startMsg,
	})
	events = append(events, sse.Event{Type: "message_start", Data: string(startData)})

	// Per content block: start + delta + stop.
	for i, block := range resp.Content {
		// content_block_start
		var startBlock any
		switch block.Type {
		case "tool_use":
			startBlock = map[string]any{
				"type":  "tool_use",
				"id":    block.ID,
				"name":  block.Name,
				"input": map[string]any{},
			}
		case "text":
			startBlock = map[string]any{
				"type": "text",
				"text": "",
			}
		default:
			startBlock = map[string]any{"type": block.Type}
		}
		blockStartData, _ := json.Marshal(map[string]any{
			"type":          "content_block_start",
			"index":         i,
			"content_block": startBlock,
		})
		events = append(events, sse.Event{Type: "content_block_start", Data: string(blockStartData)})

		// content_block_delta
		var deltaData []byte
		switch block.Type {
		case "text":
			deltaData, _ = json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": i,
				"delta": map[string]any{
					"type": "text_delta",
					"text": block.Text,
				},
			})
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			deltaData, _ = json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": i,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": string(inputJSON),
				},
			})
		}
		events = append(events, sse.Event{Type: "content_block_delta", Data: string(deltaData)})

		// content_block_stop
		stopData, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": i,
		})
		events = append(events, sse.Event{Type: "content_block_stop", Data: string(stopData)})
	}

	// message_delta
	msgDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": resp.StopReason,
		},
	}
	if resp.Usage != nil {
		msgDelta["usage"] = map[string]any{
			"output_tokens": resp.Usage.OutputTokens,
		}
	}
	msgDeltaData, _ := json.Marshal(msgDelta)
	events = append(events, sse.Event{Type: "message_delta", Data: string(msgDeltaData)})

	// message_stop
	stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
	events = append(events, sse.Event{Type: "message_stop", Data: string(stopData)})

	return events
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace && go test ./internal/gateway/anthropic/ -run TestSynthesizeEvents -v`
Expected: all PASS

- [ ] **Step 5: Run all anthropic package tests**

Run: `cd /workspace && go test ./internal/gateway/anthropic/ -v`
Expected: all PASS (no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/anthropic/stream.go internal/gateway/anthropic/stream_test.go
git commit -m "feat(gateway): add SynthesizeEvents for SSE event generation"
```

---

### Task 3: Proxy Streaming Handler + Tests

**Files:**
- Modify: `internal/gateway/proxy.go`
- Modify: `internal/gateway/proxy_test.go`
- Modify: `internal/gateway/testdata/rules/test-gateway.yaml`

This task adds the streaming handler to the proxy and its integration tests together. First we add the test rule and write the tests (they'll fail to compile), then implement the handler, then verify tests pass.

- [ ] **Step 1: Add response text redaction rule to test rules**

The existing test rules only redact `llm.tool_result`. We need a rule that redacts `llm.text` responses containing secrets. Append to `internal/gateway/testdata/rules/test-gateway.yaml`:

```yaml
  - name: redact-response-text-secrets
    match:
      operation: "llm.text"
      when: "params.text.contains('SECRET_')"
    action: redact
    redact:
      target: "params.text"
      patterns:
        - match: "SECRET_[A-Z_]+"
          replace: "[REDACTED]"
```

- [ ] **Step 2: Write SSE mock upstream helpers in proxy_test.go**

Add to `proxy_test.go`:

```go
// sseUpstream returns a test server that responds with SSE events for a simple text response.
func sseUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "req_stream_123")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from \"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"upstream\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

// destructiveSSEUpstream returns a test server that streams a destructive tool_use via SSE.
func destructiveSSEUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_deny\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"bash\",\"input\":{}}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\": \\\"rm -rf /\\\"}\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":15}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}
```

- [ ] **Step 3: Replace TestProxy_StreamingRejected with streaming tests**

Replace the existing `TestProxy_StreamingRejected` with these three tests:

```go
func TestProxy_StreamingAllowed(t *testing.T) {
	upstream := sseUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	reader := sse.NewReader(resp.Body)
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		events = append(events, ev)
	}

	// sseUpstream sends 7 events. Verify we get all 7 (original replay, not synthesis).
	if len(events) != 7 {
		t.Fatalf("expected 7 SSE events (original replay), got %d", len(events))
	}

	assembled, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if assembled.Content[0].Text != "Hello from upstream" {
		t.Errorf("Text = %q, want %q", assembled.Content[0].Text, "Hello from upstream")
	}
}

func TestProxy_StreamingDenyResponse(t *testing.T) {
	upstream := destructiveSSEUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Do something"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Type != "policy_denied" {
		t.Errorf("expected policy_denied, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "block-destructive") {
		t.Errorf("expected block-destructive in message, got %q", errResp.Error.Message)
	}
}

func TestProxy_StreamingRedactResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_redact\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"The key is SECRET_API_KEY_VALUE\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Show me the key"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	reader := sse.NewReader(resp.Body)
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		events = append(events, ev)
	}

	assembled, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	text := assembled.Content[0].Text
	if strings.Contains(text, "SECRET_API_KEY_VALUE") {
		t.Errorf("secret was not redacted: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in text, got: %s", text)
	}
}
```

Add `"github.com/majorcontext/keep/internal/sse"` to the imports in `proxy_test.go`.

- [ ] **Step 4: Run tests to verify they fail to compile**

Run: `cd /workspace && go test ./internal/gateway/ -run TestProxy_Streaming -v`
Expected: compilation error — `handleStreamingResponse` doesn't exist yet, or the streaming request still returns 400.

- [ ] **Step 5: Remove stream rejection and add branch in proxy.go**

In `proxy.go`, delete the entire `// 2b. Reject streaming requests` block (the `if req.Stream { ... }` section).

After the `// 6. Reassemble if redacted.` section, add:

```go
	// 7. Forward to upstream and handle response.
	if req.Stream {
		p.handleStreamingResponse(w, r, forwardBody)
		return
	}
```

- [ ] **Step 6: Implement handleStreamingResponse in proxy.go**

Add to `proxy.go`. Note: wraps upstream body in `io.LimitReader` for memory safety.

```go
// handleStreamingResponse handles the upstream call and response for streaming requests.
// It buffers the full SSE stream, reassembles into a MessagesResponse, evaluates policy,
// then replays original events (if clean) or synthesizes new events (if redacted).
func (p *Proxy) handleStreamingResponse(w http.ResponseWriter, r *http.Request, forwardBody []byte) {
	// 1. Forward to upstream.
	upstreamBase := strings.TrimRight(p.upstream.String(), "/")
	upstreamURL := upstreamBase + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(forwardBody))
	if err != nil {
		writeInternalError(w, "failed to create upstream request")
		return
	}

	for _, h := range []string{"Authorization", "Content-Type", "anthropic-version", "x-api-key"} {
		if v := r.Header.Get(h); v != "" {
			upstreamReq.Header.Set(h, v)
		}
	}

	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		writeInternalError(w, "upstream request failed")
		return
	}
	defer upstreamResp.Body.Close()

	// 2. If upstream returned non-2xx, pass through as-is.
	if upstreamResp.StatusCode < 200 || upstreamResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(upstreamResp.Body, maxResponseBodySize))
		copyResponseHeaders(w, upstreamResp)
		w.WriteHeader(upstreamResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	// 3. Buffer all SSE events from upstream.
	reader := sse.NewReader(io.LimitReader(upstreamResp.Body, maxResponseBodySize))
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeInternalError(w, "failed to read upstream SSE stream")
			return
		}
		events = append(events, ev)
	}

	// 4. Reassemble into MessagesResponse.
	resp, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		writeInternalError(w, "failed to reassemble streaming response")
		return
	}

	// 5. Decompose and evaluate response policy.
	respCalls := anthropic.DecomposeResponse(resp, p.scope, p.decompose)

	respSummaryOffset := 0
	if p.decompose.ResponseSummaryEnabled() && len(respCalls) > 0 {
		respSummaryOffset = 1
	}

	var respBlockResults []anthropic.BlockResult
	respHasRedaction := false

	for i, call := range respCalls {
		result, evalErr := p.engine.Evaluate(call, p.scope)
		if evalErr != nil {
			writeInternalError(w, "response policy evaluation error")
			return
		}

		if p.logger != nil {
			p.logger.Log(result.Audit)
		}

		if result.Decision == keep.Deny {
			writePolicyDeny(w, result.Rule, result.Message)
			return
		}

		if i >= respSummaryOffset {
			if result.Decision == keep.Redact {
				respHasRedaction = true
			}
			respBlockResults = append(respBlockResults, anthropic.BlockResult{
				BlockIndex: i - respSummaryOffset,
				Result:     result,
			})
		}
	}

	// 6. Determine which events to send.
	var outEvents []sse.Event
	if respHasRedaction {
		patched := anthropic.ReassembleResponse(resp, respBlockResults)
		outEvents = anthropic.SynthesizeEvents(patched)
	} else {
		outEvents = events
	}

	// 7. Stream events to client.
	sseWriter, err := sse.NewWriter(w)
	if err != nil {
		writeInternalError(w, "streaming not supported by response writer")
		return
	}
	// Copy rate-limit headers from upstream, then set SSE headers (overrides Content-Type).
	copyResponseHeaders(w, upstreamResp)
	sseWriter.SetHeaders()
	w.WriteHeader(http.StatusOK)

	for _, ev := range outEvents {
		if err := sseWriter.WriteEvent(ev); err != nil {
			return
		}
	}
}
```

Add `"github.com/majorcontext/keep/internal/sse"` to the imports in `proxy.go`.

- [ ] **Step 7: Run all proxy tests**

Run: `cd /workspace && go test ./internal/gateway/ -run TestProxy -v`
Expected: all PASS

- [ ] **Step 8: Run full test suite**

Run: `cd /workspace && make test-unit`
Expected: all PASS

- [ ] **Step 9: Run linter**

Run: `cd /workspace && make lint`
Expected: no new issues

- [ ] **Step 10: Commit**

```bash
git add internal/gateway/proxy.go internal/gateway/proxy_test.go internal/gateway/testdata/rules/test-gateway.yaml
git commit -m "feat(gateway): add streaming support with buffer-then-replay

Streaming requests (stream: true) are now forwarded to upstream with
SSE, buffered, reassembled for policy evaluation, then replayed to
the client. Redacted responses get synthesized SSE events."
```
