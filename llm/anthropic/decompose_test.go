package anthropic

import (
	"testing"

	"github.com/majorcontext/keep/llm"
)

func boolPtr(b bool) *bool { return &b }

// --- Request decomposition tests ---

func TestDecomposeRequest_Summary(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}
	calls := decomposeRequest(req, "test", llm.DecomposeConfig{})

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	c := calls[0]
	if c.Operation != "llm.request" {
		t.Errorf("expected operation llm.request, got %s", c.Operation)
	}
	if c.Params["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %v", c.Params["model"])
	}
	if c.Params["message_count"] != 1 {
		t.Errorf("expected message_count 1, got %v", c.Params["message_count"])
	}
	if c.Context.Scope != "test" {
		t.Errorf("expected scope test, got %s", c.Context.Scope)
	}
	if c.Context.Direction != "request" {
		t.Errorf("expected direction request, got %s", c.Context.Direction)
	}
	// token_estimate should be > 0 for "Hello" (5 chars / 4 = 1)
	if est, ok := c.Params["token_estimate"].(int); !ok || est < 1 {
		t.Errorf("expected positive token_estimate, got %v", c.Params["token_estimate"])
	}
}

func TestDecomposeRequest_ToolResults(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "do stuff"},
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_use", ID: "tu_1", Name: "read_file", Input: map[string]any{"path": "/foo"}},
				{Type: "tool_use", ID: "tu_2", Name: "write_file", Input: map[string]any{"path": "/bar"}},
			}},
			{Role: "user", Content: []ContentBlock{
				{Type: "tool_result", ToolUseID: "tu_1", Content: "file contents here"},
				{Type: "tool_result", ToolUseID: "tu_2", Content: "ok"},
			}},
		},
	}
	calls := decomposeRequest(req, "s", llm.DecomposeConfig{})

	// Should have: 1 summary + 2 tool_results = 3
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	// Verify tool_result calls.
	tr1 := calls[1]
	if tr1.Operation != "llm.tool_result" {
		t.Errorf("expected llm.tool_result, got %s", tr1.Operation)
	}
	if tr1.Params["tool_name"] != "read_file" {
		t.Errorf("expected tool_name read_file, got %v", tr1.Params["tool_name"])
	}
	if tr1.Params["tool_use_id"] != "tu_1" {
		t.Errorf("expected tool_use_id tu_1, got %v", tr1.Params["tool_use_id"])
	}
	if tr1.Params["content"] != "file contents here" {
		t.Errorf("expected content 'file contents here', got %v", tr1.Params["content"])
	}

	tr2 := calls[2]
	if tr2.Params["tool_name"] != "write_file" {
		t.Errorf("expected tool_name write_file, got %v", tr2.Params["tool_name"])
	}
	if tr2.Params["content"] != "ok" {
		t.Errorf("expected content 'ok', got %v", tr2.Params["content"])
	}
}

func TestDecomposeRequest_TextBlocks_Disabled(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello world"},
			{Role: "assistant", Content: "Hi there"},
		},
	}
	// Default config: text=false.
	calls := decomposeRequest(req, "s", llm.DecomposeConfig{})

	// Should only have the summary call; no text blocks.
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (summary only), got %d", len(calls))
	}
	if calls[0].Operation != "llm.request" {
		t.Errorf("expected llm.request, got %s", calls[0].Operation)
	}
}

func TestDecomposeRequest_TextBlocks_Enabled(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
	}
	cfg := llm.DecomposeConfig{Text: boolPtr(true)}
	calls := decomposeRequest(req, "s", cfg)

	// 1 summary + 2 text blocks = 3
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	txt1 := calls[1]
	if txt1.Operation != "llm.text" {
		t.Errorf("expected llm.text, got %s", txt1.Operation)
	}
	if txt1.Params["text"] != "Hello" {
		t.Errorf("expected text 'Hello', got %v", txt1.Params["text"])
	}
	if txt1.Params["role"] != "user" {
		t.Errorf("expected role user, got %v", txt1.Params["role"])
	}

	txt2 := calls[2]
	if txt2.Params["text"] != "Hi" {
		t.Errorf("expected text 'Hi', got %v", txt2.Params["text"])
	}
	if txt2.Params["role"] != "assistant" {
		t.Errorf("expected role assistant, got %v", txt2.Params["role"])
	}
}

