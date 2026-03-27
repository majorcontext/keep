package llm

import (
	"encoding/json"
	"testing"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// mockCodec is a test double that returns predetermined calls.
type mockCodec struct {
	calls       []keep.Call
	body        []byte
	synthEvents []sse.Event
}

func (m *mockCodec) DecomposeRequest(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error) {
	return m.calls, body, nil
}

func (m *mockCodec) DecomposeResponse(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error) {
	return m.calls, body, nil
}

func (m *mockCodec) ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error) {
	return handle.([]byte), nil
}

func (m *mockCodec) ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error) {
	return handle.([]byte), nil
}

func (m *mockCodec) ReassembleStream(events []sse.Event) ([]byte, error) {
	return m.body, nil
}

func (m *mockCodec) SynthesizeEvents(patchedBody []byte) ([]sse.Event, error) {
	return m.synthEvents, nil
}

func TestEvaluateRequest_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: allow-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.request", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateRequest(engine, codec, []byte(`{}`), "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
	if len(result.Audits) != 1 {
		t.Errorf("got %d audits, want 1", len(result.Audits))
	}
}

func TestEvaluateRequest_Deny(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: deny-all
    match: {operation: "*"}
    action: deny
    message: blocked
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.request", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateRequest(engine, codec, []byte(`{}`), "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Deny {
		t.Errorf("got decision %q, want deny", result.Decision)
	}
	if result.Rule != "deny-all" {
		t.Errorf("got rule %q, want deny-all", result.Rule)
	}
	if result.Body != nil {
		t.Error("deny result should have nil body")
	}
}

func TestEvaluateResponse_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	body := []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[]}`)
	codec := &mockCodec{
		calls: []keep.Call{
			{Operation: "llm.response", Context: keep.CallContext{Scope: "test"}},
		},
	}

	result, err := EvaluateResponse(engine, codec, body, "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
}

func TestEvaluateStream_Allow(t *testing.T) {
	rules := []byte(`
scope: test
mode: enforce
rules:
  - name: log-all
    match: {operation: "*"}
    action: log
`)
	engine, err := keep.LoadFromBytes(rules)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	events := []sse.Event{
		{Type: "message_start", Data: "{}"},
		{Type: "message_stop", Data: "{}"},
	}
	respBody, _ := json.Marshal(map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "content": []any{}})
	codec := &mockCodec{
		calls: []keep.Call{{Operation: "llm.response", Context: keep.CallContext{Scope: "test"}}},
		body:  respBody,
	}

	result, err := EvaluateStream(engine, codec, events, "test", DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != keep.Allow {
		t.Errorf("got decision %q, want allow", result.Decision)
	}
	if len(result.Events) != 2 {
		t.Errorf("got %d events, want 2 (original)", len(result.Events))
	}
}
