package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalComputeMetrics(t *testing.T) {
	results := []evalResult{
		{expected: "deny", actual: "deny"},   // TP
		{expected: "deny", actual: "deny"},   // TP
		{expected: "allow", actual: "allow"}, // TN
		{expected: "allow", actual: "deny"},  // FP
		{expected: "deny", actual: "allow"},  // FN
	}
	m := computeMetrics(results)
	if m.Total != 5 {
		t.Errorf("Total = %d, want 5", m.Total)
	}
	if m.Correct != 3 {
		t.Errorf("Correct = %d, want 3", m.Correct)
	}
	if m.Accuracy < 0.59 || m.Accuracy > 0.61 {
		t.Errorf("Accuracy = %.2f, want ~0.60", m.Accuracy)
	}
	if m.Precision < 0.66 || m.Precision > 0.67 {
		t.Errorf("Precision = %.2f, want ~0.67", m.Precision)
	}
	if m.Recall < 0.66 || m.Recall > 0.67 {
		t.Errorf("Recall = %.2f, want ~0.67", m.Recall)
	}
	if m.FalsePositives != 1 {
		t.Errorf("FalsePositives = %d, want 1", m.FalsePositives)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1", m.FalseNegatives)
	}
}

func TestEvalParseDataset(t *testing.T) {
	dataset := `[
		{"input": {"operation": "llm.text", "params": {"text": "bad"}}, "scope": "test", "expected": "deny", "label": "harmful"},
		{"input": {"operation": "llm.text", "params": {"text": "good"}}, "scope": "test", "expected": "allow", "label": "safe"}
	]`
	tmp := filepath.Join(t.TempDir(), "dataset.json")
	if err := os.WriteFile(tmp, []byte(dataset), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := parseDataset(tmp)
	if err != nil {
		t.Fatalf("parseDataset: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	if entries[0].Expected != "deny" {
		t.Errorf("entries[0].Expected = %q, want %q", entries[0].Expected, "deny")
	}
	if entries[1].Label != "safe" {
		t.Errorf("entries[1].Label = %q, want %q", entries[1].Label, "safe")
	}
}
