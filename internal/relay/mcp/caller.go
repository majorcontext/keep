package mcp

import "context"

// ToolCaller is the interface for MCP clients that can initialize,
// discover tools, and call tools. Both HTTP and stdio clients implement this.
type ToolCaller interface {
	Initialize(ctx context.Context) (*InitializeResult, error)
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}
