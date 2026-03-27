package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
	"github.com/majorcontext/keep/llm/anthropic"
	"github.com/majorcontext/keep/sse"
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
	textEnabled := true
	cfg := &gwconfig.GatewayConfig{
		Listen:   ":0",
		RulesDir: "testdata/rules",
		Provider: "anthropic",
		Upstream: upstreamURL,
		Scope:    "test-gateway",
		Decompose: gwconfig.DecomposeConfig{
			Text: &textEnabled,
		},
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

// sseUpstream returns a test server that responds with SSE events for a simple text response.
func sseUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "req_stream_123")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from \"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"upstream\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

// destructiveSSEUpstream returns a test server that streams a destructive tool_use via SSE.
func destructiveSSEUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_deny\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"bash\",\"input\":{}}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\": \\\"rm -rf /\\\"}\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":15}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "claude-sonnet-4-20250514") {
		t.Errorf("expected model list in response, got: %s", body)
	}
}

func TestProxy_StreamingAllowed(t *testing.T) {
	upstream := sseUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	reader := sse.NewReader(resp.Body)
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		events = append(events, ev)
	}

	// sseUpstream sends 7 events. Verify we get all 7 (original replay, not synthesis).
	if len(events) != 7 {
		t.Fatalf("expected 7 SSE events (original replay), got %d", len(events))
	}

	assembled, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if assembled.Content[0].Text != "Hello from upstream" {
		t.Errorf("Text = %q, want %q", assembled.Content[0].Text, "Hello from upstream")
	}
}

func TestProxy_StreamingDenyResponse(t *testing.T) {
	upstream := destructiveSSEUpstream()
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Do something"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
		t.Errorf("expected block-destructive in message, got %q", errResp.Error.Message)
	}
}

func TestProxy_StreamingRedactResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_redact\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"The key is SECRET_API_KEY_VALUE\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Show me the key"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	reader := sse.NewReader(resp.Body)
	var events []sse.Event
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		events = append(events, ev)
	}

	assembled, err := anthropic.ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	text := assembled.Content[0].Text
	if strings.Contains(text, "SECRET_API_KEY_VALUE") {
		t.Errorf("secret was not redacted: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in text, got: %s", text)
	}
}

