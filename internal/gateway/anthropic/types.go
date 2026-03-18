package anthropic

import (
	"encoding/json"
	"fmt"
	"log"
)

// MessagesRequest is the Anthropic /v1/messages request body.
type MessagesRequest struct {
	Model     string    `json:"model"`
	System    any       `json:"system,omitempty"`   // string or []ContentBlock
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
	Tools     any       `json:"tools,omitempty"`    // passthrough
	Metadata  any       `json:"metadata,omitempty"` // passthrough
}

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content any    `json:"content"` // string or []ContentBlock
}

// ContentBlocks extracts content blocks from a Message.
// If Content is a string, it returns a single text block.
// If Content is a []ContentBlock (or a []any from JSON unmarshal), it returns the parsed blocks.
func (m *Message) ContentBlocks() []ContentBlock {
	return toContentBlocks(m.Content)
}

// ContentBlock represents a single content block in a message or response.
type ContentBlock struct {
	Type      string         `json:"type"`                    // "text", "tool_use", "tool_result", "image"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`            // tool_use ID
	Name      string         `json:"name,omitempty"`          // tool_use name
	Input     map[string]any `json:"input,omitempty"`         // tool_use input
	ToolUseID string         `json:"tool_use_id,omitempty"`   // tool_result reference to tool_use ID
	Content   any            `json:"content,omitempty"`       // tool_result content: string or []ContentBlock
	IsError   bool           `json:"is_error,omitempty"`      // tool_result error flag
}

// ToolResultContent returns the text content of a tool_result block.
// It handles both string and array-of-blocks content formats.
func (b *ContentBlock) ToolResultContent() string {
	if b.Content == nil {
		return ""
	}
	switch v := b.Content.(type) {
	case string:
		return v
	default:
		blocks := toContentBlocks(b.Content)
		for _, blk := range blocks {
			if blk.Type == "text" {
				return blk.Text
			}
		}
		return ""
	}
}

// MessagesResponse is the Anthropic /v1/messages response body.
type MessagesResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`        // "message"
	Role       string         `json:"role"`        // "assistant"
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"` // "end_turn", "tool_use", etc.
	Usage      *Usage         `json:"usage,omitempty"`
}

// Usage tracks token consumption for a request/response pair.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// toContentBlocks converts a value that may be a string, []ContentBlock,
// or []any (as produced by JSON unmarshal into an any field) into []ContentBlock.
func toContentBlocks(v any) []ContentBlock {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		return []ContentBlock{{Type: "text", Text: val}}
	case []ContentBlock:
		return val
	case []any:
		blocks := make([]ContentBlock, 0, len(val))
		for _, item := range val {
			if b, ok := item.(map[string]any); ok {
				block, err := mapToContentBlock(b)
				if err != nil {
					// Log and skip malformed blocks rather than silently including empty ones
					log.Printf("warning: skipping malformed content block: %v", err)
					continue
				}
				blocks = append(blocks, block)
			}
		}
		return blocks
	default:
		// Fallback: round-trip through JSON.
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var blocks []ContentBlock
		if err := json.Unmarshal(data, &blocks); err != nil {
			return nil
		}
		return blocks
	}
}

// mapToContentBlock converts a map[string]any (from JSON decode into any) to a ContentBlock.
func mapToContentBlock(m map[string]any) (ContentBlock, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("marshal content block: %w", err)
	}
	var block ContentBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return ContentBlock{}, fmt.Errorf("unmarshal content block: %w", err)
	}
	return block, nil
}
