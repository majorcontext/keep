# MCP Relay Demo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add response-side policy evaluation to the MCP relay, add stdio transport support, and create a demo showing Keep as a policy proxy in front of a sqlite MCP server.

**Architecture:** The relay gets two new capabilities: (1) stdio MCP client for subprocess-based upstreams, and (2) response-side policy evaluation matching the LLM gateway's pattern. The demo uses `@modelcontextprotocol/server-sqlite` via npx as the upstream, with rules blocking writes and redacting passwords from query results.

**Tech Stack:** Go, MCP protocol (JSON-RPC over stdio), sqlite3 CLI (seed data), npx (sqlite MCP server)

**Spec:** `docs/plans/2026-03-23-mcp-relay-demo-design.md`

---

### Task 1: Stdio MCP Client

The sqlite MCP server communicates over stdio (JSON-RPC over newline-delimited stdin/stdout). The relay currently only supports HTTP upstreams. Add a stdio transport client.

**Files:**
- Create: `internal/relay/mcp/stdio_client.go`
- Create: `internal/relay/mcp/stdio_client_test.go`

- [ ] **Step 1: Write the failing test for StdioClient**

Create `internal/relay/mcp/stdio_client_test.go`:

```go
package mcp

import (
	"context"
	"testing"
)

func TestStdioClient_InitializeAndListTools(t *testing.T) {
	// Use "cat" as a trivial echo-like server — we'll send it JSON and read back.
	// For a real test, we use a small Go helper that acts as an MCP server on stdio.
	client, err := NewStdioClient("go", []string{"run", "testdata/mock_stdio_server.go"})
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Initialize
	initResult, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initResult.ProtocolVersion == "" {
		t.Error("expected non-empty protocol version")
	}

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Error("expected at least one tool")
	}
}

func TestStdioClient_CallTool(t *testing.T) {
	client, err := NewStdioClient("go", []string{"run", "testdata/mock_stdio_server.go"})
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Must initialize first
	_, err = client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
	if result.Content[0].Text == "" {
		t.Error("expected non-empty text in content")
	}
}
```

- [ ] **Step 2: Create the mock stdio MCP server for tests**

Create `internal/relay/mcp/testdata/mock_stdio_server.go`:

```go
//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		resp := response{JSONRPC: "2.0", ID: req.ID}

		switch req.Method {
		case "initialize":
			resp.Result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":   map[string]any{"tools": map[string]any{}},
				"serverInfo":     map[string]any{"name": "mock-stdio", "version": "1.0"},
			}
		case "notifications/initialized":
			continue // no response for notifications
		case "tools/list":
			resp.Result = map[string]any{
				"tools": []map[string]any{
					{"name": "echo", "description": "Echoes input"},
				},
			}
		case "tools/call":
			paramsBytes, _ := json.Marshal(req.Params)
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(paramsBytes, &params)

			argsJSON, _ := json.Marshal(params.Arguments)
			resp.Result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": string(argsJSON)},
				},
			}
		}

		out, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(out))
	}
}
```

- [ ] **Step 3: Implement StdioClient**

Create `internal/relay/mcp/stdio_client.go`:

```go
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioClient communicates with an MCP server over stdin/stdout.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex // serializes request/response pairs
	nextID atomic.Int32
}

// NewStdioClient spawns the given command and connects to it via stdio.
func NewStdioClient(command string, args []string) (*StdioClient, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("mcp stdio: start %q: %w", command, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB buffer

	return &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: scanner,
	}, nil
}

// call sends a JSON-RPC request and reads the response.
func (c *StdioClient) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID.Add(1),
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: marshal: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("mcp stdio: write: %w", err)
	}

	if !c.stdout.Scan() {
		if err := c.stdout.Err(); err != nil {
			return nil, fmt.Errorf("mcp stdio: read: %w", err)
		}
		return nil, fmt.Errorf("mcp stdio: server closed connection")
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(c.stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("mcp stdio: decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("mcp stdio: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// Initialize sends an initialize request to the MCP server.
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2025-03-26",
		Capabilities:    map[string]any{},
		ClientInfo:      ClientInfo{Name: "keep", Version: "0.1.0"},
	}

	resp, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}

	// Send initialized notification (fire and forget)
	notif, _ := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
	notif = append(notif, '\n')
	c.mu.Lock()
	c.stdin.Write(notif)
	c.mu.Unlock()

	var result InitializeResult
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTools retrieves the list of tools from the MCP server.
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result ListToolsResult
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes a named tool on the MCP server.
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	resp, err := c.call(ctx, "tools/call", ToolCallParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var result ToolCallResult
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Close kills the subprocess and cleans up.
func (c *StdioClient) Close() error {
	c.stdin.Close()
	return c.cmd.Process.Kill()
}
```

