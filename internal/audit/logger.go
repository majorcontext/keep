// Package audit provides structured audit logging for Keep evaluations.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/majorcontext/keep/internal/engine"
)

// Logger writes audit entries as JSON Lines to a writer.
type Logger struct {
	mu  sync.Mutex
	enc *json.Encoder
}

// NewLogger creates a logger that writes to the given writer.
func NewLogger(w io.Writer) *Logger {
	return &Logger{enc: json.NewEncoder(w)}
}

// NewLoggerFromOutput creates a logger from an output string.
// "stdout" -> os.Stdout, "stderr" -> os.Stderr, anything else -> file path.
func NewLoggerFromOutput(output string) (*Logger, io.Closer, error) {
	switch output {
	case "", "stdout":
		return NewLogger(os.Stdout), nil, nil
	case "stderr":
		return NewLogger(os.Stderr), nil, nil
	default:
		f, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		return NewLogger(f), f, nil
	}
}

// Log serializes the audit entry as JSON and writes it as a single line.
// Thread-safe.
func (l *Logger) Log(entry engine.AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.enc.Encode(entry); err != nil {
		fmt.Fprintf(os.Stderr, "audit: write error: %v\n", err)
	}
}
