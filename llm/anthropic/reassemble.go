package anthropic

import (
	"encoding/json"
	"strings"

	keep "github.com/majorcontext/keep"
)

// blockResult pairs a decomposed call's block index with its evaluation result.
type blockResult struct {
	// MessageIndex is the index into req.Messages.
	MessageIndex int
	// BlockIndex is the index into the message's content blocks.
	BlockIndex int
	// Result is the Keep evaluation result.
	Result keep.EvalResult
}

// reassembleRequest patches redaction mutations back into an Anthropic request.
// Returns a deep copy with mutated content. The original request is not modified.
func reassembleRequest(req *MessagesRequest, results []blockResult) *MessagesRequest {
	copy := deepCopyRequest(req)

	for _, br := range results {
		if len(br.Result.Mutations) == 0 {
			continue
		}
		if br.MessageIndex < 0 || br.MessageIndex >= len(copy.Messages) {
			continue
		}

		msg := &copy.Messages[br.MessageIndex]
		blocks := msg.ContentBlocks()
		if br.BlockIndex < 0 || br.BlockIndex >= len(blocks) {
			continue
		}

		block := &blocks[br.BlockIndex]
		for _, m := range br.Result.Mutations {
			applyMutationToRequestBlock(block, m.Path, m.Replaced)
		}

		// Write the modified blocks back to the message.
		msg.Content = blocks
	}

	return copy
}

// reassembleResponse patches mutations into a response.
// Returns a deep copy with mutated content.
func reassembleResponse(resp *MessagesResponse, results []blockResult) *MessagesResponse {
	copy := deepCopyResponse(resp)

	for _, br := range results {
		if len(br.Result.Mutations) == 0 {
			continue
		}
		if br.BlockIndex < 0 || br.BlockIndex >= len(copy.Content) {
			continue
		}

		block := &copy.Content[br.BlockIndex]
		for _, m := range br.Result.Mutations {
			applyMutationToResponseBlock(block, m.Path, m.Replaced)
		}
	}

	return copy
}

// applyMutationToRequestBlock applies a single mutation to a request content block.
func applyMutationToRequestBlock(block *ContentBlock, path, replaced string) {
	switch {
	case path == "params.content" || strings.HasPrefix(path, "params.content."):
		// tool_result: replace Content field
		block.Content = replaced
	case path == "params.text":
		// text block
		block.Text = replaced
	}
}

// applyMutationToResponseBlock applies a single mutation to a response content block.
func applyMutationToResponseBlock(block *ContentBlock, path, replaced string) {
	switch {
	case path == "params.text":
		block.Text = replaced
	case strings.HasPrefix(path, "params.input."):
		// Extract the key within input: "params.input.foo" → "foo"
		key := strings.TrimPrefix(path, "params.input.")
		if key == "" {
			return
		}
		if block.Input == nil {
			block.Input = make(map[string]any)
		}
		block.Input[key] = replaced
	}
}

// deepCopyRequest returns a deep copy of a MessagesRequest.
func deepCopyRequest(req *MessagesRequest) *MessagesRequest {
	if req == nil {
		return nil
	}
	data, err := json.Marshal(req)
	if err != nil {
		// Fallback: shallow copy
		c := *req
		return &c
	}
	var out MessagesRequest
	if err := json.Unmarshal(data, &out); err != nil {
		c := *req
		return &c
	}
	return &out
}

// deepCopyResponse returns a deep copy of a MessagesResponse.
func deepCopyResponse(resp *MessagesResponse) *MessagesResponse {
	if resp == nil {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		c := *resp
		return &c
	}
	var out MessagesResponse
	if err := json.Unmarshal(data, &out); err != nil {
		c := *resp
		return &c
	}
	return &out
}
