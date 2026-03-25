package sse

import (
	"errors"
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

func TestReader_MaxLineSize(t *testing.T) {
	// Build a line that exceeds maxLineSize (1 MB).
	bigLine := "data: " + strings.Repeat("x", maxLineSize) + "\n\n"
	r := NewReader(strings.NewReader(bigLine))
	_, err := r.Next()
	if err == nil {
		t.Fatal("expected error for oversized line, got nil")
	}
	// bufio.Scanner returns a generic "token too long" error.
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected 'too long' error, got: %v", err)
	}
}

func TestReader_ScannerError(t *testing.T) {
	r := NewReader(&errReader{
		data: "data: hello\n",
		err:  errors.New("connection reset"),
	})
	_, err := r.Next()
	if err == nil || err.Error() != "connection reset" {
		t.Fatalf("expected 'connection reset' error, got: %v", err)
	}
}

type errReader struct {
	data string
	err  error
	done bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
}
