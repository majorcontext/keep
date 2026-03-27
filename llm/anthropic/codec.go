package anthropic

import (
	"encoding/json"
	"fmt"

	keep "github.com/majorcontext/keep"
	"github.com/majorcontext/keep/llm"
	"github.com/majorcontext/keep/sse"
)

// Codec implements [llm.Codec] for the Anthropic Messages API.
// It is stateless and safe for concurrent use.
type Codec struct{}

// NewCodec returns a new Anthropic codec.
func NewCodec() *Codec { return &Codec{} }

// requestHandle carries parsed state from DecomposeRequest to ReassembleRequest.
type requestHandle struct {
	req      *MessagesRequest
	body     []byte
	blockMap []blockPosition
	cfg      llm.DecomposeConfig
}

// responseHandle carries parsed state from DecomposeResponse to ReassembleResponse.
type responseHandle struct {
	resp     *MessagesResponse
	body     []byte
	blockMap []blockPosition
	cfg      llm.DecomposeConfig
}

// DecomposeRequest parses an Anthropic request body and returns policy calls
// plus an opaque handle for ReassembleRequest.
func (c *Codec) DecomposeRequest(body []byte, scope string, cfg llm.DecomposeConfig) ([]keep.Call, any, error) {
	var req MessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("anthropic: unmarshal request: %w", err)
	}

	calls := decomposeRequest(&req, scope, cfg)
	blockMap := walkRequestBlocks(&req, cfg)

	h := &requestHandle{
		req:      &req,
		body:     body,
		blockMap: blockMap,
		cfg:      cfg,
	}
	return calls, h, nil
}

// DecomposeResponse parses an Anthropic response body and returns policy calls
// plus an opaque handle for ReassembleResponse.
func (c *Codec) DecomposeResponse(body []byte, scope string, cfg llm.DecomposeConfig) ([]keep.Call, any, error) {
	var resp MessagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	calls := decomposeResponse(&resp, scope, cfg)
	blockMap := walkResponseBlocks(&resp, cfg)

	h := &responseHandle{
		resp:     &resp,
		body:     body,
		blockMap: blockMap,
		cfg:      cfg,
	}
	return calls, h, nil
}

// ReassembleRequest patches mutations from eval results back into the request.
// Returns the original body if no mutations are present.
func (c *Codec) ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error) {
	h, ok := handle.(*requestHandle)
	if !ok {
		return nil, fmt.Errorf("anthropic: invalid request handle type %T", handle)
	}

	blockResults := buildBlockResults(h.blockMap, results)
	if !hasMutations(blockResults) {
		return h.body, nil
	}

	patched := reassembleRequest(h.req, blockResults)
	out, err := json.Marshal(patched)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal patched request: %w", err)
	}
	return out, nil
}

// ReassembleResponse patches mutations from eval results back into the response.
// Returns the original body if no mutations are present.
func (c *Codec) ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error) {
	h, ok := handle.(*responseHandle)
	if !ok {
		return nil, fmt.Errorf("anthropic: invalid response handle type %T", handle)
	}

	blockResults := buildBlockResults(h.blockMap, results)
	if !hasMutations(blockResults) {
		return h.body, nil
	}

	patched := reassembleResponse(h.resp, blockResults)
	out, err := json.Marshal(patched)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal patched response: %w", err)
	}
	return out, nil
}

// ReassembleStream reassembles SSE events into a complete response body.
func (c *Codec) ReassembleStream(events []sse.Event) ([]byte, error) {
	resp, err := reassembleFromEvents(events)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reassemble stream: %w", err)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal stream response: %w", err)
	}
	return body, nil
}

// SynthesizeEvents creates replacement SSE events from a patched response body.
func (c *Codec) SynthesizeEvents(patchedBody []byte) ([]sse.Event, error) {
	var resp MessagesResponse
	if err := json.Unmarshal(patchedBody, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal for synthesize: %w", err)
	}
	return synthesizeEvents(&resp), nil
}

// buildBlockResults maps eval results back to block positions using CallIndex.
// The offset for summary calls is already baked into BlockPosition.CallIndex
// by WalkRequestBlocks / WalkResponseBlocks.
func buildBlockResults(blockMap []blockPosition, results []keep.EvalResult) []blockResult {
	out := make([]blockResult, 0, len(blockMap))
	for _, pos := range blockMap {
		if pos.CallIndex < 0 || pos.CallIndex >= len(results) {
			continue
		}
		out = append(out, blockResult{
			MessageIndex: pos.MessageIndex,
			BlockIndex:   pos.BlockIndex,
			Result:       results[pos.CallIndex],
		})
	}
	return out
}

// hasMutations reports whether any block result contains mutations.
func hasMutations(results []blockResult) bool {
	for _, br := range results {
		if len(br.Result.Mutations) > 0 {
			return true
		}
	}
	return false
}
