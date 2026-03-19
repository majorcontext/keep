# SSE Primitive Package Design

**Date:** 2026-03-19
**Package:** `internal/sse`
**Status:** Approved

## Context

The Keep LLM gateway currently rejects streaming requests (`stream: true`) with HTTP 400. To support streaming, we need to buffer-then-replay SSE events: read the full upstream SSE stream, allow the policy engine to evaluate the complete message, then replay the buffered events to the client.

This design covers the low-level SSE protocol primitive. Anthropic-specific logic (event type interpretation, message reassembly from buffered events) is out of scope and belongs in `internal/gateway/anthropic/`.

## Approach: Buffer-then-Replay (Option B)

1. Client sends `stream: true`
2. Proxy forwards `stream: true` to Anthropic
3. Proxy buffers all SSE events using `Reader`, assembles the complete message
4. Policy engine evaluates the complete message
5. Proxy replays buffered events to the client using `Writer` (or returns error)

The `internal/sse` package provides the Reader and Writer. The caller (proxy) manages buffering as a simple `[]Event` slice.

## API Surface

### Event

```go
type Event struct {
    Type  string // "event:" field; empty string means unnamed event
    Data  string // "data:" field; multiple data lines joined with "\n"
    ID    string // "id:" field
    Retry int    // "retry:" field in milliseconds; 0 means not set
}
```

### Reader

```go
type Reader struct { /* internal: wraps bufio.Scanner or similar */ }

func NewReader(r io.Reader) *Reader

// Next returns the next event from the stream.
// Returns io.EOF when the stream ends cleanly.
func (r *Reader) Next() (Event, error)
```

Parsing rules (per the SSE spec):
- Lines starting with `:` are comments and are skipped
- Blank lines delimit events
- `event:`, `data:`, `id:`, `retry:` are recognized field names
- Multiple `data:` lines within one event are joined with `\n`
- Space after the colon is optional and stripped if present
- Fields with no colon are ignored
- An event block with no fields set is skipped (no empty Event returned)
- `retry:` values that are not valid integers are ignored

### Writer

```go
type Writer struct { /* internal: wraps http.ResponseWriter */ }

func NewWriter(w http.ResponseWriter) *Writer

// SetHeaders sets Content-Type: text/event-stream, Cache-Control: no-cache,
// and Connection: keep-alive on the underlying ResponseWriter.
func (w *Writer) SetHeaders()

// WriteEvent writes a single event in SSE wire format and flushes.
// Only non-zero fields are written.
// Multi-line Data is split into multiple "data:" lines.
// Returns an error if the underlying writer fails or does not support flushing.
func (w *Writer) WriteEvent(e Event) error
```

## What Is NOT in This Package

- Anthropic event types, JSON parsing, or message reassembly
- Buffer/replay orchestration (caller manages `[]Event`)
- HTTP client code (caller reads `resp.Body`)
- Reconnection or `Last-Event-ID` logic

## Testing Strategy

**Reader tests** (table-driven):
- Single event with type and data
- Multi-line data (multiple `data:` lines joined with `\n`)
- Comment lines skipped
- Empty/unnamed event type
- `id:` and `retry:` fields
- Space handling after colon (with and without)
- Malformed lines (no colon) ignored
- EOF mid-event (partial event discarded or returned depending on fields set)
- Multiple events in one stream
- Event blocks with no fields set are skipped

**Writer tests** (output capture):
- Single event with all fields
- Event with only data (no type/id/retry)
- Multi-line data split into multiple `data:` lines
- Flush called after each event

**Round-trip test**:
- Write events with Writer, parse back with Reader, assert equality

## File Layout

```
internal/sse/
  sse.go          # Event type
  reader.go       # Reader implementation
  reader_test.go  # Reader tests
  writer.go       # Writer implementation
  writer_test.go  # Writer tests
```
