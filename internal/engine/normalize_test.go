package engine

import (
	"testing"
)

func TestDeepLowerStrings(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{
			name: "simple strings",
			in:   map[string]any{"name": "Bash", "count": 42},
			want: map[string]any{"name": "bash", "count": 42},
		},
		{
			name: "nested map",
			in:   map[string]any{"input": map[string]any{"command": "CURL https://evil.com"}},
			want: map[string]any{"input": map[string]any{"command": "curl https://evil.com"}},
		},
		{
			name: "slice of mixed types",
			in:   map[string]any{"tags": []any{"FOO", "Bar", 123}},
			want: map[string]any{"tags": []any{"foo", "bar", 123}},
		},
		{
			name: "nil map",
			in:   nil,
			want: nil,
		},
		{
			name: "preserves keys",
			in:   map[string]any{"ToolName": "Bash"},
			want: map[string]any{"ToolName": "bash"},
		},
		{
			name: "bool and float preserved",
			in:   map[string]any{"enabled": true, "score": 3.14, "name": "Test"},
			want: map[string]any{"enabled": true, "score": 3.14, "name": "test"},
		},
		{
			name: "empty map",
			in:   map[string]any{},
			want: map[string]any{},
		},
		{
			name: "deeply nested",
			in:   map[string]any{"a": map[string]any{"b": map[string]any{"c": "DEEP"}}},
			want: map[string]any{"a": map[string]any{"b": map[string]any{"c": "deep"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepLowerStrings(tt.in)
			assertMapsEqual(t, tt.want, got)
		})
	}
}

func TestDeepLowerStrings_DoesNotMutateOriginal(t *testing.T) {
	original := map[string]any{"name": "Bash", "nested": map[string]any{"cmd": "CURL"}}
	_ = deepLowerStrings(original)

	if original["name"] != "Bash" {
		t.Error("original map was mutated")
	}
	nested := original["nested"].(map[string]any)
	if nested["cmd"] != "CURL" {
		t.Error("original nested map was mutated")
	}
}

func TestLowerContext(t *testing.T) {
	ctx := map[string]any{
		"agent_id":  "BOT-1",
		"direction": "Request",
		"scope":     "Test-Scope",
		"timestamp": 12345, // non-string preserved
		"labels":    map[string]string{"Env": "PROD", "team": "Infra"},
	}

	got := lowerContext(ctx)

	if got["agent_id"] != "bot-1" {
		t.Errorf("agent_id: want bot-1, got %v", got["agent_id"])
	}
	if got["direction"] != "request" {
		t.Errorf("direction: want request, got %v", got["direction"])
	}
	if got["scope"] != "test-scope" {
		t.Errorf("scope: want test-scope, got %v", got["scope"])
	}
	if got["timestamp"] != 12345 {
		t.Errorf("timestamp: want 12345, got %v", got["timestamp"])
	}

	labels := got["labels"].(map[string]string)
	if labels["env"] != "prod" {
		t.Errorf("labels[env]: want prod, got %v", labels["env"])
	}
	if labels["team"] != "infra" {
		t.Errorf("labels[team]: want infra, got %v", labels["team"])
	}
	// Original key "Env" should be lowered to "env"
	if _, ok := labels["Env"]; ok {
		t.Error("labels key 'Env' should have been lowered to 'env'")
	}
}

// assertMapsEqual is a simple deep-equal helper for map[string]any.
func assertMapsEqual(t *testing.T, want, got map[string]any) {
	t.Helper()
	if want == nil && got == nil {
		return
	}
	if want == nil || got == nil {
		t.Errorf("want %v, got %v", want, got)
		return
	}
	if len(want) != len(got) {
		t.Errorf("length mismatch: want %d, got %d\nwant: %v\ngot:  %v", len(want), len(got), want, got)
		return
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		// Handle nested maps
		if wm, ok := wv.(map[string]any); ok {
			if gm, ok := gv.(map[string]any); ok {
				assertMapsEqual(t, wm, gm)
				continue
			}
		}
		// Handle slices
		if ws, ok := wv.([]any); ok {
			gs, ok := gv.([]any)
			if !ok || len(ws) != len(gs) {
				t.Errorf("key %q: want %v, got %v", k, wv, gv)
				continue
			}
			for i := range ws {
				if ws[i] != gs[i] {
					t.Errorf("key %q[%d]: want %v, got %v", k, i, ws[i], gs[i])
				}
			}
			continue
		}
		if wv != gv {
			t.Errorf("key %q: want %v, got %v", k, wv, gv)
		}
	}
}
