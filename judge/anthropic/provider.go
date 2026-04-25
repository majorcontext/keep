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
// It uses forced tool use to guarantee structured JSON output.
func (p *Provider) Judge(ctx context.Context, req judge.Request) (judge.Verdict, error) {
	model := ResolveModel(req.Model)

	body := map[string]any{
		"model":      model,
		"max_tokens": 256,
		"messages": []map[string]any{{
			"role": "user",
			"content": fmt.Sprintf(
				"You are a policy judge. Evaluate the following content against this criteria:\n\n"+
					"Criteria: %s\n\nContent: %s",
				req.Prompt, req.Content),
		}},
		"tools": []map[string]any{{
			"name":        "verdict",
			"description": "Return your judgment as a structured verdict.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"decision": map[string]any{
						"type":        "string",
						"enum":        []string{"allow", "deny"},
						"description": "Whether to allow or deny the content.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Brief explanation for the decision.",
					},
				},
				"required": []string{"decision", "reason"},
			},
		}},
		"tool_choice": map[string]any{
			"type": "tool",
			"name": "verdict",
		},
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
			Type  string          `json:"type"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse response: %w", err)
	}

	// Find the tool_use content block.
	var toolInput json.RawMessage
	for _, block := range apiResp.Content {
		if block.Type == "tool_use" {
			toolInput = block.Input
			break
		}
	}
	if toolInput == nil {
		return judge.Verdict{}, fmt.Errorf("no tool_use block in judge response")
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(toolInput, &verdict); err != nil {
		return judge.Verdict{}, fmt.Errorf("parse verdict: %w", err)
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
