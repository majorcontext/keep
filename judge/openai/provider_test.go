package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/majorcontext/keep/judge"
	openaijudge "github.com/majorcontext/keep/judge/openai"
)

func TestProviderJudgeDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}
		resp := map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"decision": "deny", "reason": "harmful content"}`,
				},
			}},
			"usage": map[string]any{
				"prompt_tokens": 90, "completion_tokens": 25,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := openaijudge.New("test-key", openaijudge.WithBaseURL(srv.URL))
	v, err := p.Judge(context.Background(), judge.Request{
		Model: "gpt-4o-mini", Prompt: "safe?", Content: "bad content",
	})
	if err != nil {
		t.Fatalf("Judge error: %v", err)
	}
	if v.Decision != judge.Deny {
		t.Errorf("Decision = %q, want %q", v.Decision, judge.Deny)
	}
	if v.Usage.InputTokens != 90 {
		t.Errorf("InputTokens = %d, want 90", v.Usage.InputTokens)
	}
}

func TestModelShortcuts(t *testing.T) {
	tests := []string{"gpt-4o", "gpt-4o-mini", "o3"}
	for _, s := range tests {
		if resolved := openaijudge.ResolveModel(s); resolved == "" {
			t.Errorf("ResolveModel(%q) returned empty", s)
		}
	}
}
