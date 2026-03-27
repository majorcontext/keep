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
    match:
      operation: "*"
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := anthropic.NewCodec()
	reqBody, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	})

	result, err := llm.EvaluateRequest(engine, codec, reqBody, "gateway", llm.DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got %q, want allow", result.Decision)
	}
	if result.Body == nil {
		t.Error("expected non-nil body for allow decision")
	}
}

func TestPipeline_Anthropic_StreamAllow(t *testing.T) {
	rules := []byte(`
scope: gateway
mode: enforce
rules:
  - name: log-all
    match:
      operation: "*"
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := anthropic.NewCodec()

	msgStart, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "claude-sonnet-4-20250514", "content": []any{},
			"stop_reason": nil,
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 0},
		},
	})
	blockStart, _ := json.Marshal(map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	blockDelta, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": "Hi!"},
	})
	blockStop, _ := json.Marshal(map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	msgDelta, _ := json.Marshal(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
		"usage": map[string]any{"output_tokens": 3},
	})
	msgStop, _ := json.Marshal(map[string]any{
		"type": "message_stop",
	})

	events := []sse.Event{
		{Type: "message_start", Data: string(msgStart)},
		{Type: "content_block_start", Data: string(blockStart)},
		{Type: "content_block_delta", Data: string(blockDelta)},
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
		t.Errorf("got %d events, want %d (original replayed)", len(result.Events), len(events))
	}
}
