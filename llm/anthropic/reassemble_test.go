package anthropic

import (
	"testing"

	keep "github.com/majorcontext/keep"
)

// --- reassembleRequest tests ---

func TestReassembleRequest_NoMutations(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-3-opus",
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "tu_1", Content: "original content"},
				},
			},
		},
	}

	results := []blockResult{
		{
			MessageIndex: 0,
			BlockIndex:   0,
			Result:       keep.EvalResult{Decision: keep.Allow},
		},
	}

	got := reassembleRequest(req, results)
	blocks := got.Messages[0].ContentBlocks()
	if len(blocks) == 0 {
		t.Fatal("expected content blocks")
	}
	if blocks[0].ToolResultContent() != "original content" {
		t.Errorf("expected 'original content', got %q", blocks[0].ToolResultContent())
	}
}

func TestReassembleRequest_RedactToolResult(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-3-opus",
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "tu_1", Content: "secret token: abc123"},
				},
			},
		},
	}

	results := []blockResult{
		{
			MessageIndex: 0,
			BlockIndex:   0,
			Result: keep.EvalResult{
				Decision: keep.Redact,
				Mutations: []keep.Mutation{
					{Path: "params.content", Original: "secret token: abc123", Replaced: "secret token: [REDACTED]"},
				},
			},
		},
	}

	got := reassembleRequest(req, results)
	blocks := got.Messages[0].ContentBlocks()
	if len(blocks) == 0 {
		t.Fatal("expected content blocks")
	}
	want := "secret token: [REDACTED]"
	if blocks[0].ToolResultContent() != want {
		t.Errorf("expected %q, got %q", want, blocks[0].ToolResultContent())
	}
}

func TestReassembleRequest_MultipleMutations(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-3-opus",
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "tu_1", Content: "secret-a"},
				},
			},
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "tu_2", Content: "secret-b"},
				},
			},
		},
	}

	results := []blockResult{
		{
			MessageIndex: 0,
			BlockIndex:   0,
			Result: keep.EvalResult{
				Decision: keep.Redact,
				Mutations: []keep.Mutation{
					{Path: "params.content", Original: "secret-a", Replaced: "[REDACTED-A]"},
				},
			},
		},
		{
			MessageIndex: 1,
			BlockIndex:   0,
			Result: keep.EvalResult{
				Decision: keep.Redact,
				Mutations: []keep.Mutation{
					{Path: "params.content", Original: "secret-b", Replaced: "[REDACTED-B]"},
				},
			},
		},
	}

	got := reassembleRequest(req, results)

	blocks0 := got.Messages[0].ContentBlocks()
	if blocks0[0].ToolResultContent() != "[REDACTED-A]" {
		t.Errorf("msg0: expected '[REDACTED-A]', got %q", blocks0[0].ToolResultContent())
	}

	blocks1 := got.Messages[1].ContentBlocks()
	if blocks1[0].ToolResultContent() != "[REDACTED-B]" {
		t.Errorf("msg1: expected '[REDACTED-B]', got %q", blocks1[0].ToolResultContent())
	}
}

func TestReassembleRequest_OriginalUnmodified(t *testing.T) {
	req := &MessagesRequest{
		Model: "claude-3-opus",
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "tu_1", Content: "original"},
				},
			},
		},
	}

	results := []blockResult{
		{
			MessageIndex: 0,
			BlockIndex:   0,
			Result: keep.EvalResult{
				Decision: keep.Redact,
				Mutations: []keep.Mutation{
					{Path: "params.content", Original: "original", Replaced: "[REDACTED]"},
				},
			},
		},
	}

	_ = reassembleRequest(req, results)

	// The original must remain unchanged.
	blocks := req.Messages[0].ContentBlocks()
	if blocks[0].ToolResultContent() != "original" {
		t.Errorf("original was modified: got %q", blocks[0].ToolResultContent())
	}
}

// --- reassembleResponse tests ---

func TestReassembleResponse_NoMutations(t *testing.T) {
	resp := &MessagesResponse{
		ID:   "msg_1",
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello, world!"},
		},
	}

	results := []blockResult{
		{
			BlockIndex: 0,
			Result:     keep.EvalResult{Decision: keep.Allow},
		},
	}

	got := reassembleResponse(resp, results)
	if got.Content[0].Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", got.Content[0].Text)
	}
}

func TestReassembleResponse_RedactToolUseInput(t *testing.T) {
	resp := &MessagesResponse{
		ID:   "msg_1",
		Role: "assistant",
		Content: []ContentBlock{
			{
				Type:  "tool_use",
				ID:    "tu_1",
				Name:  "bash",
				Input: map[string]any{"command": "rm -rf /secret"},
			},
		},
	}

	results := []blockResult{
		{
			BlockIndex: 0,
			Result: keep.EvalResult{
				Decision: keep.Redact,
				Mutations: []keep.Mutation{
					{Path: "params.input.command", Original: "rm -rf /secret", Replaced: "rm -rf [REDACTED]"},
				},
			},
		},
	}

	got := reassembleResponse(resp, results)
	cmd, ok := got.Content[0].Input["command"]
	if !ok {
		t.Fatal("expected 'command' key in Input")
	}
	if cmd != "rm -rf [REDACTED]" {
		t.Errorf("expected 'rm -rf [REDACTED]', got %q", cmd)
	}

	// Original must be untouched.
	if resp.Content[0].Input["command"] != "rm -rf /secret" {
		t.Errorf("original response was modified")
	}
}
