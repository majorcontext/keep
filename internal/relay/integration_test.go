//go:build e2e

package relay_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/relay"
	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// startMockUpstream creates a minimal MCP server serving the provided tools
// and echoing tool call arguments back as JSON text content.
func startMockUpstream(t *testing.T, tools []mcp.Tool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		var resp mcp.JSONRPCResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = mcp.InitializeResult{
				ProtocolVersion: "2025-03-26",
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			}
		case "tools/list":
			resp.Result = mcp.ListToolsResult{Tools: tools}
		case "tools/call":
			paramsBytes, _ := json.Marshal(req.Params)
			var params mcp.ToolCallParams
			json.Unmarshal(paramsBytes, &params)
			argsJSON, _ := json.Marshal(params.Arguments)
			resp.Result = mcp.ToolCallResult{
				Content: []mcp.ContentBlock{{Type: "text", Text: string(argsJSON)}},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// TestRelayIntegration exercises the full relay stack end-to-end:
// mock upstream → router → handler → MCP server → HTTP client.
func TestRelayIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Start a mock upstream MCP server with two tools.
	upstreamTools := []mcp.Tool{
		{Name: "allowed_tool", Description: "An allowed tool"},
		{Name: "blocked_tool", Description: "A blocked tool"},
	}
	mockUpstream := startMockUpstream(t, upstreamTools)
	t.Cleanup(mockUpstream.Close)

	// 2. Load Keep engine from testdata/rules/ (e2e-tools.yaml defines e2e-scope
	//    with a deny rule for blocked_tool).
	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	// 3. Build Router pointing at the mock upstream with scope "e2e-scope".
	routes := []relayconfig.Route{
		{Scope: "e2e-scope", Upstream: mockUpstream.URL},
	}
	router, err := relay.NewRouter(ctx, routes)
	if err != nil {
		t.Fatalf("relay.NewRouter: %v", err)
	}

	// 4. Create audit logger writing to a buffer.
	var auditBuf bytes.Buffer
	logger := audit.NewLogger(&auditBuf)

	// 5. Create RelayHandler.
	handler := relay.NewRelayHandler(engine, router, logger, "")

	// 6. Create MCP server with merged tools and handler.
	relayServer := mcp.NewServer(router.Tools(), handler)

	// 7. Start the relay HTTP server.
	relayHTTP := httptest.NewServer(relayServer)
	t.Cleanup(relayHTTP.Close)

	// 8. Connect an MCP client to the relay.
	client := mcp.NewClient(relayHTTP.URL)

	// Initialize handshake.
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("client.Initialize: %v", err)
	}

	// ListTools should expose both tools.
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("client.ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("ListTools: got %d tools, want 2", len(tools))
	}
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	for _, want := range []string{"allowed_tool", "blocked_tool"} {
		if !toolNames[want] {
			t.Errorf("ListTools: missing tool %q", want)
		}
	}

	// Call allowed_tool — should succeed and return upstream result.
	result, err := client.CallTool(ctx, "allowed_tool", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("CallTool(allowed_tool): unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("CallTool(allowed_tool): expected non-empty result")
	}

	// Call blocked_tool — should fail with a policy denial error.
	_, err = client.CallTool(ctx, "blocked_tool", map[string]any{})
	if err == nil {
		t.Fatal("CallTool(blocked_tool): expected policy denial error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "This tool is blocked") {
		t.Errorf("CallTool(blocked_tool): error should mention blocking; got: %v", err)
	}

	// 9. Verify audit buffer contains entries for both calls.
	auditOutput := auditBuf.String()
	if auditOutput == "" {
		t.Fatal("audit buffer is empty; expected at least one JSON audit entry")
	}

	lines := strings.Split(strings.TrimSpace(auditOutput), "\n")
	if len(lines) < 2 {
		t.Fatalf("audit buffer: got %d line(s), want at least 2", len(lines))
	}

	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("audit entry[%d] is not valid JSON: %v\nraw: %s", i, err, line)
		}
		if _, ok := entry["Decision"]; !ok {
			t.Errorf("audit entry[%d] missing 'Decision' field; keys: %v", i, entry)
		}
	}
}
