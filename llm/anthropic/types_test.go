package anthropic

import (
	"encoding/json"
	"testing"
)

// realisticRequest is a multi-turn conversation: user text, assistant tool_use, user tool_result.
const realisticRequest = `{
  "model": "claude-opus-4-5",
  "max_tokens": 1024,
  "tools": [
    {
      "name": "get_weather",
      "description": "Get the current weather in a given location",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string", "description": "City and state, e.g. San Francisco, CA"},
          "unit": {"type": "string", "enum": ["celsius", "fahrenheit"]}
        },
        "required": ["location"]
      }
    }
  ],
  "messages": [
    {
      "role": "user",
      "content": "What is the weather like in San Francisco?"
    },
    {
      "role": "assistant",
      "content": [
        {
          "type": "text",
          "text": "I'll check the weather for you."
        },
        {
          "type": "tool_use",
          "id": "toolu_01A09q90qw90lq917835lq9",
          "name": "get_weather",
          "input": {"location": "San Francisco, CA", "unit": "celsius"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "toolu_01A09q90qw90lq917835lq9",
          "content": "15 degrees celsius, partly cloudy"
        }
      ]
    }
  ]
}`

// realisticResponse is an Anthropic response containing a tool_use block.
const realisticResponse = `{
  "id": "msg_01XFDUDYJgAACzvnptvVoYEL",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "I'll look up the current weather for San Francisco."
    },
    {
      "type": "tool_use",
      "id": "toolu_01A09q90qw90lq917835lq9",
      "name": "get_weather",
      "input": {"location": "San Francisco, CA", "unit": "celsius"}
    }
  ],
  "model": "claude-opus-4-5",
  "stop_reason": "tool_use",
  "usage": {
    "input_tokens": 415,
    "output_tokens": 96
  }
}`

func TestMessagesRequest_Unmarshal(t *testing.T) {
	var req MessagesRequest
	if err := json.Unmarshal([]byte(realisticRequest), &req); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if req.Model != "claude-opus-4-5" {
		t.Errorf("model: got %q, want %q", req.Model, "claude-opus-4-5")
	}
	if req.MaxTokens != 1024 {
		t.Errorf("max_tokens: got %d, want 1024", req.MaxTokens)
	}
	if req.Tools == nil {
		t.Error("tools should not be nil")
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages: got %d, want 3", len(req.Messages))
	}

	// First message: user with string content.
	userMsg := req.Messages[0]
	if userMsg.Role != "user" {
		t.Errorf("messages[0].role: got %q, want %q", userMsg.Role, "user")
	}
	blocks0 := userMsg.ContentBlocks()
	if len(blocks0) != 1 || blocks0[0].Type != "text" {
		t.Errorf("messages[0] content blocks: got %+v", blocks0)
	}
	if blocks0[0].Text != "What is the weather like in San Francisco?" {
		t.Errorf("messages[0] text: got %q", blocks0[0].Text)
	}

	// Second message: assistant with array content (text + tool_use).
	assistantMsg := req.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("messages[1].role: got %q, want %q", assistantMsg.Role, "assistant")
	}
	blocks1 := assistantMsg.ContentBlocks()
	if len(blocks1) != 2 {
		t.Fatalf("messages[1] blocks: got %d, want 2", len(blocks1))
	}
	if blocks1[0].Type != "text" {
		t.Errorf("messages[1] blocks[0].type: got %q, want text", blocks1[0].Type)
	}
	if blocks1[1].Type != "tool_use" {
		t.Errorf("messages[1] blocks[1].type: got %q, want tool_use", blocks1[1].Type)
	}
	if blocks1[1].ID != "toolu_01A09q90qw90lq917835lq9" {
		t.Errorf("tool_use id: got %q", blocks1[1].ID)
	}
	if blocks1[1].Name != "get_weather" {
		t.Errorf("tool_use name: got %q", blocks1[1].Name)
	}

	// Third message: user with tool_result array content.
	toolResultMsg := req.Messages[2]
	blocks2 := toolResultMsg.ContentBlocks()
	if len(blocks2) != 1 {
		t.Fatalf("messages[2] blocks: got %d, want 1", len(blocks2))
	}
	if blocks2[0].Type != "tool_result" {
		t.Errorf("messages[2] blocks[0].type: got %q, want tool_result", blocks2[0].Type)
	}
	if blocks2[0].ToolUseID != "toolu_01A09q90qw90lq917835lq9" {
		t.Errorf("tool_result tool_use_id: got %q", blocks2[0].ToolUseID)
	}
}

