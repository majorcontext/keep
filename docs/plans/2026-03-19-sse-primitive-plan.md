# SSE Primitive Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a spec-compliant SSE Reader and Writer in `internal/sse/` for the gateway's buffer-then-replay streaming support.

**Architecture:** Three files — `sse.go` (Event type), `reader.go` (line-by-line SSE parser using `bufio.Scanner`), `writer.go` (SSE formatter with `http.Flusher` support). No Anthropic-specific logic. Caller manages buffering as `[]Event`.

**Tech Stack:** Go stdlib only (`bufio`, `fmt`, `io`, `net/http`, `strconv`, `strings`)

**Spec:** `docs/plans/2026-03-19-sse-primitive-design.md`

**Note on Reader/Writer asymmetry:** The Writer skips empty-string Data (can't distinguish zero-value from "set to empty" with a plain string). The Reader _can_ produce `Event{Data:""}` from `data: \n\n`. This means round-tripping events with empty Data is lossy. This is acceptable — empty-data events are uncommon in practice and the alternative (always emitting `data:` for zero-value) would produce noisy output for events that only carry Type/ID/Retry.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/sse/sse.go` | `Event` struct definition and package doc |
| `internal/sse/reader.go` | `Reader` type, `NewReader`, `Next` method — SSE wire format parser |
| `internal/sse/reader_test.go` | Table-driven tests for all Reader parsing rules |
| `internal/sse/writer.go` | `Writer` type, `NewWriter`, `SetHeaders`, `WriteEvent` — SSE wire format writer |
| `internal/sse/writer_test.go` | Writer output tests and round-trip test |

---

### Task 1: Event Type

**Files:**
- Create: `internal/sse/sse.go`

- [ ] **Step 1: Create the Event type**

```go
// Package sse implements Server-Sent Events parsing and writing
// per the WHATWG spec (https://html.spec.whatwg.org/multipage/server-sent-events.html).
package sse

// Event represents a single Server-Sent Event.
type Event struct {
	Type  string // "event:" field; empty string means unnamed event
	Data  string // "data:" field; multiple data lines joined with "\n"
	ID    string // "id:" field
	Retry int    // "retry:" field in milliseconds; 0 means not set
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /workspace && go build ./internal/sse/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/sse/sse.go
git commit -m "feat(sse): add Event type"
```

---

### Task 2: Reader — Core Parsing

**Files:**
- Create: `internal/sse/reader.go`
- Create: `internal/sse/reader_test.go`

- [ ] **Step 1: Write failing tests for basic parsing**

In `reader_test.go`, write table-driven tests covering:

```go
package sse

import (
	"io"
	"strings"
	"testing"
)

func TestReader_Next(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Event
	}{
		{
			name:  "single event with type and data",
			input: "event: greeting\ndata: hello\n\n",
			want:  []Event{{Type: "greeting", Data: "hello"}},
		},
		{
			name:  "data only (no event type)",
			input: "data: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "multiple data lines joined with newline",
			input: "data: line1\ndata: line2\ndata: line3\n\n",
			want:  []Event{{Data: "line1\nline2\nline3"}},
		},
		{
			name:  "comment lines skipped",
			input: ": this is a comment\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "id field",
			input: "id: 42\ndata: hello\n\n",
			want:  []Event{{ID: "42", Data: "hello"}},
		},
		{
			name:  "retry field",
			input: "retry: 3000\ndata: hello\n\n",
			want:  []Event{{Retry: 3000, Data: "hello"}},
		},
		{
			name:  "invalid retry ignored",
			input: "retry: abc\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "negative retry ignored",
			input: "retry: -100\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "space after colon is optional",
			input: "data:hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "bare field name with no colon sets empty value",
			input: "data\n\n",
			want:  []Event{{Data: ""}},
		},
		{
			name:  "unknown field name without colon is ignored",
			input: "badfield\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "empty event block skipped",
			input: "\n\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "multiple events",
			input: "data: first\n\ndata: second\n\n",
			want:  []Event{{Data: "first"}, {Data: "second"}},
		},
		{
			name:  "all fields set",
			input: "event: update\ndata: payload\nid: 7\nretry: 5000\n\n",
			want:  []Event{{Type: "update", Data: "payload", ID: "7", Retry: 5000}},
		},
		{
			name:  "EOF mid-event with data returns event",
			input: "data: partial",
			want:  []Event{{Data: "partial"}},
		},
		{
			name:  "EOF mid-event no fields set returns nothing",
			input: ": just a comment",
			want:  nil,
		},
		{
			name:  "data with empty value after colon",
			input: "data: \n\n",
			want:  []Event{{Data: ""}},
		},
		{
			name:  "multiple data lines with empty line in between data",
			input: "data: line1\ndata:\ndata: line3\n\n",
			want:  []Event{{Data: "line1\n\nline3"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReader(strings.NewReader(tt.input))
			var got []Event
			for {
				ev, err := r.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got = append(got, ev)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d events, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("event[%d]\ngot:  %+v\nwant: %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./internal/sse/ -run TestReader -v`
Expected: compilation error — `NewReader` undefined

- [ ] **Step 3: Implement Reader**

In `reader.go`:

```go
package sse

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Reader parses Server-Sent Events from a byte stream.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a Reader that parses SSE from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{scanner: bufio.NewScanner(r)}
}

// Next returns the next event from the stream.
// It returns io.EOF when no more events are available.
func (r *Reader) Next() (Event, error) {
	var ev Event
	var hasFields bool
	var dataSet bool

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Blank line = event boundary.
		if line == "" {
			if hasFields {
				return ev, nil
			}
			continue
		}

		// Comment line.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field name and value.
		field, value := parseLine(line)

		switch field {
		case "event":
			ev.Type = value
			hasFields = true
		case "data":
			if dataSet {
				ev.Data += "\n" + value
			} else {
				ev.Data = value
				dataSet = true
			}
			hasFields = true
		case "id":
			ev.ID = value
			hasFields = true
		case "retry":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				ev.Retry = n
			}
			hasFields = true
		default:
			// Unknown field names are ignored per spec.
		}
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}

	// EOF: return final event if it had fields.
	if hasFields {
		return ev, nil
	}
	return Event{}, io.EOF
}

// parseLine splits an SSE line into field name and value.
// If the line contains ':', the field is everything before the first ':',
// and the value is everything after (with one optional leading space stripped).
// If there is no ':', the entire line is the field name and value is "".
func parseLine(line string) (field, value string) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return line, ""
	}
	field = line[:i]
	value = line[i+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return field, value
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace && go test ./internal/sse/ -run TestReader -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sse/reader.go internal/sse/reader_test.go
git commit -m "feat(sse): add Reader with SSE wire format parser"
```

---

### Task 3: Writer — SSE Output

**Files:**
- Create: `internal/sse/writer.go`
- Create: `internal/sse/writer_test.go`

- [ ] **Step 1: Write failing tests for Writer**

In `writer_test.go`:

```go
package sse

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriter_WriteEvent(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "event with type and data",
			event: Event{Type: "greeting", Data: "hello"},
			want:  "event: greeting\ndata: hello\n\n",
		},
		{
			name:  "data only",
			event: Event{Data: "hello"},
			want:  "data: hello\n\n",
		},
		{
			name:  "multi-line data split into multiple data lines",
			event: Event{Data: "line1\nline2\nline3"},
			want:  "data: line1\ndata: line2\ndata: line3\n\n",
		},
		{
			name:  "all fields",
			event: Event{Type: "update", Data: "payload", ID: "7", Retry: 5000},
			want:  "event: update\ndata: payload\nid: 7\nretry: 5000\n\n",
		},
		{
			name:  "id only (no data)",
			event: Event{ID: "42"},
			want:  "id: 42\n\n",
		},
		{
			name:  "retry only",
			event: Event{Retry: 3000},
			want:  "retry: 3000\n\n",
		},
		{
			name:  "zero-value event produces only trailing blank line",
			event: Event{},
			want:  "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := NewWriter(rec)
			if err := w.WriteEvent(tt.event); err != nil {
				t.Fatalf("WriteEvent error: %v", err)
			}
			got := rec.Body.String()
			if got != tt.want {
				t.Errorf("output mismatch\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestWriter_SetHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewWriter(rec)
	w.SetHeaders()

	resp := rec.Result()
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
	}
	if got := resp.Header.Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection = %q, want %q", got, "keep-alive")
	}
}

func TestWriter_Flush(t *testing.T) {
	flushed := false
	fw := &flushRecorder{
		ResponseWriter: httptest.NewRecorder(),
		onFlush:        func() { flushed = true },
	}
	w := NewWriter(fw)
	_ = w.WriteEvent(Event{Data: "test"})
	if !flushed {
		t.Error("WriteEvent did not flush")
	}
}

func TestWriter_NoFlusher(t *testing.T) {
	// A ResponseWriter that does not implement http.Flusher should cause an error.
	nf := &noFlushWriter{header: make(http.Header)}
	w := NewWriter(nf)
	err := w.WriteEvent(Event{Data: "test"})
	if err == nil {
		t.Fatal("expected error for non-flushable writer, got nil")
	}
}

func TestWriter_WriteError(t *testing.T) {
	fw := &failWriter{header: make(http.Header)}
	w := NewWriter(fw)
	err := w.WriteEvent(Event{Data: "test"})
	if err == nil {
		t.Fatal("expected error from failing writer, got nil")
	}
}

// --- test helpers ---

type flushRecorder struct {
	http.ResponseWriter
	onFlush func()
}

func (f *flushRecorder) Flush() {
	f.onFlush()
}

// noFlushWriter implements http.ResponseWriter but NOT http.Flusher.
type noFlushWriter struct {
	header http.Header
}

func (n *noFlushWriter) Header() http.Header        { return n.header }
func (n *noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noFlushWriter) WriteHeader(int)             {}

// failWriter returns an error on Write.
type failWriter struct {
	header http.Header
}

func (f *failWriter) Header() http.Header        { return f.header }
func (f *failWriter) Write([]byte) (int, error)   { return 0, errors.New("write failed") }
func (f *failWriter) WriteHeader(int)             {}
func (f *failWriter) Flush()                      {}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace && go test ./internal/sse/ -run TestWriter -v`
Expected: compilation error — `NewWriter` undefined

- [ ] **Step 3: Implement Writer**

In `writer.go`:

```go
package sse

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Writer writes Server-Sent Events to an HTTP response.
type Writer struct {
	w http.ResponseWriter
}

// NewWriter creates a Writer that writes SSE to w.
func NewWriter(w http.ResponseWriter) *Writer {
	return &Writer{w: w}
}

// SetHeaders sets the standard SSE response headers.
func (w *Writer) SetHeaders() {
	h := w.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

// WriteEvent writes a single event in SSE wire format and flushes.
// Returns an error if the underlying writer fails or does not support flushing.
func (w *Writer) WriteEvent(e Event) error {
	var b strings.Builder

	if e.Type != "" {
		fmt.Fprintf(&b, "event: %s\n", e.Type)
	}
	if e.Data != "" {
		for _, line := range strings.Split(e.Data, "\n") {
			fmt.Fprintf(&b, "data: %s\n", line)
		}
	}
	if e.ID != "" {
		fmt.Fprintf(&b, "id: %s\n", e.ID)
	}
	if e.Retry > 0 {
		fmt.Fprintf(&b, "retry: %d\n", e.Retry)
	}

	// Trailing blank line to end the event.
	b.WriteByte('\n')

	if _, err := fmt.Fprint(w.w, b.String()); err != nil {
		return err
	}

	f, ok := w.w.(http.Flusher)
	if !ok {
		return errors.New("sse: ResponseWriter does not implement http.Flusher")
	}
	f.Flush()

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace && go test ./internal/sse/ -run TestWriter -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sse/writer.go internal/sse/writer_test.go
git commit -m "feat(sse): add Writer with SSE wire format output"
```

---

### Task 4: Round-Trip Test

**Files:**
- Modify: `internal/sse/writer_test.go`

- [ ] **Step 1: Write round-trip test**

Append to `writer_test.go`:

```go
func TestRoundTrip(t *testing.T) {
	events := []Event{
		{Type: "message_start", Data: `{"type":"message_start"}`},
		{Type: "content_block_start", Data: `{"type":"content_block_start","index":0}`},
		{Type: "content_block_delta", Data: `{"type":"content_block_delta","delta":{"text":"Hello"}}`},
		{Type: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Type: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`},
		{Type: "message_stop", Data: `{"type":"message_stop"}`},
	}

	// Write events to a recorder.
	rec := httptest.NewRecorder()
	w := NewWriter(rec)
	for _, ev := range events {
		if err := w.WriteEvent(ev); err != nil {
			t.Fatalf("WriteEvent: %v", err)
		}
	}

	// Parse them back.
	r := NewReader(strings.NewReader(rec.Body.String()))
	var got []Event
	for {
		ev, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during read-back: %v", err)
		}
		got = append(got, ev)
	}

	if len(got) != len(events) {
		t.Fatalf("round-trip: got %d events, want %d", len(got), len(events))
	}
	for i := range got {
		if got[i] != events[i] {
			t.Errorf("event[%d]\ngot:  %+v\nwant: %+v", i, got[i], events[i])
		}
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd /workspace && go test ./internal/sse/ -run TestRoundTrip -v`
Expected: PASS

- [ ] **Step 3: Run all tests with race detector**

Run: `cd /workspace && go test -race ./internal/sse/`
Expected: all PASS, no races

- [ ] **Step 4: Run project-wide tests to verify no regressions**

Run: `cd /workspace && make test-unit`
Expected: all PASS

- [ ] **Step 5: Run linter**

Run: `cd /workspace && make lint`
Expected: no new issues

- [ ] **Step 6: Commit**

```bash
git add internal/sse/writer_test.go
git commit -m "test(sse): add round-trip test with Anthropic-shaped events"
```