func TestProxy_OversizedUpstreamResponse(t *testing.T) {
	// Upstream that returns a body larger than maxResponseBodySize.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write a response that exceeds 16 MB. The gateway should truncate it via LimitReader,
		// causing a JSON parse failure, which results in the truncated body being passed through.
		w.WriteHeader(http.StatusOK)
		// Write a valid JSON start, then pad with data to exceed the limit.
		_, _ = w.Write([]byte(`{"id":"msg_big","type":"message","role":"assistant","content":[{"type":"text","text":"`))
		// Write 17 MB of padding
		chunk := bytes.Repeat([]byte("x"), 1024*1024)
		for i := 0; i < 17; i++ {
			_, _ = w.Write(chunk)
		}
		_, _ = w.Write([]byte(`"}],"model":"claude-sonnet-4-20250514","stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
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
	defer func() { _ = resp.Body.Close() }()

	// The response was truncated at 16 MB, so JSON unmarshal will fail.
	// The gateway should still return a response (pass-through of truncated body).
	// We just verify the gateway didn't crash and returned some response.
	if resp.StatusCode != http.StatusOK {
		t.Logf("got status %d (acceptable — oversized response handling)", resp.StatusCode)
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
	defer func() { _ = resp.Body.Close() }()

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

func TestProxy_StreamingMidStreamFailure(t *testing.T) {
	// Upstream sends some SSE events then closes the connection without message_stop.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send message_start and one content delta, then abruptly stop (no message_stop).
		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_cut\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial output\"}}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		// Connection closes here — no content_block_stop, no message_delta, no message_stop.
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The proxy should still return a response. Since message_start was received,
	// reassembly may succeed with partial data, or the proxy may return an error.
	// Either way, the gateway must not panic and must return a valid HTTP response.
	body, _ := io.ReadAll(resp.Body)

	// The proxy should return 200 with streamed events (partial reassembly succeeds
	// because message_start was present), or 500 if reassembly detects the incomplete stream.
	switch resp.StatusCode {
	case http.StatusOK:
		// If 200, verify the response is SSE with the partial content.
		ct := resp.Header.Get("Content-Type")
		if ct != "text/event-stream" {
			t.Errorf("Content-Type = %q, want text/event-stream", ct)
		}
	case http.StatusInternalServerError:
		// 500 is also acceptable — the proxy detected the incomplete stream.
		var errResp policyError
		if err := json.Unmarshal(body, &errResp); err != nil {
			t.Fatalf("expected JSON error body, got: %s", body)
		}
		if errResp.Error.Type != "internal_error" {
			t.Errorf("expected internal_error, got %s", errResp.Error.Type)
		}
	default:
		t.Fatalf("expected 200 or 500, got %d: %s", resp.StatusCode, body)
	}
}

func TestProxy_StreamingMalformedEvents(t *testing.T) {
	// Upstream sends SSE events with invalid JSON data that can't be reassembled.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			// Valid message_start.
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_bad\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			// Malformed content_block_start — invalid JSON.
			"event: content_block_start\ndata: {{{not valid json}}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// ReassembleFromEvents should fail on the malformed content_block_start event,
	// causing the proxy to return a 500 internal error.
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Type != "internal_error" {
		t.Errorf("expected internal_error, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "stream policy evaluation error") {
		t.Errorf("expected stream policy evaluation error message, got %q", errResp.Error.Message)
	}
}

func TestProxy_StreamingTooManyEvents(t *testing.T) {
	// Upstream sends more than maxSSEEvents (10000) events.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Start with a valid message_start.
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_flood\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Flood with 10001 content_block_delta events to exceed maxSSEEvents.
		delta := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n"
		for i := 0; i < 10001; i++ {
			_, _ = w.Write([]byte(delta))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// message_stop would come here but the proxy should bail out before reaching it.
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, nil)
	gw := httptest.NewServer(p)
	defer gw.Close()

	reqBody := anthropic.MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(gw.URL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The proxy should detect the event flood and return a 500 error.
	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Type != "internal_error" {
		t.Errorf("expected internal_error, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "too large") {
		t.Errorf("expected 'too large' in error message, got %q", errResp.Error.Message)
	}
}

// TestProxy_UpstreamConnectionRefused verifies that when the upstream is
// unreachable, the gateway returns a 500 internal error.
func TestProxy_UpstreamConnectionRefused(t *testing.T) {
	// Use RFC 5737 TEST-NET-1 (192.0.2.0/24) which is non-routable,
	// avoiding TOCTOU races from binding and immediately closing a port.
	p := newTestProxy(t, "http://192.0.2.1:1", nil)
	// Use a short client timeout so the test doesn't wait for a full TCP timeout.
	p.client.Timeout = 500 * time.Millisecond
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
		t.Fatalf("request to gateway failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Type != "internal_error" {
		t.Errorf("expected internal_error, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "upstream") {
		t.Errorf("expected 'upstream' in error message, got %q", errResp.Error.Message)
	}
}

// TestProxy_UpstreamTimeout verifies that when the upstream hangs beyond the
// client timeout, the gateway returns a 500 internal error.
func TestProxy_UpstreamTimeout(t *testing.T) {
	// Upstream that blocks for a long time.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than our custom short client timeout.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Create proxy with a short client timeout to avoid a slow test.
	eng := newTestEngine(t)
	textEnabled := true
	cfg := &gwconfig.GatewayConfig{
		Listen:   ":0",
		RulesDir: "testdata/rules",
		Provider: "anthropic",
		Upstream: upstream.URL,
		Scope:    "test-gateway",
		Decompose: gwconfig.DecomposeConfig{
			Text: &textEnabled,
		},
	}
	p, err := NewProxy(eng, cfg, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	// Override the client with a short timeout.
	p.client = &http.Client{Timeout: 100 * time.Millisecond}

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
		t.Fatalf("request to gateway failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, body)
	}

	var errResp policyError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Type != "internal_error" {
		t.Errorf("expected internal_error, got %s", errResp.Error.Type)
	}
	if !strings.Contains(errResp.Error.Message, "upstream") {
		t.Errorf("expected 'upstream' in error message, got %q", errResp.Error.Message)
	}
}

// TestProxy_UpstreamNon2xxPassthrough verifies that non-2xx responses from the
// upstream (e.g. 429 rate limit, 500 server error) are passed through to the
// client as-is, preserving status code and body.
func TestProxy_UpstreamNon2xxPassthrough(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("retry-after", "30")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer upstream.Close()

			p := newTestProxy(t, upstream.URL, nil)
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
				t.Fatalf("request to gateway failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.statusCode {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status %d, got %d: %s", tt.statusCode, resp.StatusCode, body)
			}

			respBody, _ := io.ReadAll(resp.Body)
			if string(respBody) != tt.body {
				t.Errorf("expected body %q, got %q", tt.body, string(respBody))
			}

			// Verify headers are passed through.
			if ra := resp.Header.Get("retry-after"); ra != "30" {
				t.Errorf("expected retry-after header '30', got %q", ra)
			}
		})
	}
}
