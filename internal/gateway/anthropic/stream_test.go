package anthropic

import (
	"strings"
	"testing"

	"github.com/majorcontext/keep/internal/sse"
)

func TestReassembleFromEvents_TextOnly(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if resp.ID != "msg_01" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg_01")
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, want %q", resp.Role, "assistant")
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-20250514")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "text")
	}
	if resp.Content[0].Text != "Hello world" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello world")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", resp.Usage.OutputTokens)
	}
}

func TestReassembleFromEvents_ToolUse(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":30,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{}}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\": \"SF\"}"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("Type = %q, want %q", block.Type, "tool_use")
	}
	if block.ID != "toolu_01" {
		t.Errorf("ID = %q, want %q", block.ID, "toolu_01")
	}
	if block.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", block.Name, "get_weather")
	}
	loc, ok := block.Input["location"]
	if !ok || loc != "SF" {
		t.Errorf("Input[location] = %v, want %q", loc, "SF")
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_use")
	}
}

func TestReassembleFromEvents_MultiBlock(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_03","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":20,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_02","name":"bash","input":{}}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\": \"ls\"}"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":1}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Let me check." {
		t.Errorf("Content[0] = %+v, want text 'Let me check.'", resp.Content[0])
	}
	if resp.Content[1].Type != "tool_use" || resp.Content[1].Name != "bash" {
		t.Errorf("Content[1] = %+v, want tool_use 'bash'", resp.Content[1])
	}
	cmd, _ := resp.Content[1].Input["command"]
	if cmd != "ls" {
		t.Errorf("Input[command] = %v, want %q", cmd, "ls")
	}
}

func TestReassembleFromEvents_PingIgnored(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_04","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`},
		{Type: "ping", Data: `{"type":"ping"}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if resp.Content[0].Text != "Hi" {
		t.Errorf("Text = %q, want %q", resp.Content[0].Text, "Hi")
	}
}

func TestReassembleFromEvents_NoMessageStart(t *testing.T) {
	events := []sse.Event{
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
	}

	_, err := ReassembleFromEvents(events)
	if err == nil {
		t.Fatal("expected error for missing message_start")
	}
}

func TestReassembleFromEvents_EmptyContent(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_empty","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if len(resp.Content) != 0 {
		t.Errorf("len(Content) = %d, want 0", len(resp.Content))
	}
}

func TestReassembleFromEvents_MissingMessageStop(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{"type":"message_start","message":{"id":"msg_trunc","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
	}

	resp, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}
	if resp.Content[0].Text != "partial" {
		t.Errorf("Text = %q, want %q", resp.Content[0].Text, "partial")
	}
}

func TestReassembleFromEvents_MalformedJSON(t *testing.T) {
	events := []sse.Event{
		{Type: "message_start", Data: `{not json}`},
	}

	_, err := ReassembleFromEvents(events)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse message_start") {
		t.Errorf("error = %q, want it to mention 'parse message_start'", err.Error())
	}
}

func TestSynthesizeEvents_TextOnly(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_01",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello world"},
		},
		Usage: &Usage{InputTokens: 25, OutputTokens: 10},
	}

	events := SynthesizeEvents(resp)

	wantTypes := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(wantTypes))
	}
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}

	if strings.Contains(events[0].Data, `"stop_reason":""`) {
		t.Error("message_start should have null stop_reason, not empty string")
	}
}

func TestSynthesizeEvents_ToolUse(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_02",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "tool_use", ID: "toolu_01", Name: "bash", Input: map[string]any{"command": "ls"}},
		},
		Usage: &Usage{InputTokens: 30, OutputTokens: 15},
	}

	events := SynthesizeEvents(resp)

	if len(events) != 6 {
		t.Fatalf("len(events) = %d, want 6", len(events))
	}
	if events[2].Type != "content_block_delta" {
		t.Fatalf("events[2].Type = %q, want content_block_delta", events[2].Type)
	}
	if !strings.Contains(events[2].Data, `"partial_json"`) {
		t.Errorf("expected input_json_delta in event data, got: %s", events[2].Data)
	}
}

func TestSynthesizeEvents_MultiBlock(t *testing.T) {
	resp := &MessagesResponse{
		ID:         "msg_03",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me check."},
			{Type: "tool_use", ID: "toolu_02", Name: "bash", Input: map[string]any{"command": "ls"}},
		},
		Usage: &Usage{InputTokens: 20, OutputTokens: 20},
	}

	events := SynthesizeEvents(resp)

	// message_start + (start+delta+stop)*2 + message_delta + message_stop = 1 + 6 + 2 = 9
	if len(events) != 9 {
		t.Fatalf("len(events) = %d, want 9", len(events))
	}
}

func TestSynthesizeEvents_RoundTrip(t *testing.T) {
	original := &MessagesResponse{
		ID:         "msg_rt",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello world"},
		},
		Usage: &Usage{InputTokens: 25, OutputTokens: 10},
	}

	events := SynthesizeEvents(original)
	rebuilt, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if rebuilt.ID != original.ID {
		t.Errorf("ID = %q, want %q", rebuilt.ID, original.ID)
	}
	if rebuilt.StopReason != original.StopReason {
		t.Errorf("StopReason = %q, want %q", rebuilt.StopReason, original.StopReason)
	}
	if len(rebuilt.Content) != len(original.Content) {
		t.Fatalf("len(Content) = %d, want %d", len(rebuilt.Content), len(original.Content))
	}
	if rebuilt.Content[0].Text != original.Content[0].Text {
		t.Errorf("Text = %q, want %q", rebuilt.Content[0].Text, original.Content[0].Text)
	}
}

func TestSynthesizeEvents_ToolUseRoundTrip(t *testing.T) {
	original := &MessagesResponse{
		ID:         "msg_rt2",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: "tool_use",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me look that up."},
			{Type: "tool_use", ID: "toolu_03", Name: "get_weather", Input: map[string]any{"location": "SF"}},
		},
		Usage: &Usage{InputTokens: 30, OutputTokens: 20},
	}

	events := SynthesizeEvents(original)
	rebuilt, err := ReassembleFromEvents(events)
	if err != nil {
		t.Fatalf("ReassembleFromEvents: %v", err)
	}

	if len(rebuilt.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(rebuilt.Content))
	}
	if rebuilt.Content[0].Text != "Let me look that up." {
		t.Errorf("Content[0].Text = %q", rebuilt.Content[0].Text)
	}
	if rebuilt.Content[1].Name != "get_weather" {
		t.Errorf("Content[1].Name = %q", rebuilt.Content[1].Name)
	}
	loc, _ := rebuilt.Content[1].Input["location"]
	if loc != "SF" {
		t.Errorf("Input[location] = %v, want %q", loc, "SF")
	}
}
