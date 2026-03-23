package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// mockUpstream starts a mock MCP server returning specific tools.
func mockUpstream(t *testing.T, tools []mcp.Tool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

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
			resp.Result = mcp.ToolCallResult{
				Content: []mcp.ContentBlock{{Type: "text", Text: "ok"}},
			}
		}

		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestRouter_BuildTable(t *testing.T) {
	toolA := mcp.Tool{Name: "tool_a", Description: "Tool A"}
	toolB := mcp.Tool{Name: "tool_b", Description: "Tool B"}

	srv1 := mockUpstream(t, []mcp.Tool{toolA})
	defer srv1.Close()
	srv2 := mockUpstream(t, []mcp.Tool{toolB})
	defer srv2.Close()

	routes := []relayconfig.Route{
		{Scope: "scope1", Upstream: srv1.URL},
		{Scope: "scope2", Upstream: srv2.URL},
	}

	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: unexpected error: %v", err)
	}

	routeA, err := router.Lookup("tool_a")
	if err != nil {
		t.Fatalf("Lookup tool_a: %v", err)
	}
	if routeA.Scope != "scope1" {
		t.Errorf("tool_a scope: got %q, want %q", routeA.Scope, "scope1")
	}

	routeB, err := router.Lookup("tool_b")
	if err != nil {
		t.Fatalf("Lookup tool_b: %v", err)
	}
	if routeB.Scope != "scope2" {
		t.Errorf("tool_b scope: got %q, want %q", routeB.Scope, "scope2")
	}
}

func TestRouter_ToolConflict(t *testing.T) {
	conflictingTool := mcp.Tool{Name: "conflicting_tool", Description: "Conflict"}

	srv1 := mockUpstream(t, []mcp.Tool{conflictingTool})
	defer srv1.Close()
	srv2 := mockUpstream(t, []mcp.Tool{conflictingTool})
	defer srv2.Close()

	routes := []relayconfig.Route{
		{Scope: "scope1", Upstream: srv1.URL},
		{Scope: "scope2", Upstream: srv2.URL},
	}

	_, err := NewRouter(context.Background(), routes)
	if err == nil {
		t.Fatal("NewRouter: expected error for conflicting tool, got nil")
	}
	if !strings.Contains(err.Error(), "conflicting_tool") {
		t.Errorf("error message does not mention tool name: %v", err)
	}
}

func TestRouter_LookupHit(t *testing.T) {
	tool := mcp.Tool{Name: "my_tool", Description: "My Tool"}

	srv := mockUpstream(t, []mcp.Tool{tool})
	defer srv.Close()

	routes := []relayconfig.Route{
		{Scope: "myscope", Upstream: srv.URL},
	}

	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	route, err := router.Lookup("my_tool")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if route.Client == nil {
		t.Error("route.Client is nil")
	}
	if route.Scope != "myscope" {
		t.Errorf("route.Scope: got %q, want %q", route.Scope, "myscope")
	}
	if route.Tool.Name != "my_tool" {
		t.Errorf("route.Tool.Name: got %q, want %q", route.Tool.Name, "my_tool")
	}
}

func TestRouter_LookupMiss(t *testing.T) {
	srv := mockUpstream(t, []mcp.Tool{{Name: "existing_tool"}})
	defer srv.Close()

	routes := []relayconfig.Route{
		{Scope: "scope1", Upstream: srv.URL},
	}

	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	_, err = router.Lookup("nonexistent_tool")
	if err == nil {
		t.Fatal("Lookup: expected error for unknown tool, got nil")
	}
}

func TestRouter_MergedToolList(t *testing.T) {
	toolA := mcp.Tool{Name: "tool_a"}
	toolB := mcp.Tool{Name: "tool_b"}
	toolC := mcp.Tool{Name: "tool_c"}

	srv1 := mockUpstream(t, []mcp.Tool{toolA, toolB})
	defer srv1.Close()
	srv2 := mockUpstream(t, []mcp.Tool{toolC})
	defer srv2.Close()

	routes := []relayconfig.Route{
		{Scope: "scope1", Upstream: srv1.URL},
		{Scope: "scope2", Upstream: srv2.URL},
	}

	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	tools := router.Tools()
	if len(tools) != 3 {
		t.Fatalf("Tools(): got %d tools, want 3", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"tool_a", "tool_b", "tool_c"} {
		if !names[want] {
			t.Errorf("Tools() missing %q", want)
		}
	}
}

func TestRouter_MissingAuthToken(t *testing.T) {
	const envVar = "TEST_MISSING_AUTH_TOKEN_VAR"
	_ = os.Unsetenv(envVar)

	routes := []relayconfig.Route{
		{
			Scope:    "authed-scope",
			Upstream: "https://example.com",
			Auth: &relayconfig.Auth{
				Type:     "bearer",
				TokenEnv: envVar,
			},
		},
	}

	_, err := NewRouter(context.Background(), routes)
	if err == nil {
		t.Fatal("expected error for missing auth token env var, got nil")
	}
	if !strings.Contains(err.Error(), envVar) {
		t.Errorf("expected error to mention env var %q, got: %v", envVar, err)
	}
}

func TestRouter_UpstreamDown(t *testing.T) {
	goodTool := mcp.Tool{Name: "good_tool"}

	srvGood := mockUpstream(t, []mcp.Tool{goodTool})
	defer srvGood.Close()

	// Use an invalid address that won't connect
	badUpstream := "http://127.0.0.1:1"

	routes := []relayconfig.Route{
		{Scope: "bad", Upstream: badUpstream},
		{Scope: "good", Upstream: srvGood.URL},
	}

	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: expected success even with one down upstream, got: %v", err)
	}

	// Bad upstream's tools should be excluded
	_, err = router.Lookup("bad_tool")
	if err == nil {
		t.Error("Lookup bad_tool: expected error, got nil")
	}

	// Good upstream's tools should be available
	route, err := router.Lookup("good_tool")
	if err != nil {
		t.Fatalf("Lookup good_tool: %v", err)
	}
	if route.Scope != "good" {
		t.Errorf("good_tool scope: got %q, want %q", route.Scope, "good")
	}
}
