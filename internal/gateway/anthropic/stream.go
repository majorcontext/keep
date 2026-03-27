package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/majorcontext/keep/sse"
)

// ReassembleFromEvents builds a MessagesResponse from a sequence of Anthropic
// streaming SSE events. It accumulates text deltas and input_json_delta fragments
// into complete content blocks.
func ReassembleFromEvents(events []sse.Event) (*MessagesResponse, error) {
	var resp *MessagesResponse
	var blocks []ContentBlock
	inputJSON := make(map[int]*strings.Builder)
	textBuf := make(map[int]*strings.Builder)

	for _, ev := range events {
		switch ev.Type {
		case "message_start":
			var envelope struct {
				Message MessagesResponse `json:"message"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &envelope); err != nil {
				return nil, fmt.Errorf("parse message_start: %w", err)
			}
			resp = &envelope.Message

		case "content_block_start":
			var payload struct {
				Index        int          `json:"index"`
				ContentBlock ContentBlock `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse content_block_start: %w", err)
			}
			for len(blocks) <= payload.Index {
				blocks = append(blocks, ContentBlock{})
			}
			blocks[payload.Index] = payload.ContentBlock
			if payload.ContentBlock.Type == "tool_use" {
				inputJSON[payload.Index] = &strings.Builder{}
			}
			if payload.ContentBlock.Type == "text" {
				textBuf[payload.Index] = &strings.Builder{}
			}

		case "content_block_delta":
			var payload struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse content_block_delta: %w", err)
			}
			switch payload.Delta.Type {
			case "text_delta":
				if buf, ok := textBuf[payload.Index]; ok {
					buf.WriteString(payload.Delta.Text)
				}
			case "input_json_delta":
				if buf, ok := inputJSON[payload.Index]; ok {
					buf.WriteString(payload.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			// No action needed.

		case "message_delta":
			var payload struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage *Usage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
				return nil, fmt.Errorf("parse message_delta: %w", err)
			}
			if resp != nil {
				resp.StopReason = payload.Delta.StopReason
				if payload.Usage != nil {
					if resp.Usage == nil {
						resp.Usage = &Usage{}
					}
					resp.Usage.OutputTokens = payload.Usage.OutputTokens
				}
			}

		case "message_stop", "ping":
			// No action needed.
		}
	}

	if resp == nil {
		return nil, errors.New("no message_start event found")
	}

	for idx, buf := range textBuf {
		if idx < len(blocks) {
			blocks[idx].Text = buf.String()
		}
	}

	for idx, buf := range inputJSON {
		if idx < len(blocks) {
			raw := buf.String()
			if raw != "" {
				var input map[string]any
				if err := json.Unmarshal([]byte(raw), &input); err != nil {
					return nil, fmt.Errorf("parse tool_use input at index %d: %w", idx, err)
				}
				blocks[idx].Input = input
			}
		}
	}

	resp.Content = blocks
	return resp, nil
}

// SynthesizeEvents produces a minimal valid Anthropic SSE event sequence
// from a MessagesResponse. Each content block gets one start + one delta + one stop.
// This is used when the response was redacted and the original events can't be replayed.
func SynthesizeEvents(resp *MessagesResponse) []sse.Event {
	var events []sse.Event

	// message_start: use a raw map so stop_reason serializes as null (not "").
	startMsg := map[string]any{
		"id":          resp.ID,
		"type":        resp.Type,
		"role":        resp.Role,
		"model":       resp.Model,
		"content":     []any{},
		"stop_reason": nil,
	}
	if resp.Usage != nil {
		startMsg["usage"] = map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": 0,
		}
	}
	startData, _ := json.Marshal(map[string]any{
		"type":    "message_start",
		"message": startMsg,
	})
	events = append(events, sse.Event{Type: "message_start", Data: string(startData)})

	// Per content block: start + delta + stop.
	for i, block := range resp.Content {
		// content_block_start
		var startBlock any
		switch block.Type {
		case "tool_use":
			startBlock = map[string]any{
				"type":  "tool_use",
				"id":    block.ID,
				"name":  block.Name,
				"input": map[string]any{},
			}
		case "text":
			startBlock = map[string]any{
				"type": "text",
				"text": "",
			}
		default:
			startBlock = map[string]any{"type": block.Type}
		}
		blockStartData, _ := json.Marshal(map[string]any{
			"type":          "content_block_start",
			"index":         i,
			"content_block": startBlock,
		})
		events = append(events, sse.Event{Type: "content_block_start", Data: string(blockStartData)})

		// content_block_delta
		var deltaData []byte
		switch block.Type {
		case "text":
			deltaData, _ = json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": i,
				"delta": map[string]any{
					"type": "text_delta",
					"text": block.Text,
				},
			})
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			deltaData, _ = json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": i,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": string(inputJSON),
				},
			})
		}
		events = append(events, sse.Event{Type: "content_block_delta", Data: string(deltaData)})

		// content_block_stop
		stopData, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": i,
		})
		events = append(events, sse.Event{Type: "content_block_stop", Data: string(stopData)})
	}

	// message_delta
	msgDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": resp.StopReason,
		},
	}
	if resp.Usage != nil {
		msgDelta["usage"] = map[string]any{
			"output_tokens": resp.Usage.OutputTokens,
		}
	}
	msgDeltaData, _ := json.Marshal(msgDelta)
	events = append(events, sse.Event{Type: "message_delta", Data: string(msgDeltaData)})

	// message_stop
	stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
	events = append(events, sse.Event{Type: "message_stop", Data: string(stopData)})

	return events
}
