package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerboseWriter_RequestRaw_ShowsMessagesOnly(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	body := `{"model":"claude-haiku-4-5-20251001","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello world"}]}]}`
	v.RequestRaw([]byte(body))

	out := buf.String()
	// Header should show metadata
	if !strings.Contains(out, "model=claude-haiku-4-5-20251001") {
		t.Errorf("expected model in header, got: %s", out)
	}
	if !strings.Contains(out, "stream=true") {
		t.Errorf("expected stream in header, got: %s", out)
	}
	// Body should show messages structure
	if !strings.Contains(out, "\"role\": \"user\"") {
		t.Errorf("expected messages content, got: %s", out)
	}
	if !strings.Contains(out, "\"text\": \"hello world\"") {
		t.Errorf("expected text content, got: %s", out)
	}
}

func TestVerboseWriter_TruncatesStringValues(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 20)

	body := `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"This is a very long message that should be truncated at the string level"}]}]}`
	v.RequestRaw([]byte(body))

	out := buf.String()
	// Short strings preserved
	if !strings.Contains(out, "\"role\": \"user\"") {
		t.Errorf("expected short string preserved, got: %s", out)
	}
	// Long string truncated
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation marker …, got: %s", out)
	}
	if strings.Contains(out, "should be truncated at the string level") {
		t.Errorf("expected string value truncated, but full text appeared")
	}
}

func TestVerboseWriter_FullMode_NoTruncation(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	longText := strings.Repeat("x", 500)
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"` + longText + `"}]}]}`
	v.RequestRaw([]byte(body))

	out := buf.String()
	if !strings.Contains(out, longText) {
		t.Errorf("expected full text in output with no truncation")
	}
}

func TestVerboseWriter_ToolCallStructure(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	body := `{"model":"claude-haiku-4-5-20251001","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_01ABC","name":"bash","input":{"command":"echo hello"}}]}`
	v.ResponseRaw([]byte(body))

	out := buf.String()
	// Header should show response metadata
	if !strings.Contains(out, "model=claude-haiku-4-5-20251001") {
		t.Errorf("expected model in header, got: %s", out)
	}
	if !strings.Contains(out, "stop_reason=tool_use") {
		t.Errorf("expected stop_reason in header, got: %s", out)
	}
	// Content should show tool call structure
	for _, key := range []string{"tool_use", "toolu_01ABC", "bash", "command", "echo hello"} {
		if !strings.Contains(out, key) {
			t.Errorf("expected %q in output, got: %s", key, out)
		}
	}
}

func TestVerboseWriter_RequestDenied(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	v.RequestDenied("block-network", "Network access blocked")

	out := buf.String()
	if !strings.Contains(out, "REQUEST DENIED") {
		t.Errorf("expected DENIED header, got: %s", out)
	}
	if !strings.Contains(out, "block-network") {
		t.Errorf("expected rule name, got: %s", out)
	}
}

func TestVerboseWriter_RequestAfterPolicy(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	body := `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"[REDACTED:aws-access-token]"}]}]}`
	v.RequestAfterPolicy([]byte(body), "redact-secrets")

	out := buf.String()
	if !strings.Contains(out, "after policy: redact-secrets") {
		t.Errorf("expected policy label, got: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:aws-access-token]") {
		t.Errorf("expected redacted content visible, got: %s", out)
	}
}

func TestVerboseWriter_ResponseAllowed(t *testing.T) {
	var buf bytes.Buffer
	v := NewVerboseWriter(&buf, 0)

	v.ResponseAllowed()

	out := buf.String()
	if !strings.Contains(out, "RESPONSE (policy: allow)") {
		t.Errorf("expected allow response, got: %s", out)
	}
}
