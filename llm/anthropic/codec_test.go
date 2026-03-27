package anthropic

import (
	"encoding/json"
	"testing"

	keep "github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
)

func TestCodec_DecomposeRequest(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello, Claude!"}
		]
	}`)

	c := NewCodec()
	calls, handle, err := c.DecomposeRequest(body, "test-scope", llm.DecomposeConfig{})
	if err != nil {
		t.Fatalf("DecomposeRequest: %v", err)
	}
	if handle == nil {
		t.Fatal("expected non-nil handle")
	}

	// Default config: summary enabled, no text blocks decomposed.
	// Should get 1 call: llm.request summary.
	if len(calls) < 1 {
		t.Fatalf("expected at least 1 call, got %d", len(calls))
	}
	if calls[0].Operation != "llm.request" {
		t.Errorf("expected operation llm.request, got %s", calls[0].Operation)
	}
	if calls[0].Context.Scope != "test-scope" {
		t.Errorf("expected scope test-scope, got %s", calls[0].Context.Scope)
	}
	if calls[0].Context.Direction != "request" {
		t.Errorf("expected direction request, got %s", calls[0].Context.Direction)
	}
}

func TestCodec_DecomposeResponse(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me help you."},
			{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/test.txt"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use"
	}`)

	c := NewCodec()
	calls, handle, err := c.DecomposeResponse(body, "test-scope", llm.DecomposeConfig{})
	if err != nil {
		t.Fatalf("DecomposeResponse: %v", err)
	}
	if handle == nil {
		t.Fatal("expected non-nil handle")
	}

	// Default config: summary + tool_use enabled, text disabled.
	// Should get 2 calls: llm.response summary + llm.tool_use.
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Operation != "llm.response" {
		t.Errorf("expected llm.response, got %s", calls[0].Operation)
	}
	// Find tool_use call.
	found := false
	for _, call := range calls {
		if call.Operation == "llm.tool_use" {
			found = true
			if call.Params["name"] != "read_file" {
				t.Errorf("expected tool name read_file, got %v", call.Params["name"])
			}
		}
	}
	if !found {
		t.Error("expected llm.tool_use call not found")
	}
}

func TestCodec_ReassembleRequest_NoMutations(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hello"}],"max_tokens":1024}`)

	c := NewCodec()
	calls, handle, err := c.DecomposeRequest(body, "test", llm.DecomposeConfig{})
	if err != nil {
		t.Fatalf("DecomposeRequest: %v", err)
	}

	// Build Allow results for all calls.
	results := make([]keep.EvalResult, len(calls))
	for i := range results {
		results[i] = keep.EvalResult{Decision: keep.Allow}
	}

	out, err := c.ReassembleRequest(handle, results)
	if err != nil {
		t.Fatalf("ReassembleRequest: %v", err)
	}

	// With no mutations, should return the original body.
	if string(out) != string(body) {
		t.Errorf("expected original body returned unchanged\ngot:  %s\nwant: %s", out, body)
	}
}

func TestCodec_ReassembleStream(t *testing.T) {
	// Build a minimal set of SSE events for a text response.
	resp := &MessagesResponse{
		ID:         "msg_stream",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello from streaming!"},
		},
		Usage: &Usage{InputTokens: 10, OutputTokens: 5},
	}

	events := SynthesizeEvents(resp)
	if len(events) == 0 {
		t.Fatal("expected non-empty events from SynthesizeEvents")
	}

	c := NewCodec()
	body, err := c.ReassembleStream(events)
	if err != nil {
		t.Fatalf("ReassembleStream: %v", err)
	}

	// Verify the reassembled body contains the text content.
	var got MessagesResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal reassembled body: %v", err)
	}
	if len(got.Content) == 0 {
		t.Fatal("expected content blocks in reassembled response")
	}
	if got.Content[0].Text != "Hello from streaming!" {
		t.Errorf("expected text 'Hello from streaming!', got %q", got.Content[0].Text)
	}
}

func TestCodec_SynthesizeEvents(t *testing.T) {
	body := []byte(`{
		"id": "msg_synth",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Synthesized response"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn"
	}`)

	c := NewCodec()
	events, err := c.SynthesizeEvents(body)
	if err != nil {
		t.Fatalf("SynthesizeEvents: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("expected non-empty events")
	}

	// Verify we can round-trip: synthesize → reassemble.
	roundTripped, err := c.ReassembleStream(events)
	if err != nil {
		t.Fatalf("ReassembleStream round-trip: %v", err)
	}
	var got MessagesResponse
	if err := json.Unmarshal(roundTripped, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Content[0].Text != "Synthesized response" {
		t.Errorf("round-trip text mismatch: got %q", got.Content[0].Text)
	}
}

// Ensure Codec satisfies the llm.Codec interface at compile time.
var _ llm.Codec = (*Codec)(nil)
