package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
)

// Client is an MCP Streamable HTTP client.
type Client struct {
	url        string
	httpClient *http.Client
	authFn     func(r *http.Request)
	nextID     atomic.Int32
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBearerToken returns a ClientOption that reads a bearer token from the
// given environment variable at call time and adds it as an Authorization
// header.
func WithBearerToken(envVar string) ClientOption {
	return func(c *Client) {
		c.authFn = func(r *http.Request) {
			token := os.Getenv(envVar)
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		}
	}
}

// WithHeader returns a ClientOption that reads a value from the given
// environment variable at call time and sets it as a custom request header.
func WithHeader(headerName, envVar string) ClientOption {
	return func(c *Client) {
		c.authFn = func(r *http.Request) {
			val := os.Getenv(envVar)
			if val != "" {
				r.Header.Set(headerName, val)
			}
		}
	}
}

// NewClient creates a new MCP client targeting upstream with the given options.
func NewClient(upstream string, opts ...ClientOption) *Client {
	c := &Client{
		url:        upstream,
		httpClient: &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// call executes a JSON-RPC request and returns the raw response.
func (c *Client) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	id := int(c.nextID.Add(1))
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if c.authFn != nil {
		c.authFn(httpReq)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: http: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("mcp: decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return &rpcResp, nil
}

// unmarshalResult re-marshals the generic Result field into dst.
func unmarshalResult(rpcResp *JSONRPCResponse, dst any) error {
	raw, err := json.Marshal(rpcResp.Result)
	if err != nil {
		return fmt.Errorf("mcp: re-marshal result: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("mcp: unmarshal result: %w", err)
	}
	return nil
}

// Initialize sends an initialize request to the MCP server.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: ClientInfo{
			Name:    "keep",
			Version: "0.1.0",
		},
	}

	rpcResp, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}

	var result InitializeResult
	if err := unmarshalResult(rpcResp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTools retrieves the list of tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	rpcResp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result ListToolsResult
	if err := unmarshalResult(rpcResp, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes a named tool on the MCP server with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	params := ToolCallParams{
		Name:      name,
		Arguments: args,
	}

	rpcResp, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	var result ToolCallResult
	if err := unmarshalResult(rpcResp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
