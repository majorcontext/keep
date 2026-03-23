package engine

import (
	"strings"
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

// makeMCPReadOnlyEvaluator builds an evaluator with a single rule that denies
// write_query operations, simulating a read-only database policy for MCP tools.
func makeMCPReadOnlyEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "block-write-query",
			Action: config.ActionDeny,
			Match: config.Match{
				Operation: "write_query",
			},
			Message: "Database is read-only.",
		},
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// makeMCPResponseRedactEvaluator builds an evaluator with a redact rule that
// matches read_query responses and scrubs known passwords from params.content.
func makeMCPResponseRedactEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "redact-passwords-in-response",
			Action: config.ActionRedact,
			Match: config.Match{
				Operation: "read_query",
				When:      "context.direction == 'response'",
			},
			Redact: &config.RedactSpec{
				Target: "params.content",
				Patterns: []config.RedactPattern{
					{Match: `hunter2|p@ssw0rd!|letmein123`, Replace: "********"},
				},
			},
		},
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// makeMCPCall creates a Call for an MCP tool operation with the given params
// and direction context.
func makeMCPCall(operation string, params map[string]any, direction string) Call {
	return Call{
		Operation: operation,
		Params:    params,
		Context:   CallContext{Scope: "test", Direction: direction},
	}
}

// ─── Write query blocking ────────────────────────────────────────────

func TestMCPToolCall_WriteQueryDenied(t *testing.T) {
	ev := makeMCPReadOnlyEvaluator(t)

	cases := []struct {
		name   string
		params map[string]any
	}{
		{"INSERT", map[string]any{"sql": "INSERT INTO users (name) VALUES ('alice')"}},
		{"UPDATE", map[string]any{"sql": "UPDATE users SET name = 'bob' WHERE id = 1"}},
		{"DELETE", map[string]any{"sql": "DELETE FROM users WHERE id = 1"}},
		{"DROP TABLE", map[string]any{"sql": "DROP TABLE users"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeMCPCall("write_query", tc.params, "request"))
			if result.Decision != Deny {
				t.Errorf("expected deny, got %s for %s", result.Decision, tc.name)
			}
			if result.Message != "Database is read-only." {
				t.Errorf("expected message 'Database is read-only.', got %q", result.Message)
			}
		})
	}
}

func TestMCPToolCall_ReadQueryAllowed(t *testing.T) {
	ev := makeMCPReadOnlyEvaluator(t)

	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{
		"sql": "SELECT * FROM users WHERE id = 1",
	}, "request"))
	if result.Decision != Allow {
		t.Errorf("expected allow for read_query, got %s", result.Decision)
	}
}

func TestMCPToolCall_OtherToolsAllowed(t *testing.T) {
	ev := makeMCPReadOnlyEvaluator(t)

	tools := []string{"list_tables", "describe_table", "create_table"}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			result := ev.Evaluate(makeMCPCall(tool, nil, "request"))
			if result.Decision != Allow {
				t.Errorf("expected allow for %s, got %s", tool, result.Decision)
			}
		})
	}
}

// ─── Response redaction ──────────────────────────────────────────────

func TestMCPToolCall_ResponseRedaction(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			"single password",
			"password: hunter2",
			"password: ********",
		},
		{
			"multiple passwords",
			"creds: hunter2, p@ssw0rd!, letmein123",
			"creds: ********, ********, ********",
		},
		{
			"password in JSON",
			`{"password": "hunter2", "user": "admin"}`,
			`{"password": "********", "user": "admin"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeMCPCall("read_query", map[string]any{
				"content": tc.content,
			}, "response"))
			if result.Decision != Redact {
				t.Errorf("expected redact, got %s", result.Decision)
			}
			if len(result.Mutations) == 0 {
				t.Fatal("expected mutations, got none")
			}
			if result.Mutations[0].Path != "params.content" {
				t.Errorf("expected mutation path params.content, got %s", result.Mutations[0].Path)
			}
			if result.Mutations[0].Replaced != tc.want {
				t.Errorf("redacted content = %q, want %q", result.Mutations[0].Replaced, tc.want)
			}
		})
	}
}

func TestMCPToolCall_ResponseRedaction_RequestSideIgnored(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	// The redact rule has context.direction == 'response', so a request-side
	// call with the same operation should not trigger redaction.
	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{
		"content": "password: hunter2",
	}, "request"))
	if result.Decision == Redact {
		t.Error("expected no redaction on request side, but got redact")
	}
}

func TestMCPToolCall_ResponseRedaction_NoPasswords(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	clean := []string{
		"SELECT id, name FROM users WHERE active = true",
		`{"rows": [{"id": 1, "name": "alice"}]}`,
		"No results found.",
		"Query returned 42 rows in 0.03s",
	}

	for _, content := range clean {
		t.Run(strings.TrimSpace(content[:min(len(content), 30)]), func(t *testing.T) {
			result := ev.Evaluate(makeMCPCall("read_query", map[string]any{
				"content": content,
			}, "response"))
			if result.Decision == Redact {
				t.Errorf("expected allow for clean content, got redact for: %s", content)
			}
			if len(result.Mutations) > 0 {
				t.Errorf("expected no mutations for clean content, got %d", len(result.Mutations))
			}
		})
	}
}
