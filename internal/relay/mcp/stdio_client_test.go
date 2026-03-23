package mcp

import (
	"context"
	"testing"
)

func TestStdioClient_InitializeAndListTools(t *testing.T) {
	client, err := NewStdioClient("go", "run", "testdata/mock_stdio_server.go")
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	result, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("ProtocolVersion = %q, want %q", result.ProtocolVersion, "2025-03-26")
	}
	if result.ServerInfo.Name != "mock-stdio-server" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "mock-stdio-server")
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("tools[0].Name = %q, want %q", tools[0].Name, "echo")
	}
}

// TestStdioClient_NoisyServer verifies the client correctly handles async
// notifications interleaved with responses (as real MCP servers often do).
func TestStdioClient_NoisyServer(t *testing.T) {
	client, err := NewStdioClient("go", "run", "testdata/mock_noisy_server.go")
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	result, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if result.ServerInfo.Name != "mock-noisy" {
		t.Errorf("ServerInfo.Name = %q, want %q", result.ServerInfo.Name, "mock-noisy")
	}

	// ListTools should succeed even though the server sends a notification before the response.
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	if tools[0].Name != "read_query" {
		t.Errorf("tools[0].Name = %q, want %q", tools[0].Name, "read_query")
	}
}

func TestStdioClient_CallTool(t *testing.T) {
	client, err := NewStdioClient("go", "run", "testdata/mock_stdio_server.go")
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	args := map[string]any{"message": "hello"}
	result, err := client.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(content) = %d, want 1", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("content[0].Type = %q, want %q", result.Content[0].Type, "text")
	}
	if result.Content[0].Text != `{"message":"hello"}` {
		t.Errorf("content[0].Text = %q, want %q", result.Content[0].Text, `{"message":"hello"}`)
	}
}
