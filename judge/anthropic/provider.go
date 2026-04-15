// Package anthropic implements a judge.Provider backed by the Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/majorcontext/keep/judge"
)

const defaultBaseURL = "https://api.anthropic.com"
const apiVersion = "2023-06-01"

// Provider evaluates content using the Anthropic Messages API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Option configures a Provider.
type Option func(*Provider)

// WithBaseURL overrides the default Anthropic API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// New creates an Anthropic judge provider with the given API key.
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

// Judge evaluates content against a policy prompt using the Anthropic Messages API.
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return judge.Verdict{}, fmt.Errorf("judge request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
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
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return judge.Verdict{}, fmt.Errorf("empty response from judge")
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &verdict); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse verdict JSON: %w", err)
	}

	var d judge.Decision
	switch verdict.Decision {
	case "allow":
		d = judge.Allow
	case "deny":
		d = judge.Deny
	default:
		return judge.Verdict{}, fmt.Errorf("unknown verdict decision %q (expected \"allow\" or \"deny\")", verdict.Decision)
	}

	return judge.Verdict{
		Decision: d,
		Reason:   verdict.Reason,
		Usage: judge.Usage{
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
		},
	}, nil
}
