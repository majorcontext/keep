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
