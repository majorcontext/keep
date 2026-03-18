package keep_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/majorcontext/keep"
)

func BenchmarkEvaluate_Allow(b *testing.B) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	call := keep.Call{
		Operation: "create_issue",
		Params:    map[string]any{"title": "Fix bug", "teamId": "TEAM-ENG", "priority": 1},
		Context:   keep.CallContext{AgentID: "bench", Timestamp: time.Now(), Scope: "linear-tools"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Evaluate(call, "linear-tools")
	}
}

func BenchmarkEvaluate_Deny(b *testing.B) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	call := keep.Call{
		Operation: "delete_issue",
		Params:    map[string]any{"issueId": "ISSUE-123"},
		Context:   keep.CallContext{AgentID: "bench", Timestamp: time.Now(), Scope: "linear-tools"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Evaluate(call, "linear-tools")
	}
}

func BenchmarkEvaluate_Redact(b *testing.B) {
	eng, err := keep.Load("testdata/rules")
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	call := keep.Call{
		Operation: "llm.tool_result",
		Params:    map[string]any{"content": "key is AKIAIOSFODNN7EXAMPLE"},
		Context:   keep.CallContext{AgentID: "bench", Timestamp: time.Now(), Scope: "anthropic-gateway"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Evaluate(call, "anthropic-gateway")
	}
}

func BenchmarkEvaluate_ManyRules(b *testing.B) {
	// Build a temp dir with a generated rule file containing 50 deny rules.
	dir := b.TempDir()

	var sb strings.Builder
	sb.WriteString("scope: bench-scope\n")
	sb.WriteString("mode: enforce\n")
	sb.WriteString("rules:\n")
	for i := 0; i < 50; i++ {
		sb.WriteString(fmt.Sprintf("  - name: deny-op-%d\n", i))
		sb.WriteString(fmt.Sprintf("    match:\n      operation: \"op_%d\"\n", i))
		sb.WriteString("    action: deny\n")
		sb.WriteString(fmt.Sprintf("    message: \"operation op_%d is not permitted.\"\n", i))
	}

	if err := os.WriteFile(filepath.Join(dir, "bench.yaml"), []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}

	eng, err := keep.Load(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	// Use an operation that matches none of the 50 deny rules — worst-case traversal.
	call := keep.Call{
		Operation: "allowed_operation",
		Params:    map[string]any{"key": "value"},
		Context:   keep.CallContext{AgentID: "bench", Timestamp: time.Now(), Scope: "bench-scope"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Evaluate(call, "bench-scope")
	}
}
