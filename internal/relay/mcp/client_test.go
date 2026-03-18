package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// mockMCPServer creates an httptest.Server that invokes handler for each
// incoming JSON-RPC request and writes the returned JSONRPCResponse as JSON.
func mockMCPServer(t *testing.T, handler func(req JSONRPCRequest) JSONRPCResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp := handler(req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestClient_Initialize(t *testing.T) {
	srv := mockMCPServer(t, func(req JSONRPCRequest) JSONRPCResponse {
		if req.Method != "initialize" {
			t.Errorf("unexpected method: %s", req.Method)
		}
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": true},
				},
				"serverInfo": map[string]any{
					"name":    "test-server",
					"version": "1.0.0",
				},
			},
		}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("got protocol version %q, want %q", result.ProtocolVersion, "2025-03-26")
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("got server name %q, want %q", result.ServerInfo.Name, "test-server")
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability, got nil")
	} else if !result.Capabilities.Tools.ListChanged {
		t.Error("expected listChanged=true")
	}
}

func TestClient_ListTools(t *testing.T) {
	srv := mockMCPServer(t, func(req JSONRPCRequest) JSONRPCResponse {
		if req.Method != "tools/list" {
			t.Errorf("unexpected method: %s", req.Method)
		}
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": []any{
					map[string]any{"name": "echo", "description": "echoes input"},
					map[string]any{"name": "add", "description": "adds two numbers"},
				},
			},
		}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("got tool[0].Name=%q, want %q", tools[0].Name, "echo")
	}
	if tools[1].Name != "add" {
		t.Errorf("got tool[1].Name=%q, want %q", tools[1].Name, "add")
	}
}

func TestClient_CallTool(t *testing.T) {
	srv := mockMCPServer(t, func(req JSONRPCRequest) JSONRPCResponse {
		if req.Method != "tools/call" {
			t.Errorf("unexpected method: %s", req.Method)
		}
		// Echo back tool name and args as text content.
		raw, _ := json.Marshal(req.Params)
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": string(raw)},
				},
				"isError": false,
			},
		}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.CallTool(context.Background(), "echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("got content type %q, want %q", result.Content[0].Type, "text")
	}
	if result.IsError {
		t.Error("expected isError=false")
	}
}

func TestClient_CallTool_Error(t *testing.T) {
	srv := mockMCPServer(t, func(req JSONRPCRequest) JSONRPCResponse {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: "method not found",
			},
		}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.CallTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service down"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 503, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention status code 503, got: %v", err)
	}
}

func TestClient_Unreachable(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // port 1 is not listening
	_, err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

func TestClient_WithBearerToken(t *testing.T) {
	const envVar = "TEST_MCP_TOKEN"
	t.Setenv(envVar, "secret-token")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": []any{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithBearerToken(envVar))
	_, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("got Authorization=%q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestClient_WithHeader(t *testing.T) {
	const envVar = "TEST_MCP_API_KEY"
	const headerName = "X-Api-Key"
	t.Setenv(envVar, "api-key-value")

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(headerName)
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": []any{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithHeader(headerName, envVar))
	_, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotHeader != "api-key-value" {
		t.Errorf("got %s=%q, want %q", headerName, gotHeader, "api-key-value")
	}
}

func TestClient_NoAuth_WhenEnvEmpty(t *testing.T) {
	const envVar = "TEST_MCP_EMPTY_TOKEN"
	os.Unsetenv(envVar)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": []any{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithBearerToken(envVar))
	_, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}
