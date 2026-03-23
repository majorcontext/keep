package sse

import (
	"errors"
	"io"
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
			w, err := NewWriter(rec)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
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
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	w.SetHeaders()

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

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
	w, err := NewWriter(fw)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	_ = w.WriteEvent(Event{Data: "test"})
	if !flushed {
		t.Error("WriteEvent did not flush")
	}
}

func TestWriter_NoFlusher(t *testing.T) {
	// A ResponseWriter that does not implement http.Flusher should cause an error from NewWriter.
	nf := &noFlushWriter{header: make(http.Header)}
	_, err := NewWriter(nf)
	if err == nil {
		t.Fatal("expected error for non-flushable writer, got nil")
	}
}

func TestWriter_WriteError(t *testing.T) {
	fw := &failWriter{header: make(http.Header)}
	w, err := NewWriter(fw)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	err = w.WriteEvent(Event{Data: "test"})
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

func (n *noFlushWriter) Header() http.Header         { return n.header }
func (n *noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noFlushWriter) WriteHeader(int)             {}

// failWriter returns an error on Write.
type failWriter struct {
	header http.Header
}

func (f *failWriter) Header() http.Header        { return f.header }
func (f *failWriter) Write([]byte) (int, error)  { return 0, errors.New("write failed") }
func (f *failWriter) WriteHeader(int)            {}
func (f *failWriter) Flush()                     {}

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
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
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