- [ ] **Step 4: Run tests to verify**

Run: `make test-unit ARGS='-run TestStdioClient -v ./internal/relay/mcp'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/mcp/stdio_client.go internal/relay/mcp/stdio_client_test.go internal/relay/mcp/testdata/mock_stdio_server.go
git commit -m "feat(relay): add stdio MCP client for subprocess upstreams"
```

---

### Task 2: Extract ToolCaller Interface and Update Router

The router currently uses `*mcp.Client` (HTTP only). Extract an interface so both HTTP and stdio clients can be used interchangeably.

**Files:**
- Modify: `internal/relay/mcp/client.go` (add interface)
- Create: `internal/relay/mcp/caller.go` (interface definition)
- Modify: `internal/relay/router.go` (use interface, support Command config)
- Modify: `internal/relay/handler.go` (use interface)
- Modify: `internal/relay/config/config.go` (add Command/Args fields to Route)
- Modify: `internal/relay/handler_test.go` (update for interface change)

- [ ] **Step 1: Define ToolCaller interface**

Create `internal/relay/mcp/caller.go`:

```go
package mcp

import "context"

// ToolCaller is the interface for MCP clients that can initialize,
// discover tools, and call tools. Both HTTP and stdio clients implement this.
type ToolCaller interface {
	Initialize(ctx context.Context) (*InitializeResult, error)
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}
```

- [ ] **Step 2: Update ToolRoute to use ToolCaller interface**

In `internal/relay/router.go`, change:

```go
// Before:
type ToolRoute struct {
	Client *mcp.Client
	Scope  string
	Tool   mcp.Tool
}

// After:
type ToolRoute struct {
	Client mcp.ToolCaller
	Scope  string
	Tool   mcp.Tool
}
```

- [ ] **Step 3: Add Command/Args to relay config**

In `internal/relay/config/config.go`, update the `Route` struct:

```go
type Route struct {
	Scope    string   `yaml:"scope"`
	Upstream string   `yaml:"upstream,omitempty"`
	Command  string   `yaml:"command,omitempty"`
	Args     []string `yaml:"args,omitempty"`
	Auth     *Auth    `yaml:"auth,omitempty"`
}
```

Update `validate()` to require either `Upstream` or `Command` (not both, not neither):

```go
if r.Upstream == "" && r.Command == "" {
	return fmt.Errorf("relay config: routes[%d]: either upstream or command is required", i)
}
if r.Upstream != "" && r.Command != "" {
	return fmt.Errorf("relay config: routes[%d]: upstream and command are mutually exclusive", i)
}
```

Remove the existing `r.Upstream == ""` check.

- [ ] **Step 4: Update router to create stdio clients**

In `internal/relay/router.go`, update `NewRouter` to check for `Command`:

```go
var client mcp.ToolCaller
if route.Command != "" {
	stdioClient, err := mcp.NewStdioClient(route.Command, route.Args)
	if err != nil {
		log.Printf("relay: stdio upstream %q failed to start: %v (skipping)", route.Command, err)
		continue
	}
	client = stdioClient
} else {
	var opts []mcp.ClientOption
	if route.Auth != nil {
		switch route.Auth.Type {
		case "bearer":
			opts = append(opts, mcp.WithBearerToken(route.Auth.TokenEnv))
		case "header":
			opts = append(opts, mcp.WithHeader(route.Auth.Header, route.Auth.TokenEnv))
		}
	}
	client = mcp.NewClient(route.Upstream, opts...)
}
```

- [ ] **Step 5: Run existing tests to verify nothing broke**

