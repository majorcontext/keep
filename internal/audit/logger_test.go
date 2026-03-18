package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/majorcontext/keep/internal/engine"
)

func TestJSONLogger_Write(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	entry := engine.AuditEntry{
		Scope:     "test-scope",
		Operation: "test-op",
		AgentID:   "test-agent",
		Decision:  engine.Allow,
	}
	logger.Log(entry)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected non-empty output")
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, line)
	}

	if got["Scope"] != "test-scope" {
		t.Errorf("Scope: got %v, want test-scope", got["Scope"])
	}
	if got["Operation"] != "test-op" {
		t.Errorf("Operation: got %v, want test-op", got["Operation"])
	}
	if got["Decision"] != string(engine.Allow) {
		t.Errorf("Decision: got %v, want %s", got["Decision"], engine.Allow)
	}
}

func TestJSONLogger_MultipleEntries(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	entries := []engine.AuditEntry{
		{Scope: "scope-1", Operation: "op-1", AgentID: "agent-1", Decision: engine.Allow},
		{Scope: "scope-2", Operation: "op-2", AgentID: "agent-2", Decision: engine.Deny},
		{Scope: "scope-3", Operation: "op-3", AgentID: "agent-3", Decision: engine.Redact},
	}

	for _, e := range entries {
		logger.Log(e)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
	}
}

func TestJSONLogger_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			logger.Log(engine.AuditEntry{
				Scope:     "concurrent-scope",
				Operation: "concurrent-op",
				AgentID:   "test-agent",
				Decision:  engine.Allow,
			})
		}(i)
	}

	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != goroutines {
		t.Fatalf("expected %d lines, got %d", goroutines, len(lines))
	}

	for i, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d is not valid JSON (possible interleaving): %v\nline: %s", i, err, line)
		}
	}
}
