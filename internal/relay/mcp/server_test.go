package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockHandler struct {
	lastToolName string
	lastArgs     map[string]any
	result       *ToolCallResult
	err          error
}

func (h *mockHandler) HandleToolCall(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	h.lastToolName = name
	h.lastArgs = args
	return h.result, h.err
}

var testTools = []Tool{
	{Name: "say_hello", Description: "Says hello", InputSchema: map[string]any{"type": "object"}},
	{Name: "say_bye", Description: "Says bye", InputSchema: map[string]any{"type": "object"}},
}

func postJSON(t *testing.T, srv *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) JSONRPCResponse {
	t.Helper()
	var resp JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestServer_Initialize(t *testing.T) {
	srv := NewServer(testTools, &mockHandler{})

	w := postJSON(t, srv, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.ID != float64(1) {
		t.Errorf("expected ID 1, got %v", resp.ID)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("expected protocol version 2025-03-26, got %q", result.ProtocolVersion)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability to be set")
	}
	if result.ServerInfo.Name != "keep-mcp-relay" {
		t.Errorf("expected server name keep-mcp-relay, got %q", result.ServerInfo.Name)
	}
}

func TestServer_ListTools(t *testing.T) {
	srv := NewServer(testTools, &mockHandler{})

	w := postJSON(t, srv, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result ListToolsResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if len(result.Tools) != len(testTools) {
		t.Fatalf("expected %d tools, got %d", len(testTools), len(result.Tools))
	}
	if result.Tools[0].Name != "say_hello" {
		t.Errorf("expected first tool say_hello, got %q", result.Tools[0].Name)
	}
	if result.Tools[1].Name != "say_bye" {
		t.Errorf("expected second tool say_bye, got %q", result.Tools[1].Name)
	}
}

func TestServer_CallTool(t *testing.T) {
	handler := &mockHandler{
		result: &ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: "hello world"}},
		},
	}
	srv := NewServer(testTools, handler)

	args := map[string]any{"greeting": "hi"}
	w := postJSON(t, srv, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "say_hello",
			"arguments": args,
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	if handler.lastToolName != "say_hello" {
		t.Errorf("expected tool name say_hello, got %q", handler.lastToolName)
	}
	if handler.lastArgs["greeting"] != "hi" {
		t.Errorf("expected arg greeting=hi, got %v", handler.lastArgs["greeting"])
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello world" {
		t.Errorf("unexpected content: %+v", result.Content)
	}
}

func TestServer_CallTool_Error(t *testing.T) {
	handler := &mockHandler{
		err: errors.New("something went wrong"),
	}
	srv := NewServer(testTools, handler)

	w := postJSON(t, srv, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "say_hello",
			"arguments": map[string]any{},
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("expected error code -32000, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %q", resp.Error.Message)
	}
}

func TestServer_InvalidMethod(t *testing.T) {
	srv := NewServer(testTools, &mockHandler{})

	w := postJSON(t, srv, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "nonexistent/method",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestServer_OversizedBody(t *testing.T) {
	srv := NewServer(testTools, &mockHandler{})

	// Create a body larger than 4MB
	bigBody := make([]byte, 5<<20) // 5 MB
	for i := range bigBody {
		bigBody[i] = 'a'
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bigBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected error code -32700, got %d", resp.Error.Code)
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	srv := NewServer(testTools, &mockHandler{})

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected error code -32700, got %d", resp.Error.Code)
	}
}