Run: `make test-unit ARGS='-v ./internal/relay/...'`
Expected: All existing tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/relay/mcp/caller.go internal/relay/mcp/client.go internal/relay/router.go internal/relay/handler.go internal/relay/config/config.go internal/relay/handler_test.go
git commit -m "refactor(relay): extract ToolCaller interface, support stdio upstreams in config"
```

---

### Task 3: Response-Side Policy Evaluation

Add response-side evaluation to `HandleToolCall`. After the upstream returns, evaluate the result content against policy rules with `Direction: "response"`. This enables rules that redact or deny based on tool output.

**Files:**
- Modify: `internal/relay/handler.go`
- Modify: `internal/relay/handler_test.go`
- Create: `internal/relay/testdata/rules/test-tools-response.yaml`

- [ ] **Step 1: Write failing test for response-side redaction**

Add a new test rules file `internal/relay/testdata/rules/test-tools-response.yaml`:

```yaml
scope: test-response-scope
mode: enforce
rules:
  - name: redact-passwords
    match:
      operation: "read_query"
      when: "context.direction == 'response'"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "hunter2|p@ssw0rd!"
          replace: "********"
  - name: block-writes
    match:
      operation: "write_query"
    action: deny
    message: "Database is read-only."
```

Add test to `internal/relay/handler_test.go`:

```go
func TestHandler_ResponseRedact(t *testing.T) {
	// Mock upstream that returns content containing a password
	tools := []mcp.Tool{
		{Name: "read_query", Description: "Run a SELECT query"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = mcp.InitializeResult{
				ProtocolVersion: "2025-03-26",
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			}
		case "tools/list":
			resp.Result = mcp.ListToolsResult{Tools: tools}
		case "tools/call":
			resp.Result = mcp.ToolCallResult{
				Content: []mcp.ContentBlock{
					{Type: "text", Text: `[{"id":1,"name":"Alice","password":"hunter2"},{"id":2,"name":"Bob","password":"p@ssw0rd!"}]`},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	routes := []relayconfig.Route{{Scope: "test-response-scope", Upstream: srv.URL}}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	handler := NewRelayHandler(engine, router, audit.NewLogger(&buf))

	result, err := handler.HandleToolCall(context.Background(), "read_query", map[string]any{"query": "SELECT * FROM users"})
	if err != nil {
		t.Fatalf("HandleToolCall: unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected non-empty result")
	}

	text := result.Content[0].Text
	if strings.Contains(text, "hunter2") || strings.Contains(text, "p@ssw0rd!") {
		t.Errorf("response should not contain passwords, got: %s", text)
	}
	if !strings.Contains(text, "********") {
		t.Errorf("response should contain redaction marker, got: %s", text)
	}
}

func TestHandler_ResponseDeny(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "read_query", Description: "Run a SELECT query"},
		{Name: "write_query", Description: "Run a write query"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = mcp.InitializeResult{
				ProtocolVersion: "2025-03-26",
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			}
		case "tools/list":
			resp.Result = mcp.ListToolsResult{Tools: tools}
		case "tools/call":
			resp.Result = mcp.ToolCallResult{
				Content: []mcp.ContentBlock{{Type: "text", Text: "some result"}},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	engine, err := keep.Load("testdata/rules")
	if err != nil {
		t.Fatalf("keep.Load: %v", err)
	}
	t.Cleanup(engine.Close)

	routes := []relayconfig.Route{{Scope: "test-response-scope", Upstream: srv.URL}}
	router, err := NewRouter(context.Background(), routes)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	handler := NewRelayHandler(engine, router, nil)

	// write_query should be denied on request side (no direction guard)
	_, err = handler.HandleToolCall(context.Background(), "write_query", map[string]any{"query": "DELETE FROM users"})
	if err == nil {
		t.Fatal("expected deny error for write_query")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected read-only message, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestHandler_Response -v ./internal/relay'`
Expected: FAIL (response redaction not implemented yet)

- [ ] **Step 3: Implement response-side evaluation**

Update `internal/relay/handler.go` `HandleToolCall` method. After the upstream returns successfully (step 6), add response evaluation:

```go
// HandleToolCall implements mcp.Handler.
func (h *RelayHandler) HandleToolCall(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	// 1. Lookup route
	route, err := h.router.Lookup(name)
	if err != nil {
		return nil, err
	}

	// 2. Build Keep call (request side)
	call := keep.Call{
		Operation: name,
		Params:    args,
		Context: keep.CallContext{
			AgentID:   "relay",
			Timestamp: time.Now(),
			Scope:     route.Scope,
			Direction: "request",
		},
	}

	// 3. Evaluate request policy
	result, err := h.engine.Evaluate(call, route.Scope)
	if err != nil {
		return nil, fmt.Errorf("policy evaluation error: %w", err)
	}

	// 4. Log request audit entry
	if h.logger != nil {
		h.logger.Log(result.Audit)
	}

	// 5. Handle request decision
	switch result.Decision {
	case keep.Deny:
		msg := result.Message
		if msg == "" {
			msg = "Denied by policy"
		}
		return nil, fmt.Errorf("policy denied: %s (rule: %s)", msg, result.Rule)

	case keep.Redact:
		args = keep.ApplyMutations(args, result.Mutations)
	}

	// 6. Forward to upstream
	toolResult, err := route.Client.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}

	// 7. Evaluate response policy
	toolResult, err = h.evaluateResponse(name, route.Scope, toolResult)
	if err != nil {
		return nil, err
	}

	return toolResult, nil
}

// evaluateResponse runs policy evaluation on the upstream response.
func (h *RelayHandler) evaluateResponse(name, scope string, toolResult *mcp.ToolCallResult) (*mcp.ToolCallResult, error) {
	if toolResult == nil || len(toolResult.Content) == 0 {
		return toolResult, nil
	}

	// Join all text content blocks for evaluation
	var texts []string
	for _, block := range toolResult.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	if len(texts) == 0 {
		return toolResult, nil
	}

	joined := strings.Join(texts, "\n")

	call := keep.Call{
		Operation: name,
		Params:    map[string]any{"content": joined},
		Context: keep.CallContext{
			AgentID:   "relay",
			Timestamp: time.Now(),
			Scope:     scope,
			Direction: "response",
		},
	}

	result, err := h.engine.Evaluate(call, scope)
	if err != nil {
		return nil, fmt.Errorf("response policy evaluation error: %w", err)
	}

	if h.logger != nil {
		h.logger.Log(result.Audit)
	}

	switch result.Decision {
	case keep.Deny:
		msg := result.Message
		if msg == "" {
			msg = "Denied by policy"
		}
		return nil, fmt.Errorf("response policy denied: %s (rule: %s)", msg, result.Rule)

	case keep.Redact:
		mutated := keep.ApplyMutations(map[string]any{"content": joined}, result.Mutations)
		redacted, _ := mutated["content"].(string)

		// If there was only one text block, replace its text directly.
		// If multiple, replace the first block with the full redacted text
		// and clear the rest (the join merged them).
		if len(texts) == 1 {
			for i := range toolResult.Content {
				if toolResult.Content[i].Type == "text" {
					toolResult.Content[i].Text = redacted
					break
				}
			}
		} else {
			first := true
			for i := range toolResult.Content {
				if toolResult.Content[i].Type == "text" {
					if first {
						toolResult.Content[i].Text = redacted
						first = false
					} else {
						toolResult.Content[i].Text = ""
					}
				}
			}
		}
	}

	return toolResult, nil
}
```

Add `"strings"` to the imports in `handler.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit ARGS='-run TestHandler -v ./internal/relay'`
Expected: All handler tests PASS (both new and existing)

- [ ] **Step 5: Commit**

```bash
git add internal/relay/handler.go internal/relay/handler_test.go internal/relay/testdata/rules/test-tools-response.yaml
git commit -m "feat(relay): add response-side policy evaluation"
```

---

### Task 4: MCP Engine Tests

Add engine-level tests for MCP rule patterns (read-only enforcement, response redaction). These use inline rule configs, no file dependencies.

**Files:**
- Create: `internal/engine/mcp_toolcall_test.go`

- [ ] **Step 1: Write MCP tool call tests**

Create `internal/engine/mcp_toolcall_test.go`:

```go
package engine

import (
	"strings"
	"testing"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
)

func makeMCPReadOnlyEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:    "block-writes",
			Action:  config.ActionDeny,
			Match:   config.Match{Operation: "write_query"},
			Message: "Database is read-only.",
		},
	}
	ev, err := NewEvaluator(env, "test", config.ModeEnforce, config.ErrorModeClosed, rules, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func makeMCPResponseRedactEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	env, err := keepcel.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{
		{
			Name:   "redact-passwords",
			Action: config.ActionRedact,
			Match: config.Match{
				Operation: "read_query",
				When:      "context.direction == 'response'",
			},
			Redact: &config.RedactSpec{
				Target: "params.content",
				Patterns: []config.RedactPattern{
					{Match: "hunter2|p@ssw0rd!|letmein123", Replace: "********"},
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

func makeMCPCall(operation string, params map[string]any, direction string) Call {
	return Call{
		Operation: operation,
		Params:    params,
		Context:   CallContext{Scope: "test", Direction: direction},
	}
}

func TestMCPToolCall_WriteQueryDenied(t *testing.T) {
	ev := makeMCPReadOnlyEvaluator(t)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"INSERT", map[string]any{"query": "INSERT INTO users (name) VALUES ('test')"}},
		{"UPDATE", map[string]any{"query": "UPDATE users SET role='admin' WHERE id=1"}},
		{"DELETE", map[string]any{"query": "DELETE FROM users WHERE id=1"}},
		{"DROP TABLE", map[string]any{"query": "DROP TABLE users"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ev.Evaluate(makeMCPCall("write_query", tc.args, "request"))
			if result.Decision != Deny {
				t.Errorf("expected deny, got %s", result.Decision)
			}
		})
	}
}

func TestMCPToolCall_ReadQueryAllowed(t *testing.T) {
	ev := makeMCPReadOnlyEvaluator(t)

	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{"query": "SELECT * FROM users"}, "request"))
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

func TestMCPToolCall_ResponseRedaction(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	content := `[{"id":1,"name":"Alice","password":"hunter2"},{"id":2,"name":"Bob","password":"p@ssw0rd!"}]`
	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{"content": content}, "response"))

	if result.Decision != Redact {
		t.Errorf("expected redact, got %s", result.Decision)
	}
	if len(result.Mutations) == 0 {
		t.Fatal("expected mutations")
	}
	replaced := result.Mutations[0].Replaced
	if strings.Contains(replaced, "hunter2") || strings.Contains(replaced, "p@ssw0rd!") {
		t.Errorf("redacted content still contains passwords: %s", replaced)
	}
	if !strings.Contains(replaced, "********") {
		t.Errorf("redacted content missing placeholder: %s", replaced)
	}
}

func TestMCPToolCall_ResponseRedaction_RequestSideIgnored(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	// The redact rule has direction == 'response', so request-side calls should not redact
	content := "hunter2"
	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{"content": content}, "request"))

	if result.Decision == Redact {
		t.Error("redact rule should not fire on request direction")
	}
}

func TestMCPToolCall_ResponseRedaction_NoPasswords(t *testing.T) {
	ev := makeMCPResponseRedactEvaluator(t)

	content := `[{"id":1,"name":"Alice","role":"admin"}]`
	result := ev.Evaluate(makeMCPCall("read_query", map[string]any{"content": content}, "response"))

	if result.Decision == Redact && len(result.Mutations) > 0 {
		t.Errorf("expected no redaction for content without passwords, got mutations")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `make test-unit ARGS='-run TestMCPToolCall -v ./internal/engine'`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/mcp_toolcall_test.go
git commit -m "test(engine): add MCP tool call policy tests"
```

---

### Task 5: Demo Files

Create the demo script, config, rules, and seed data.

**Files:**
- Create: `examples/mcp-relay-demo/demo.sh`
- Create: `examples/mcp-relay-demo/relay.yaml`
- Create: `examples/mcp-relay-demo/rules/demo.yaml`
- Create: `examples/mcp-relay-demo/seed.sql`

- [ ] **Step 1: Create seed.sql**

Create `examples/mcp-relay-demo/seed.sql`:

```sql
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL,
  password TEXT NOT NULL,
  role TEXT NOT NULL
);

INSERT INTO users VALUES (1,  'Alice Chen',     'alice@company.com',   'hunter2',      'admin');
INSERT INTO users VALUES (2,  'Bob Park',       'bob@company.com',     'p@ssw0rd!',    'editor');
INSERT INTO users VALUES (3,  'Carol White',    'carol@company.com',   'letmein123',   'viewer');
INSERT INTO users VALUES (4,  'Dan Rivera',     'dan@company.com',     'qwerty456',    'editor');
INSERT INTO users VALUES (5,  'Eve Foster',     'eve@company.com',     'trustno1!',    'admin');
INSERT INTO users VALUES (6,  'Frank Zhao',     'frank@company.com',   'baseball9',    'viewer');
INSERT INTO users VALUES (7,  'Grace Kim',      'grace@company.com',   'shadow99!',    'editor');
INSERT INTO users VALUES (8,  'Hank Patel',     'hank@company.com',    'dragon123',    'viewer');
INSERT INTO users VALUES (9,  'Iris Novak',     'iris@company.com',    'master!42',    'admin');
INSERT INTO users VALUES (10, 'Jack Torres',    'jack@company.com',    'abc123xyz',    'editor');
INSERT INTO users VALUES (11, 'Karen Liu',      'karen@company.com',   'welcome1!',    'viewer');
INSERT INTO users VALUES (12, 'Leo Santos',     'leo@company.com',     'passw0rd!',    'editor');
```

- [ ] **Step 2: Create rules/demo.yaml**

Create `examples/mcp-relay-demo/rules/demo.yaml`:

```yaml
scope: demo-sqlite
mode: enforce

rules:
  # Block all write operations — database is read-only
  - name: block-writes
    match:
      operation: "write_query"
    action: deny
    message: "Database is read-only. Write operations are not permitted."

  # Redact plaintext passwords from query results
  - name: redact-passwords
    match:
      operation: "read_query"
      when: "context.direction == 'response'"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "hunter2|p@ssw0rd!|letmein123|qwerty456|trustno1!|baseball9|shadow99!|dragon123|master!42|abc123xyz|welcome1!|passw0rd!"
          replace: "********"

  # Log everything
  - name: audit-all
    match:
      operation: "*"
    action: log
```

- [ ] **Step 3: Create relay.yaml**

Create `examples/mcp-relay-demo/relay.yaml`:

```yaml
listen: ":19090"
rules_dir: RULES_DIR

routes:
  - scope: demo-sqlite
    command: COMMAND
    args: ARGS

log:
  format: json
  output: LOG_OUTPUT
```

- [ ] **Step 4: Create demo.sh**

Create `examples/mcp-relay-demo/demo.sh`:

```bash
#!/usr/bin/env bash
#
# Keep MCP Relay Demo
#
# Runs the keep-mcp-relay in front of a sqlite MCP server and demonstrates
# policy enforcement (read-only mode + password redaction).
#
# Prerequisites:
#   - npx (Node.js) or bunx (Bun)
#   - sqlite3
#
# Usage:
#   ./examples/mcp-relay-demo/demo.sh
#
set -euo pipefail

# ── Colors ───────────────────────────────────────────────────────
BOLD='\033[1m'
DIM='\033[2m'
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
RELAY_PORT=19090
RELAY_PID=""

cleanup() {
  [ -n "$RELAY_PID" ] && kill "$RELAY_PID" 2>/dev/null || true
}
trap cleanup EXIT

# ── Check prerequisites ──────────────────────────────────────────
if ! command -v sqlite3 &>/dev/null; then
  echo -e "${RED}Error:${RESET} sqlite3 is required but not found."
  exit 1
fi

NPX=""
if command -v npx &>/dev/null; then
  NPX="npx"
elif command -v bunx &>/dev/null; then
  NPX="bunx"
else
  echo -e "${RED}Error:${RESET} npx or bunx is required but not found."
  exit 1
fi

echo ""
echo -e "${BOLD}Keep MCP Relay Demo${RESET}"
echo -e "${DIM}Policy enforcement for MCP tool calls${RESET}"
echo ""

# ── Build ─────────────────────────────────────────────────────────
echo -e "${DIM}Building relay...${RESET}"
go build -o "$DEMO_DIR/keep-mcp-relay" ./cmd/keep-mcp-relay

# ── Create and seed database ─────────────────────────────────────
DB_PATH="$DEMO_DIR/demo.db"
sqlite3 "$DB_PATH" < "$SCRIPT_DIR/seed.sql"
echo -e "${DIM}Database seeded with 12 users${RESET}"

# ── Start relay ──────────────────────────────────────────────────
sed \
  -e "s|RULES_DIR|$SCRIPT_DIR/rules|" \
  -e "s|LOG_OUTPUT|$DEMO_DIR/audit.jsonl|" \
  -e "s|COMMAND|$NPX|" \
  -e "s|ARGS|[\"-y\", \"@modelcontextprotocol/server-sqlite\", \"$DB_PATH\"]|" \
  "$SCRIPT_DIR/relay.yaml" > "$DEMO_DIR/relay.yaml"

"$DEMO_DIR/keep-mcp-relay" --config "$DEMO_DIR/relay.yaml" >/dev/null 2>&1 &
RELAY_PID=$!
sleep 2

echo -e "${GREEN}Relay running${RESET} on :${RELAY_PORT} ${DIM}(PID $RELAY_PID)${RESET}"
echo ""

# ── Print instructions ───────────────────────────────────────────
echo -e "${BOLD}How to connect Claude:${RESET}"
echo ""
echo -e "  Add this to your Claude MCP settings ${DIM}(claude mcp add)${RESET}:"
echo ""
echo -e "    ${CYAN}claude mcp add keep-demo --transport http http://localhost:${RELAY_PORT}${RESET}"
echo ""
echo -e "  Then try these prompts:"
echo ""
echo -e "    ${DIM}1.${RESET} ${BOLD}\"List all users in the database\"${RESET}"
echo -e "       ${DIM}→ Passwords will be redacted to ********${RESET}"
echo ""
echo -e "    ${DIM}2.${RESET} ${BOLD}\"Add a new user named Test User\"${RESET}"
echo -e "       ${DIM}→ Write will be denied: Database is read-only${RESET}"
echo ""
echo -e "  ${BOLD}Audit log:${RESET} $DEMO_DIR/audit.jsonl"
echo ""
echo -e "${DIM}Press Ctrl+C to stop the relay${RESET}"

# Wait for relay to exit
wait "$RELAY_PID" 2>/dev/null || true
```

- [ ] **Step 5: Make demo.sh executable**

```bash
chmod +x examples/mcp-relay-demo/demo.sh
```

- [ ] **Step 6: Run the full test suite**

Run: `make test-unit`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add examples/mcp-relay-demo/
git commit -m "feat(demo): add MCP relay demo with sqlite backend"
```

---

### Task 6: Config Validation Tests

Update the relay config validation tests for the new `Command`/`Args` fields.

**Files:**
- Modify: `internal/relay/config/config_test.go` (if it exists)

- [ ] **Step 1: Check for existing config tests**

Run: `ls internal/relay/config/*_test.go`

- [ ] **Step 2: Add validation tests**

Add tests verifying:
- Route with `command` and no `upstream` is valid
- Route with both `command` and `upstream` is rejected
- Route with neither `command` nor `upstream` is rejected
- Route with `command` and `args` is valid

```go
func TestValidate_CommandRoute(t *testing.T) {
	cfg := &RelayConfig{
		Listen:   ":9090",
		RulesDir: "/tmp/rules",
		Routes: []Route{
			{Scope: "test", Command: "npx", Args: []string{"-y", "some-mcp-server"}},
		},
	}
	if err := cfg.validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}
}

func TestValidate_BothUpstreamAndCommand(t *testing.T) {
	cfg := &RelayConfig{
		Listen:   ":9090",
		RulesDir: "/tmp/rules",
		Routes: []Route{
			{Scope: "test", Upstream: "http://localhost:8080", Command: "npx"},
		},
	}
	if err := cfg.validate(); err == nil {
		t.Error("expected error for route with both upstream and command")
	}
}

func TestValidate_NeitherUpstreamNorCommand(t *testing.T) {
	cfg := &RelayConfig{
		Listen:   ":9090",
		RulesDir: "/tmp/rules",
		Routes: []Route{
			{Scope: "test"},
		},
	}
	if err := cfg.validate(); err == nil {
		t.Error("expected error for route with neither upstream nor command")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `make test-unit ARGS='-run TestValidate -v ./internal/relay/config'`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/relay/config/
git commit -m "test(relay): add config validation tests for command routes"
```