func TestMessagesResponse_Unmarshal(t *testing.T) {
	var resp MessagesResponse
	if err := json.Unmarshal([]byte(realisticResponse), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if resp.ID != "msg_01XFDUDYJgAACzvnptvVoYEL" {
		t.Errorf("id: got %q", resp.ID)
	}
	if resp.Type != "message" {
		t.Errorf("type: got %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("role: got %q, want assistant", resp.Role)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason: got %q, want tool_use", resp.StopReason)
	}
	if resp.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if resp.Usage.InputTokens != 415 {
		t.Errorf("input_tokens: got %d, want 415", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 96 {
		t.Errorf("output_tokens: got %d, want 96", resp.Usage.OutputTokens)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("content blocks: got %d, want 2", len(resp.Content))
	}
	if resp.Content[1].Type != "tool_use" {
		t.Errorf("content[1].type: got %q, want tool_use", resp.Content[1].Type)
	}
}

func TestMessage_ContentBlocks_String(t *testing.T) {
	m := Message{Role: "user", Content: "Hello, world!"}
	blocks := m.ContentBlocks()
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("type: got %q, want text", blocks[0].Type)
	}
	if blocks[0].Text != "Hello, world!" {
		t.Errorf("text: got %q", blocks[0].Text)
	}
}

func TestMessage_ContentBlocks_Array(t *testing.T) {
	raw := `{"role":"user","content":[{"type":"text","text":"foo"},{"type":"tool_result","tool_use_id":"tu_1","content":"bar"}]}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	blocks := m.ContentBlocks()
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "foo" {
		t.Errorf("blocks[0]: %+v", blocks[0])
	}
	if blocks[1].Type != "tool_result" || blocks[1].ToolUseID != "tu_1" {
		t.Errorf("blocks[1]: %+v", blocks[1])
	}
}

func TestContentBlock_ToolResultContent_String(t *testing.T) {
	b := ContentBlock{
		Type:      "tool_result",
		ToolUseID: "tu_1",
		Content:   "15 degrees celsius, partly cloudy",
	}
	got := b.ToolResultContent()
	want := "15 degrees celsius, partly cloudy"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContentBlock_ToolResultContent_Array(t *testing.T) {
	// Simulate JSON unmarshal: Content becomes []any with map[string]any items.
	raw := `{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"cloudy and cool"}]}`
	var b ContentBlock
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := b.ToolResultContent()
	want := "cloudy and cool"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContentBlock_ToolResultContent_MultipleTextBlocks(t *testing.T) {
	// Simulate JSON unmarshal: Content has multiple text blocks that should be joined.
	raw := `{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"},{"type":"text","text":"line three"}]}`
	var b ContentBlock
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := b.ToolResultContent()
	want := "line one\nline two\nline three"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMessagesRequest_Marshal(t *testing.T) {
	var req MessagesRequest
	if err := json.Unmarshal([]byte(realisticRequest), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, err := json.Marshal(&req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Round-trip: unmarshal the re-marshaled JSON and verify key fields.
	var req2 MessagesRequest
	if err := json.Unmarshal(data, &req2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}

	if req2.Model != req.Model {
		t.Errorf("model: got %q, want %q", req2.Model, req.Model)
	}
	if req2.MaxTokens != req.MaxTokens {
		t.Errorf("max_tokens: got %d, want %d", req2.MaxTokens, req.MaxTokens)
	}
	if len(req2.Messages) != len(req.Messages) {
		t.Errorf("messages len: got %d, want %d", len(req2.Messages), len(req.Messages))
	}

	// Verify the assistant message content blocks survive the round-trip.
	blocks := req2.Messages[1].ContentBlocks()
	if len(blocks) != 2 {
		t.Fatalf("round-trip assistant blocks: got %d, want 2", len(blocks))
	}
	if blocks[1].Type != "tool_use" {
		t.Errorf("round-trip blocks[1].type: got %q, want tool_use", blocks[1].Type)
	}
	if blocks[1].ID != "toolu_01A09q90qw90lq917835lq9" {
		t.Errorf("round-trip tool_use id: got %q", blocks[1].ID)
	}
}

func TestMessagesResponse_Marshal(t *testing.T) {
	var resp MessagesResponse
	if err := json.Unmarshal([]byte(realisticResponse), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, err := json.Marshal(&resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var resp2 MessagesResponse
	if err := json.Unmarshal(data, &resp2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}

	if resp2.ID != resp.ID {
		t.Errorf("id: got %q, want %q", resp2.ID, resp.ID)
	}
	if resp2.StopReason != resp.StopReason {
		t.Errorf("stop_reason: got %q, want %q", resp2.StopReason, resp.StopReason)
	}
	if resp2.Usage == nil || resp2.Usage.InputTokens != resp.Usage.InputTokens {
		t.Errorf("usage input_tokens mismatch after round-trip")
	}
	if len(resp2.Content) != len(resp.Content) {
		t.Errorf("content len: got %d, want %d", len(resp2.Content), len(resp.Content))
	}
	if resp2.Content[1].Type != "tool_use" {
		t.Errorf("content[1].type: got %q, want tool_use", resp2.Content[1].Type)
	}
	if resp2.Content[1].Name != "get_weather" {
		t.Errorf("content[1].name: got %q, want get_weather", resp2.Content[1].Name)
	}
}
