package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/majorcontext/keep/judge"
)

const defaultBaseURL = "https://api.openai.com"

// Provider implements judge.Provider using OpenAI's Chat Completions API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Option configures a Provider.
type Option func(*Provider)

// WithBaseURL overrides the default OpenAI API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// New creates an OpenAI judge provider with the given API key.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Judge sends content to OpenAI for evaluation and returns a verdict.
func (p *Provider) Judge(ctx context.Context, req judge.Request) (judge.Verdict, error) {
	model := ResolveModel(req.Model)

	body := map[string]any{
		"model":      model,
		"max_tokens": 256,
		"messages": []map[string]any{{
			"role": "user",
			"content": fmt.Sprintf(
				"You are a policy judge. Evaluate the following content against this criteria:\n\n"+
					"Criteria: %s\n\nContent: %s\n\n"+
					"Respond with ONLY a JSON object: {\"decision\": \"allow\" or \"deny\", \"reason\": \"brief explanation\"}",
				req.Prompt, req.Content),
		}},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("judge request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return judge.Verdict{}, fmt.Errorf("judge API error (status %d): %s", resp.StatusCode, respBody)
	}

	return parseResponse(respBody)
}

func parseResponse(body []byte) (judge.Verdict, error) {
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return judge.Verdict{}, fmt.Errorf("empty response from judge")
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &verdict); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse verdict JSON: %w", err)
	}

	d := judge.Allow
	if verdict.Decision == "deny" {
		d = judge.Deny
	}

	return judge.Verdict{
		Decision: d,
		Reason:   verdict.Reason,
		Usage: judge.Usage{
			InputTokens:  apiResp.Usage.PromptTokens,
			OutputTokens: apiResp.Usage.CompletionTokens,
		},
	}, nil
}
