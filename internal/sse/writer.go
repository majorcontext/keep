package sse

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Writer writes Server-Sent Events to an HTTP response.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter creates a Writer that writes SSE to w.
// It returns an error if w does not implement http.Flusher.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("sse: ResponseWriter does not implement http.Flusher")
	}
	return &Writer{w: w, flusher: f}, nil
}

// SetHeaders sets the standard SSE response headers.
func (w *Writer) SetHeaders() {
	h := w.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

// WriteEvent writes a single event in SSE wire format and flushes.
// Returns an error if the underlying writer fails.
func (w *Writer) WriteEvent(e Event) error {
	var b strings.Builder

	if e.Type != "" {
		b.WriteString("event: ")
		b.WriteString(e.Type)
		b.WriteByte('\n')
	}
	if e.Data != "" {
		for _, line := range strings.Split(e.Data, "\n") {
			b.WriteString("data: ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if e.ID != "" {
		b.WriteString("id: ")
		b.WriteString(e.ID)
		b.WriteByte('\n')
	}
	if e.Retry > 0 {
		b.WriteString("retry: ")
		b.WriteString(strconv.Itoa(e.Retry))
		b.WriteByte('\n')
	}

	// Trailing blank line to end the event.
	b.WriteByte('\n')

	if _, err := w.w.Write([]byte(b.String())); err != nil {
		return err
	}

	w.flusher.Flush()

	return nil
}
