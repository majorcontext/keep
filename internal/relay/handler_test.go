package relay

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
	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// mockUpstreamWithEcho starts a mock MCP server that echoes tool call args back
// as JSON in the result content, enabling redaction verification.
func mockUpstreamWithEcho(t *testing.T, tools []mcp.Tool) *httptest.Server {
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
			// Echo back the arguments so tests can verify mutations.
			paramsBytes, _ := json.Marshal(req.Params)
			var params mcp.ToolCallParams
			json.Unmarshal(paramsBytes, &params)
			argsJSON, _ := json.Marshal(params.Arguments)
			resp.Result = mcp.ToolCallResult{
				Content: []mcp.ContentBlock{{Type: "text", Text: string(argsJSON)}},
			}
		}

		json.NewEncoder(w).Encode(resp)
	}))
}

// mockUpstreamError starts a mock MCP server that always returns a JSON-RPC
// error for tools/call requests.
func mockUpstreamError(t *testing.T, tools []mcp.Tool) *httptest.Server {
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
			resp.Error = &mcp.JSONRPCError{Code: -32000, Message: "upstream internal error"}
		}

		json.NewEncoder(w).Encode(resp)
	}))
}

// setupTest builds a RelayHandler backed by a mock upstream and the test rules
// in testdata/rules/. Returns the handler and a buffer that captures audit log
// output.
func setupTest(t *testing.T) (*RelayHandler, *bytes.Buffer) {
	t.Helper()

	tools := []mcp.Tool{
		{Name: "allowed_tool", Description: "An allowed tool"},
		{Name: "dangerous_tool", Description: "A dangerous tool"},
		{Name: "read_file", Description: "Reads a file"},
	}

	// 1. Start mock upstream with echo behaviour.
	srv := mockUpstreamWithEcho(t, tools)
	t.Cleanup(srv.Close)

	// 2. Load Keep engine from testdata/rules/.
	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	// 3. Build Router from the mock upstream.
	routes := []relayconfig.Route{
		{Scope: "test-scope", Upstream: srv.URL},
	}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// 4. Create audit logger writing to a buffer.
	var buf bytes.Buffer
	logger := audit.NewLogger(&buf)

	// 5. Create RelayHandler.
	handler := NewRelayHandler(engine, router, logger)

	return handler, &buf
}

// TestHandler_Allow verifies that a tool with no matching deny rules is
// forwarded to the upstream and the result is returned to the caller.
func TestHandler_Allow(t *testing.T) {
	handler, _ := setupTest(t)

	result, err := handler.HandleToolCall(context.Background(), "allowed_tool", map[string]any{"x": "1"})
	if err != nil {
		t.Fatalf("HandleToolCall: unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("HandleToolCall: expected non-empty result")
	}
}

// TestHandler_Deny verifies that a tool matched by a deny rule returns an
// error containing the rule name and policy message. The upstream must never
// be called.
func TestHandler_Deny(t *testing.T) {
	handler, _ := setupTest(t)

	result, err := handler.HandleToolCall(context.Background(), "dangerous_tool", map[string]any{})
	if err == nil {
		t.Fatal("HandleToolCall: expected error for denied tool, got nil")
	}
	if result != nil {
		t.Errorf("HandleToolCall: expected nil result on deny, got %+v", result)
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "block-dangerous") {
		t.Errorf("error should contain rule name %q, got: %s", "block-dangerous", errStr)
	}
	if !strings.Contains(errStr, "This tool is blocked by policy.") {
		t.Errorf("error should contain policy message, got: %s", errStr)
	}
}

// TestHandler_Redact verifies that args containing secrets are redacted before
// being forwarded to the upstream. The mock upstream echoes the args it
// received so we can inspect them.
func TestHandler_Redact(t *testing.T) {
	handler, _ := setupTest(t)

	args := map[string]any{
		"path":    "/etc/config",
		"content": "value=SECRET_KEY other=SECRET_TOKEN",
	}

	result, err := handler.HandleToolCall(context.Background(), "read_file", args)
	if err != nil {
		t.Fatalf("HandleToolCall: unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("HandleToolCall: expected non-empty result")
	}

	// The upstream echoed the args it received. Parse them back.
	echoedText := result.Content[0].Text
	var echoedArgs map[string]any
	if err := json.Unmarshal([]byte(echoedText), &echoedArgs); err != nil {
		t.Fatalf("parse echoed args: %v", err)
	}

	content, ok := echoedArgs["content"].(string)
	if !ok {
		t.Fatalf("echoed args missing 'content' string field")
	}
	if strings.Contains(content, "SECRET_KEY") || strings.Contains(content, "SECRET_TOKEN") {
		t.Errorf("upstream received unredacted content: %s", content)
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Errorf("upstream content does not contain redaction marker: %s", content)
	}
}

// TestHandler_UnknownTool verifies that a call to a tool not in the router
// returns an error.
func TestHandler_UnknownTool(t *testing.T) {
	handler, _ := setupTest(t)

	result, err := handler.HandleToolCall(context.Background(), "nonexistent_tool", map[string]any{})
	if err == nil {
		t.Fatal("HandleToolCall: expected error for unknown tool, got nil")
	}
	if result != nil {
		t.Errorf("HandleToolCall: expected nil result for unknown tool, got %+v", result)
	}
}

// TestHandler_UpstreamError verifies that when the upstream returns an error
// it is surfaced to the caller.
func TestHandler_UpstreamError(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "allowed_tool", Description: "An allowed tool"},
	}
	srv := mockUpstreamError(t, tools)
	t.Cleanup(srv.Close)

	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	routes := []relayconfig.Route{
		{Scope: "test-scope", Upstream: srv.URL},
	}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	handler := NewRelayHandler(engine, router, nil)

	result, err := handler.HandleToolCall(context.Background(), "allowed_tool", map[string]any{})
	if err == nil {
		t.Fatal("HandleToolCall: expected upstream error, got nil")
	}
	if result != nil {
		t.Errorf("HandleToolCall: expected nil result on upstream error, got %+v", result)
	}
	if !strings.Contains(err.Error(), "upstream internal error") {
		t.Errorf("error should mention upstream error message, got: %v", err)
	}
}