func TestDecomposeRequest_NoToolResults(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Just text"},
		},
	}
	calls := decomposeRequest(req, "s", llm.DecomposeConfig{})

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Operation != "llm.request" {
		t.Errorf("expected llm.request, got %s", calls[0].Operation)
	}
	if calls[0].Params["tool_result_count"] != 0 {
		t.Errorf("expected tool_result_count 0, got %v", calls[0].Params["tool_result_count"])
	}
}

func TestDecomposeRequest_ToolNameLookup(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "run the tool"},
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_use", ID: "tu_abc", Name: "bash", Input: map[string]any{"cmd": "ls"}},
			}},
			{Role: "user", Content: []ContentBlock{
				{Type: "tool_result", ToolUseID: "tu_abc", Content: "file1.txt\nfile2.txt"},
			}},
		},
	}
	calls := decomposeRequest(req, "s", llm.DecomposeConfig{})

	// Find the tool_result call.
	var found bool
	for _, c := range calls {
		if c.Operation == "llm.tool_result" {
			found = true
			if c.Params["tool_name"] != "bash" {
				t.Errorf("expected tool_name 'bash', got %v", c.Params["tool_name"])
			}
			if c.Params["tool_use_id"] != "tu_abc" {
				t.Errorf("expected tool_use_id 'tu_abc', got %v", c.Params["tool_use_id"])
			}
		}
	}
	if !found {
		t.Error("expected to find an llm.tool_result call")
	}
}

// --- Response decomposition tests ---

func TestDecomposeResponse_Summary(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_1",
		Role:       "assistant",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello!"},
		},
	}
	calls := decomposeResponse(resp, "s", llm.DecomposeConfig{})

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	c := calls[0]
	if c.Operation != "llm.response" {
		t.Errorf("expected llm.response, got %s", c.Operation)
	}
	if c.Params["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason end_turn, got %v", c.Params["stop_reason"])
	}
	if c.Params["tool_use_count"] != 0 {
		t.Errorf("expected tool_use_count 0, got %v", c.Params["tool_use_count"])
	}
}

func TestDecomposeResponse_ToolUse(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_2",
		Role:       "assistant",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "read_file", Input: map[string]any{"path": "/etc/hosts"}},
		},
	}
	calls := decomposeResponse(resp, "s", llm.DecomposeConfig{})

	// 1 summary + 1 tool_use = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	tu := calls[1]
	if tu.Operation != "llm.tool_use" {
		t.Errorf("expected llm.tool_use, got %s", tu.Operation)
	}
	if tu.Params["name"] != "read_file" {
		t.Errorf("expected name read_file, got %v", tu.Params["name"])
	}
	if input, ok := tu.Params["input"].(map[string]any); !ok || input["path"] != "/etc/hosts" {
		t.Errorf("expected input with path /etc/hosts, got %v", tu.Params["input"])
	}
}

func TestDecomposeResponse_MultipleBlocks(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_3",
		Role:       "assistant",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me check that for you."},
			{Type: "tool_use", ID: "tu_1", Name: "search", Input: map[string]any{"q": "test"}},
		},
	}
	// Default config: text=false, tool_use=true.
	calls := decomposeResponse(resp, "s", llm.DecomposeConfig{})

	// 1 summary + 1 tool_use (text skipped) = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Operation != "llm.response" {
		t.Errorf("expected llm.response, got %s", calls[0].Operation)
	}
	if calls[1].Operation != "llm.tool_use" {
		t.Errorf("expected llm.tool_use, got %s", calls[1].Operation)
	}
}

func TestDecomposeResponse_Direction(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_4",
		Role:       "assistant",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "bash", Input: map[string]any{}},
		},
	}
	cfg := llm.DecomposeConfig{Text: boolPtr(true)}
	calls := decomposeResponse(resp, "s", cfg)

	for i, c := range calls {
		if c.Context.Direction != "response" {
			t.Errorf("call %d: expected direction response, got %s", i, c.Context.Direction)
		}
	}
}
