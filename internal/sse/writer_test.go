package sse

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
