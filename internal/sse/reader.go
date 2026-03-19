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
	var dataLines []string

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Blank line = event boundary.
		if line == "" {
			if hasFields {
				ev.Data = strings.Join(dataLines, "\n")
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
			dataLines = append(dataLines, value)
			hasFields = true
		case "id":
			ev.ID = value
			hasFields = true
		case "retry":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				ev.Retry = n
				hasFields = true
			}
		default:
			// Unknown field names are ignored per spec.
		}
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}

	// EOF: return final event if it had fields.
	if hasFields {
		ev.Data = strings.Join(dataLines, "\n")
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
