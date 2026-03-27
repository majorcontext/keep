//go:build e2e

package gateway_test

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
	"github.com/majorcontext/keep/internal/gateway"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
	"github.com/majorcontext/keep/llm/anthropic"
)

// newE2EEngine loads the test-gateway rules and returns a ready engine.
func newE2EEngine(t *testing.T) *keep.Engine {
	t.Helper()
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("failed to load test rules: %v", err)
	}
	t.Cleanup(eng.Close)
	return eng
}

// newE2EProxy creates a gateway Proxy pointing at the given upstream URL.
func newE2EProxy(t *testing.T, upstreamURL string) *gateway.Proxy {
	t.Helper()
	eng := newE2EEngine(t)
	cfg := &gwconfig.GatewayConfig{
		Listen:   ":0",
		RulesDir: "testdata/rules",
		Provider: "anthropic",
		Upstream: upstreamURL,
		Scope:    "test-gateway",
	}
	var buf bytes.Buffer
	logger := audit.NewLogger(&buf)
	p, err := gateway.NewProxy(eng, cfg, logger)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	return p
}

// TestGatewayIntegration_AllowAndRedact sends a request containing a secret in a tool_result.
// It verifies the upstream receives the redacted payload and the client gets a valid response.
func TestGatewayIntegration_AllowAndRedact(t *testing.T) {
	var receivedBody []byte

	// Mock upstream that echoes back what it received and returns a canned response.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)

		resp := anthropic.MessagesResponse{
			ID:         "msg_e2e_redact",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: "Processed successfully"},
			},
			Usage: &anthropic.Usage{InputTokens: 20, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	proxy := newE2EProxy(t, upstream.URL)
	gw := httptest.NewServer(proxy)
	defer gw.Close()

	// Request with a tool_result containing a secret.
	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{
				Role: "assistant",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "toolu_e2e_01", Name: "fetch_config", Input: map[string]any{"path": "/config"}},
				},
			},
			{
				Role: "user",
				Content: []anthropic.ContentBlock{
					{Type: "tool_result", ToolUseID: "toolu_e2e_01", Content: "Config value: SECRET_API_KEY=abc123"},
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

	// Client should receive a successful response.
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result anthropic.MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode client response: %v", err)
	}
	if result.ID != "msg_e2e_redact" {
		t.Errorf("expected response ID msg_e2e_redact, got %s", result.ID)
	}

	// Verify the mock upstream received the redacted version.
	if len(receivedBody) == 0 {
		t.Fatal("upstream received no body")
	}
	var forwarded anthropic.MessagesRequest
	if err := json.Unmarshal(receivedBody, &forwarded); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	if len(forwarded.Messages) < 2 {
		t.Fatal("expected at least 2 messages in forwarded request")
	}
	blocks := forwarded.Messages[1].ContentBlocks()
	if len(blocks) == 0 {
		t.Fatal("expected content blocks in forwarded second message")
	}
	content := blocks[0].ToolResultContent()
	if strings.Contains(content, "SECRET_API_KEY") {
		t.Errorf("secret was not redacted in forwarded request: %s", content)
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in forwarded content, got: %s", content)
	}
}

// TestGatewayIntegration_DenyResponse sends a request where the mock upstream returns
// a response with a tool_use block containing "rm -rf". The client should receive HTTP 400.
func TestGatewayIntegration_DenyResponse(t *testing.T) {
	// Mock upstream that returns a destructive tool_use in its response.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropic.MessagesResponse{
			ID:         "msg_e2e_deny",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-sonnet-4-20250514",
			StopReason: "tool_use",
			Content: []anthropic.ContentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_e2e_02",
					Name:  "bash",
					Input: map[string]any{"command": "rm -rf /important_data"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	proxy := newE2EProxy(t, upstream.URL)
	gw := httptest.NewServer(proxy)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Clean up some files"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Client should receive HTTP 400 with policy_denied.
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errResp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Type != "policy_denied" {
		t.Errorf("expected policy_denied error type, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "block-destructive") {
		t.Errorf("expected block-destructive in error message, got %q", errResp.Error.Message)
	}
}

// TestGatewayIntegration_Passthrough sends a GET /v1/models request.
// It verifies the request reaches the mock upstream without policy evaluation.
func TestGatewayIntegration_Passthrough(t *testing.T) {
	var receivedPath string

	// Mock upstream that serves /v1/models.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"claude-sonnet-4-20250514"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	proxy := newE2EProxy(t, upstream.URL)
	gw := httptest.NewServer(proxy)
	defer gw.Close()

	resp, err := http.Get(gw.URL + "/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify the request was forwarded to the upstream.
	if receivedPath != "/v1/models" {
		t.Errorf("expected upstream to receive /v1/models, got %q", receivedPath)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "claude-sonnet-4-20250514") {
		t.Errorf("expected model list in response, got: %s", body)
	}
}
