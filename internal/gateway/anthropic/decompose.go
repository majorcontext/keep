package anthropic

import (
	"fmt"
	"strings"

	keep "github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/gateway/config"
)

// DecomposeRequest breaks an Anthropic Messages API request into flat Keep calls.
// Returns one llm.request summary + one call per content block (based on decompose config).
func DecomposeRequest(req *MessagesRequest, scope string, cfg config.DecomposeConfig) []keep.Call {
	var calls []keep.Call

	// Build a map from tool_use_id to tool name for resolving tool_result references.
	toolNameMap := buildToolNameMap(req.Messages)

	// 1. Summary call.
	if cfg.RequestSummaryEnabled() {
		calls = append(calls, keep.Call{
			Operation: "llm.request",
			Params: map[string]any{
				"model":             req.Model,
				"system":            systemToString(req.System),
				"token_estimate":    estimateTokens(req),
				"tool_result_count": countToolResults(req),
				"message_count":     len(req.Messages),
			},
			Context: keep.CallContext{Scope: scope, Direction: "request"},
		})
	}

	// 2. Walk messages for content blocks.
	for _, msg := range req.Messages {
		blocks := msg.ContentBlocks()
		for _, block := range blocks {
			switch block.Type {
			case "tool_result":
				if cfg.ToolResultEnabled() {
					toolName := toolNameMap[block.ToolUseID]
					calls = append(calls, keep.Call{
						Operation: "llm.tool_result",
						Params: map[string]any{
							"tool_name":   toolName,
							"tool_use_id": block.ToolUseID,
							"content":     block.ToolResultContent(),
						},
						Context: keep.CallContext{Scope: scope, Direction: "request"},
					})
				}
			case "text":
				if cfg.TextEnabled() {
					calls = append(calls, keep.Call{
						Operation: "llm.text",
						Params: map[string]any{
							"text": block.Text,
							"role": msg.Role,
						},
						Context: keep.CallContext{Scope: scope, Direction: "request"},
					})
				}
			}
		}
	}

	return calls
}

// DecomposeResponse breaks an Anthropic Messages API response into flat Keep calls.
func DecomposeResponse(resp *MessagesResponse, scope string, cfg config.DecomposeConfig) []keep.Call {
	var calls []keep.Call

	// 1. Summary call.
	if cfg.ResponseSummaryEnabled() {
		calls = append(calls, keep.Call{
			Operation: "llm.response",
			Params: map[string]any{
				"stop_reason":    resp.StopReason,
				"tool_use_count": countToolUses(resp),
			},
			Context: keep.CallContext{Scope: scope, Direction: "response"},
		})
	}

	// 2. Walk response content blocks.
	for _, block := range resp.Content {
		switch block.Type {
		case "tool_use":
			if cfg.ToolUseEnabled() {
				calls = append(calls, keep.Call{
					Operation: "llm.tool_use",
					Params: map[string]any{
						"name":  block.Name,
						"input": block.Input,
					},
					Context: keep.CallContext{Scope: scope, Direction: "response"},
				})
			}
		case "text":
			if cfg.TextEnabled() {
				calls = append(calls, keep.Call{
					Operation: "llm.text",
					Params: map[string]any{
						"text": block.Text,
						"role": resp.Role,
					},
					Context: keep.CallContext{Scope: scope, Direction: "response"},
				})
			}
		}
	}

	return calls
}

// systemToString converts the system field (string or []ContentBlock) to a string.
func systemToString(system any) string {
	if system == nil {
		return ""
	}
	switch v := system.(type) {
	case string:
		return v
	default:
		blocks := toContentBlocks(system)
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
}

// estimateTokens gives a rough token count for the request.
// Uses a simple heuristic of ~4 characters per token.
func estimateTokens(req *MessagesRequest) int {
	total := len(systemToString(req.System))
	for _, msg := range req.Messages {
		blocks := msg.ContentBlocks()
		for _, b := range blocks {
			switch b.Type {
			case "text":
				total += len(b.Text)
			case "tool_result":
				total += len(b.ToolResultContent())
			case "tool_use":
				total += len(fmt.Sprintf("%v", b.Input))
			}
		}
	}
	return total / 4
}

// countToolResults counts tool_result blocks in the request.
func countToolResults(req *MessagesRequest) int {
	count := 0
	for _, msg := range req.Messages {
		for _, b := range msg.ContentBlocks() {
			if b.Type == "tool_result" {
				count++
			}
		}
	}
	return count
}

// countToolUses counts tool_use blocks in the response.
func countToolUses(resp *MessagesResponse) int {
	count := 0
	for _, b := range resp.Content {
		if b.Type == "tool_use" {
			count++
		}
	}
	return count
}

// buildToolNameMap creates a map from tool_use_id to tool name
// by walking all messages and finding tool_use blocks.
func buildToolNameMap(messages []Message) map[string]string {
	m := make(map[string]string)
	for _, msg := range messages {
		for _, b := range msg.ContentBlocks() {
			if b.Type == "tool_use" && b.ID != "" {
				m[b.ID] = b.Name
			}
		}
	}
	return m
}
