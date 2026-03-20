package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/majorcontext/keep/internal/sse"
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
