package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioClient is an MCP client that communicates with a subprocess over stdio
// using newline-delimited JSON-RPC.
type StdioClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	nextID  atomic.Int32
	stderr  *bytes.Buffer
}

// NewStdioClient creates a new StdioClient that spawns the given command.
// The command is started immediately. Call Close to shut it down.
func NewStdioClient(name string, args ...string) (*StdioClient, error) {
	cmd := exec.Command(name, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start process: %w", err)
	}

	return &StdioClient{
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
		stderr:  &stderr,
	}, nil
}

// call sends a JSON-RPC request and reads the response.
// It skips any server-initiated notifications (messages without an id field)
// that may arrive before the actual response.
func (c *StdioClient) call(_ context.Context, method string, params any) (*JSONRPCResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("mcp: write request: %w", err)
	}

	// Read lines until we get a response with a matching ID.
	// MCP servers may send async notifications (no id) at any time.
	for {
		if !c.scanner.Scan() {
			if err := c.scanner.Err(); err != nil {
				return nil, fmt.Errorf("mcp: read response: %w (stderr: %s)", err, c.stderrSnippet())
			}
			return nil, fmt.Errorf("mcp: unexpected EOF from subprocess (stderr: %s)", c.stderrSnippet())
		}

		line := c.scanner.Bytes()

		// Quick check: if the line doesn't look like JSON, skip it.
		// Some MCP servers (via npx) may emit non-JSON output during startup.
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		var rpcResp JSONRPCResponse
		if err := json.Unmarshal(line, &rpcResp); err != nil {
			// Non-JSON line; skip.
			continue
		}

		// Skip server-initiated notifications (no id field).
		if rpcResp.ID == nil {
			continue
		}

		if rpcResp.Error != nil {
			return nil, fmt.Errorf("mcp: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}

		return &rpcResp, nil
	}
}

// notify sends a JSON-RPC notification (no ID, no response expected).
func (c *StdioClient) notify(method string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Notifications have no ID field.
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}{
		JSONRPC: "2.0",
		Method:  method,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcp: marshal notification: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("mcp: write notification: %w", err)
	}

	return nil
}

// Initialize sends an initialize request to the MCP server, followed by an
// initialized notification.
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2025-03-26",
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

	// Fire-and-forget initialized notification.
	_ = c.notify("notifications/initialized")

	return &result, nil
}

// ListTools retrieves the list of tools from the MCP server.
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
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
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
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

// Close shuts down the subprocess by closing stdin and killing the process.
func (c *StdioClient) Close() error {
	_ = c.stdin.Close()
	return c.cmd.Process.Kill()
}

// stderrSnippet returns the last portion of stderr output for error messages.
func (c *StdioClient) stderrSnippet() string {
	s := c.stderr.String()
	if len(s) > 256 {
		s = s[len(s)-256:]
	}
	if s == "" {
		return "<empty>"
	}
	return s
}
