// Package anthropic implements the llm.Codec interface for the
// Anthropic Messages API (https://docs.anthropic.com/en/api/messages).
//
// It decomposes Messages API requests and responses into keep.Call
// objects for policy evaluation, and reassembles mutations back into
// the wire format. It also handles SSE stream reassembly and event
// synthesis for streaming responses.
//
// This package is used internally by the gateway proxy and is also
// available to library consumers via the NewCodec constructor:
//
//	codec := anthropic.NewCodec()
//	result, err := llm.EvaluateRequest(engine, codec, body, scope, cfg)
package anthropic
