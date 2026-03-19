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
