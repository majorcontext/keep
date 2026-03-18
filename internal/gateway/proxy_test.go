package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/gateway/anthropic"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
)

// newTestEngine loads the test-gateway rules and returns a ready engine.
func newTestEngine(t *testing.T) *keep.Engine {
	t.Helper()
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("failed to load test rules: %v", err)
	}
	t.Cleanup(eng.Close)
	return eng
}

// newTestProxy creates a Proxy pointing at the given upstream server.
func newTestProxy(t *testing.T, upstreamURL string, logger *audit.Logger) *Proxy {
	t.Helper()
	eng := newTestEngine(t)
	cfg := &gwconfig.GatewayConfig{
		Listen:   ":0",
		RulesDir: "testdata/rules",
		Provider: "anthropic",
		Upstream: upstreamURL,
		Scope:    "test-gateway",
	}
	p, err := NewProxy(eng, cfg, logger)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	return p
}

// echoUpstream returns a test server that echoes the request body as a MessagesResponse.
func echoUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		// Return a simple MessagesResponse with text content echoing the model.
		var req anthropic.MessagesRequest
		_ = json.Unmarshal(body, &req)

		resp := anthropic.MessagesResponse{
			ID:         "msg_test_123",
			Type:       "message",
			Role:       "assistant",
			Model:      req.Model,
			StopReason: "end_turn",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: "Hello from upstream"},
			},
			Usage: &anthropic.Usage{InputTokens: 10, OutputTokens: 5},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// destructiveUpstream returns a test server that returns a tool_use with rm -rf.
func destructiveUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropic.MessagesResponse{
			ID:         "msg_test_456",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-sonnet-4-20250514",
			StopReason: "tool_use",
			Content: []anthropic.ContentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_01",
					Name:  "bash",
					Input: map[string]any{"command": "rm -rf /"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestProxy_AllowRequest(t *testing.T) {
	upstream := echoUpstream()
	defer upstream.Close()

	var buf bytes.Buffer
	logger := audit.NewLogger(&buf)
	p := newTestProxy(t, upstream.URL, logger)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello, world!"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result anthropic.MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID != "msg_test_123" {
		t.Errorf("expected response ID msg_test_123, got %s", result.ID)
	}
	if result.Content[0].Text != "Hello from upstream" {
		t.Errorf("expected 'Hello from upstream', got %q", result.Content[0].Text)
	}
}

func TestProxy_DenyRequest(t *testing.T) {
	upstream := echoUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	// Create a request with huge content to trigger context-size-limit.
	// The rule triggers when token_estimate > 100000, which means ~400000 chars.
	bigContent := strings.Repeat("x", 500000)

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{Role: "user", Content: bigContent},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Type != "policy_denied" {
		t.Errorf("expected policy_denied, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "context-size-limit") {
		t.Errorf("expected context-size-limit in message, got %q", errResp.Error.Message)
	}
}

func TestProxy_RedactRequest(t *testing.T) {
	// Use a recording upstream to verify the body it receives.
	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)

		resp := anthropic.MessagesResponse{
			ID:         "msg_test_789",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: "Got it"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	// tool_result with a secret that should be redacted.
	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{
				Role: "assistant",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "toolu_01", Name: "read_file", Input: map[string]any{"path": "/etc/env"}},
				},
			},
			{
				Role: "user",
				Content: []anthropic.ContentBlock{
					{Type: "tool_result", ToolUseID: "toolu_01", Content: "The key is SECRET_API_KEY_VALUE"},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Check that the upstream received redacted content.
	if len(receivedBody) == 0 {
		t.Fatal("upstream received no body")
	}

	var forwarded anthropic.MessagesRequest
	if err := json.Unmarshal(receivedBody, &forwarded); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}

	// The tool_result content should have the secret redacted.
	if len(forwarded.Messages) < 2 {
		t.Fatal("expected at least 2 messages in forwarded request")
	}

	blocks := forwarded.Messages[1].ContentBlocks()
	if len(blocks) == 0 {
		t.Fatal("expected content blocks in second message")
	}

	content := blocks[0].ToolResultContent()
	if strings.Contains(content, "SECRET_API_KEY_VALUE") {
		t.Errorf("secret was not redacted in forwarded request: %s", content)
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in forwarded content, got: %s", content)
	}
}

func TestProxy_DenyResponse(t *testing.T) {
	upstream := destructiveUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Do something"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Type != "policy_denied" {
		t.Errorf("expected policy_denied, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "block-destructive") {
		t.Errorf("expected block-destructive rule in message, got %q", errResp.Error.Message)
	}
}

func TestProxy_PassthroughNonMessages(t *testing.T) {
	// Upstream that serves /v1/models.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models": ["claude-sonnet-4-20250514"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	resp, err := http.Get(gw.URL + "/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "claude-sonnet-4-20250514") {
		t.Errorf("expected model list in response, got: %s", body)
	}
}

func TestProxy_AuditLogged(t *testing.T) {
	upstream := echoUpstream()
	defer upstream.Close()

	var buf bytes.Buffer
	logger := audit.NewLogger(&buf)
	p := newTestProxy(t, upstream.URL, logger)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Check that audit entries were written.
	logOutput := buf.String()
	if logOutput == "" {
		t.Fatal("expected audit log entries, got none")
	}

	// Should have at least one entry (the request summary).
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 audit line, got %d", len(lines))
	}

	// Verify the first line is valid JSON with expected fields.
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("audit line is not valid JSON: %v", err)
	}
	if entry["Scope"] != "test-gateway" {
		t.Errorf("expected scope test-gateway, got %v", entry["Scope"])
	}
}