// mockUpstreamWithFixedResponse starts a mock MCP server that returns a fixed
// ToolCallResult for every tools/call request.
func mockUpstreamWithFixedResponse(t *testing.T, tools []mcp.Tool, fixedResult mcp.ToolCallResult) *httptest.Server {
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
			resp.Result = fixedResult
		}

		json.NewEncoder(w).Encode(resp)
	}))
}

// TestHandler_ResponseRedact verifies that response content containing
// passwords is redacted by response-side policy rules.
func TestHandler_ResponseRedact(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "read_query", Description: "Runs a read query"},
		{Name: "write_query", Description: "Runs a write query"},
	}

	responseContent := `[{"id":1,"name":"Alice","password":"hunter2"},{"id":2,"name":"Bob","password":"p@ssw0rd!"}]`
	fixedResult := mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: responseContent}},
	}

	srv := mockUpstreamWithFixedResponse(t, tools, fixedResult)
	t.Cleanup(srv.Close)

	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	routes := []relayconfig.Route{
		{Scope: "test-response-scope", Upstream: srv.URL},
	}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	logger := audit.NewLogger(&buf)
	handler := NewRelayHandler(engine, router, logger)

	result, err := handler.HandleToolCall(context.Background(), "read_query", map[string]any{"sql": "SELECT * FROM users"})
	if err != nil {
		t.Fatalf("HandleToolCall: unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("HandleToolCall: expected non-empty result")
	}

	text := result.Content[0].Text
	if strings.Contains(text, "hunter2") {
		t.Errorf("response should not contain 'hunter2', got: %s", text)
	}
	if strings.Contains(text, "p@ssw0rd!") {
		t.Errorf("response should not contain 'p@ssw0rd!', got: %s", text)
	}
	if !strings.Contains(text, "********") {
		t.Errorf("response should contain redaction marker '********', got: %s", text)
	}
}

// TestHandler_ResponseDeny verifies that write_query is denied on the request
// side (the block-writes rule has no direction guard).
func TestHandler_ResponseDeny(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "read_query", Description: "Runs a read query"},
		{Name: "write_query", Description: "Runs a write query"},
	}

	srv := mockUpstreamWithEcho(t, tools)
	t.Cleanup(srv.Close)

	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	routes := []relayconfig.Route{
		{Scope: "test-response-scope", Upstream: srv.URL},
	}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	handler := NewRelayHandler(engine, router, nil)

	result, err := handler.HandleToolCall(context.Background(), "write_query", map[string]any{"sql": "DROP TABLE users"})
	if err == nil {
		t.Fatal("HandleToolCall: expected error for denied write_query, got nil")
	}
	if result != nil {
		t.Errorf("HandleToolCall: expected nil result on deny, got %+v", result)
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error should mention 'read-only', got: %v", err)
	}
}

// TestHandler_AuditLogged verifies that after a tool call the audit logger
// buffer contains at least one JSON line with the evaluation decision.
func TestHandler_AuditLogged(t *testing.T) {
	handler, buf := setupTest(t)

	_, _ = handler.HandleToolCall(context.Background(), "allowed_tool", map[string]any{})

	raw := buf.String()
	if raw == "" {
		t.Fatal("audit buffer is empty; expected a JSON audit entry")
	}

	// There may be multiple JSON lines (request + response evaluation).
	// Verify at least one is valid JSON with a "Decision" field.
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("audit entry is not valid JSON: %v\nraw: %s", err, line)
		}
		if _, ok := entry["Decision"]; !ok {
			t.Errorf("audit entry missing 'Decision' field; got keys: %v", entry)
		}
	}
}
