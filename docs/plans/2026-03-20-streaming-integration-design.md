# Streaming Support in LLM Gateway — Design

**Date:** 2026-03-20
**Status:** Approved

## Context

The LLM gateway currently rejects `stream: true` requests with HTTP 400. With the `internal/sse` package now providing SSE Reader/Writer primitives, we can implement buffer-then-replay streaming: forward `stream: true` to Anthropic, buffer all SSE events, reassemble into a `MessagesResponse` for policy evaluation, then replay (or synthesize) events back to the client.

## Data Flow

```
Client (stream:true) → Proxy
  1. Parse request, evaluate request-side policy (same as non-streaming)
  2. Forward to Anthropic with stream:true
  3. Buffer all SSE events from upstream using sse.Reader
  4. Reassemble buffered events into MessagesResponse
  5. Evaluate response-side policy on the assembled response
  6. If denied → return JSON error (no SSE sent yet)
  7. If redacted → patch response, synthesize new SSE events, stream to client
  8. If clean → replay original buffered events to client
```

## Components

### `internal/gateway/anthropic/stream.go`

Two functions:

**`ReassembleFromEvents(events []sse.Event) (*MessagesResponse, error)`**

Parses the JSON in each event's Data field, walks the event sequence (`message_start` → `content_block_start` → `content_block_delta` → `content_block_stop` → `message_delta` → `message_stop`), and builds a complete `MessagesResponse`. Accumulates text deltas per content block, merges tool_use `input_json_delta` events.

**`SynthesizeEvents(resp *MessagesResponse) []sse.Event`**

Takes a `MessagesResponse` and produces the minimal valid SSE event sequence:
- `message_start` — skeleton message with empty content
- Per content block: `content_block_start` + single `content_block_delta` + `content_block_stop`
- `message_delta` with stop_reason and usage
- `message_stop`

Each delta carries the full text/input in one chunk. This is valid — there's no requirement that deltas be small. The client already waited for buffering, so simulating gradual streaming is pointless.

### `internal/gateway/proxy.go`

After request-side policy evaluation (steps 1-6), if `req.Stream` is true, call `p.handleStreamingResponse(w, r, upstreamReq)` instead of the current non-streaming response path.

`handleStreamingResponse`:
1. Sends request to upstream (with `stream: true` preserved)
2. Reads upstream SSE stream via `sse.Reader`, buffers all events
3. Calls `ReassembleFromEvents` to build `MessagesResponse`
4. Runs existing response policy evaluation (decompose → evaluate → check deny/redact)
5. If denied: returns JSON error (no SSE sent yet)
6. If redacted: patches response via `ReassembleResponse`, calls `SynthesizeEvents`, streams synthesized events via `sse.Writer`
7. If clean: replays original buffered events via `sse.Writer`

Remove the current `stream: true` rejection block.

## Error Handling

- Upstream returns non-2xx with SSE content-type: buffer and pass through as-is
- Upstream returns non-2xx with JSON: pass through as-is (existing behavior)
- SSE parsing or reassembly fails: return 502 JSON error
- Policy denies: return 400 JSON error (no SSE sent yet)
- Policy redacts: synthesize and stream new events

## Response Redaction in Streaming

When policy evaluation produces redactions:
1. Patch the assembled `MessagesResponse` using existing `ReassembleResponse`
2. Call `SynthesizeEvents` on the patched response to produce new SSE events
3. Stream synthesized events to client

When no redactions, replay the original buffered events for maximum fidelity.

## Testing

### `internal/gateway/anthropic/stream_test.go`

- `TestReassembleFromEvents` — Table-driven: canned SSE events → verify assembled `MessagesResponse`. Cover: text-only, tool_use, multi-block, multi-delta accumulation, input_json_delta merging.
- `TestSynthesizeEvents` — Round-trip: synthesize from `MessagesResponse`, reassemble back, verify match.
- Edge cases: empty content, missing message_stop, malformed JSON.

### `internal/gateway/proxy_test.go`

- Replace `TestProxy_StreamingRejected` with `TestProxy_StreamingAllowed`
- `TestProxy_StreamingDenyResponse` — Destructive tool_use via SSE → JSON error
- `TestProxy_StreamingRedactResponse` — Secret in SSE text → synthesized SSE with redaction
- Mock upstream that serves canned SSE event streams

## Files

| File | Change |
|------|--------|
| `internal/gateway/anthropic/stream.go` | New: `ReassembleFromEvents`, `SynthesizeEvents` |
| `internal/gateway/anthropic/stream_test.go` | New: tests for reassembly and synthesis |
| `internal/gateway/proxy.go` | Modify: branch to streaming, remove rejection |
| `internal/gateway/proxy_test.go` | Modify: update/add streaming tests |
