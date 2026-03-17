# `keep-mcp-relay` Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the MCP relay that proxies tool calls between an agent and multiple upstream MCP servers, evaluating Keep policy on every call.

**Architecture:** A single HTTP server that speaks MCP Streamable HTTP. At startup, connects to all configured upstreams, discovers tools, and builds a routing table. On each tool call: look up upstream + scope, build a `keep.Call`, evaluate against the engine, and forward or deny. Thin transport shell over the Keep engine.

**Tech Stack:** Go, `net/http` for Streamable HTTP, `keep` engine package, `github.com/spf13/cobra` for the binary entry point.

**Depends on:** Config (sub-project 1) and Engine (sub-project 2) must be complete.

---

### Task 1: Relay config parsing

**Files:**
- Create: `internal/relay/config/config.go`
- Create: `internal/relay/config/config_test.go`
- Create: `internal/relay/config/testdata/valid.yaml`
- Create: `internal/relay/config/testdata/missing-routes.yaml`

- [ ] **Step 1: Write failing tests**

Create `internal/relay/config/config_test.go`:
- `TestParse_Valid` -- parses a complete relay config
- `TestParse_MissingListen` -- returns error
- `TestParse_MissingRulesDir` -- returns error
- `TestParse_MissingRoutes` -- returns error
- `TestParse_EmptyRoutes` -- returns error
- `TestParse_RouteMissingScope` -- returns error
- `TestParse_RouteMissingUpstream` -- returns error
- `TestParse_AuthTypes` -- bearer, header, passthrough all parse

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestParse -v ./internal/relay/config/'`
Expected: FAIL

- [ ] **Step 3: Implement config parsing**

Create `internal/relay/config/config.go`:

```go
package config

type RelayConfig struct {
	Listen      string  `yaml:"listen"`
	RulesDir    string  `yaml:"rules_dir"`
	ProfilesDir string  `yaml:"profiles_dir,omitempty"`
	PacksDir    string  `yaml:"packs_dir,omitempty"`
	Routes      []Route `yaml:"routes"`
	Log         LogConfig `yaml:"log,omitempty"`
}

type Route struct {
	Scope    string `yaml:"scope"`
	Upstream string `yaml:"upstream"`
	Auth     *Auth  `yaml:"auth,omitempty"`
}

type Auth struct {
	Type     string `yaml:"type"`      // bearer, header, passthrough
	TokenEnv string `yaml:"token_env,omitempty"`
	Header   string `yaml:"header,omitempty"`
}

type LogConfig struct {
	Format string `yaml:"format,omitempty"` // json (default) | text
	Output string `yaml:"output,omitempty"` // stdout (default) | stderr | file path
}

func Load(path string) (*RelayConfig, error)
```

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestParse -v ./internal/relay/config/'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/
git commit -m "feat(relay): add relay config parsing"
```

---

### Task 2: MCP protocol -- Streamable HTTP transport (client)

**Files:**
- Create: `internal/relay/mcp/client.go`
- Create: `internal/relay/mcp/client_test.go`
- Create: `internal/relay/mcp/types.go`

- [ ] **Step 1: Define MCP types**

Create `internal/relay/mcp/types.go` with the MCP JSON-RPC types needed:
- `JSONRPCRequest`, `JSONRPCResponse`, `JSONRPCError`
- `InitializeParams`, `InitializeResult`, `ServerCapabilities`
- `Tool` (name, description, input schema)
- `ToolCallParams` (name, arguments), `ToolCallResult` (content, isError)
- `ListToolsResult`

- [ ] **Step 2: Write failing tests for MCP client**

Create `internal/relay/mcp/client_test.go`:
- `TestClient_Initialize` -- connects to a test HTTP server, sends initialize, gets capabilities
- `TestClient_ListTools` -- sends tools/list, gets tool list
- `TestClient_CallTool` -- sends tools/call, gets result
- `TestClient_CallTool_Error` -- upstream returns error, client surfaces it
- `TestClient_Unreachable` -- upstream not responding, returns error

Use `httptest.NewServer` for the test MCP server.

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestClient'`
Expected: FAIL

- [ ] **Step 4: Implement MCP client**

Create `internal/relay/mcp/client.go`:

```go
// Client connects to a single upstream MCP server via Streamable HTTP.
type Client struct { /* unexported */ }

// NewClient creates a client for the given upstream URL.
func NewClient(upstream string, auth *config.Auth) *Client

// Initialize performs the MCP initialize handshake.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error)

// ListTools calls tools/list and returns the tool list.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error)

// CallTool calls tools/call with the given name and arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
```

MCP Streamable HTTP: POST JSON-RPC to the upstream URL. Standard HTTP request/response (no SSE for M0 -- tool calls are synchronous request/response).

- [ ] **Step 5: Run tests**

Run: `make test-unit ARGS='-run TestClient'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/relay/mcp/
git commit -m "feat(relay): add MCP Streamable HTTP client"
```

---

### Task 3: Tool discovery and routing table

**Files:**
- Create: `internal/relay/router.go`
- Create: `internal/relay/router_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/relay/router_test.go`:
- `TestRouter_BuildTable` -- two upstreams with distinct tools, routing table maps each tool to its upstream + scope
- `TestRouter_ToolConflict` -- two upstreams register same tool name, returns error
- `TestRouter_LookupHit` -- look up a known tool, returns upstream + scope
- `TestRouter_LookupMiss` -- look up unknown tool, returns error
- `TestRouter_MergedToolList` -- returns union of all upstream tools
- `TestRouter_UpstreamDown` -- one upstream unreachable, its tools excluded, other upstream works

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestRouter'`
Expected: FAIL

- [ ] **Step 3: Implement router**

Create `internal/relay/router.go`:

```go
// Router maps tool names to upstream MCP clients and scopes.
type Router struct { /* unexported */ }

// Route is a single entry: tool name -> client + scope.
type Route struct {
	Client *mcp.Client
	Scope  string
	Tool   mcp.Tool
}

// NewRouter connects to all upstreams, discovers tools, and builds the
// routing table. Returns an error if any tool name appears in multiple upstreams.
func NewRouter(ctx context.Context, routes []config.Route) (*Router, error)

// Lookup returns the route for a tool name, or an error if not found.
func (r *Router) Lookup(toolName string) (*Route, error)

// Tools returns the merged list of all tools from all upstreams.
func (r *Router) Tools() []mcp.Tool
```

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestRouter'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/
git commit -m "feat(relay): add tool discovery and routing table"
```

---

### Task 4: MCP server -- Streamable HTTP transport

**Files:**
- Create: `internal/relay/mcp/server.go`
- Create: `internal/relay/mcp/server_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/relay/mcp/server_test.go`:
- `TestServer_Initialize` -- client sends initialize, server responds with capabilities
- `TestServer_ListTools` -- client sends tools/list, server responds with tool list
- `TestServer_CallTool` -- client sends tools/call, handler is invoked, result returned
- `TestServer_CallTool_Error` -- handler returns error, server returns MCP error
- `TestServer_InvalidMethod` -- unknown method returns method-not-found error
- `TestServer_InvalidJSON` -- malformed request returns parse error

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestServer'`
Expected: FAIL

- [ ] **Step 3: Implement MCP server**

Create `internal/relay/mcp/server.go`:

```go
// Handler processes MCP tool calls.
type Handler interface {
	HandleToolCall(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}

// Server is an MCP Streamable HTTP server.
type Server struct { /* unexported */ }

// NewServer creates a server with the given tools and handler.
func NewServer(tools []Tool, handler Handler) *Server

// ServeHTTP implements http.Handler. Accepts JSON-RPC requests,
// dispatches to the appropriate method, and returns JSON-RPC responses.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

Supports: `initialize`, `tools/list`, `tools/call`. Returns JSON-RPC errors for unknown methods.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestServer'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/mcp/
git commit -m "feat(relay): add MCP Streamable HTTP server"
```

---

### Task 5: Relay handler -- policy evaluation bridge

**Files:**
- Create: `internal/relay/handler.go`
- Create: `internal/relay/handler_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/relay/handler_test.go`:
- `TestHandler_Allow` -- tool call passes policy, forwarded to upstream, result returned
- `TestHandler_Deny` -- tool call denied by policy, MCP error returned with rule name and message, upstream never called
- `TestHandler_Redact` -- tool call redacted, mutated args forwarded to upstream
- `TestHandler_UnknownTool` -- tool not in routing table, MCP error returned
- `TestHandler_UpstreamError` -- policy passes but upstream returns error, error surfaced
- `TestHandler_AuditLogged` -- every call produces an audit entry regardless of decision

Use mock upstream (test MCP server) and real Keep engine with test rules.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestHandler'`
Expected: FAIL

- [ ] **Step 3: Implement handler**

Create `internal/relay/handler.go`:

```go
// RelayHandler bridges MCP tool calls to Keep policy evaluation.
type RelayHandler struct {
	engine *keep.Engine
	router *Router
	logger AuditLogger
}

// AuditLogger writes audit entries.
type AuditLogger interface {
	Log(entry keep.AuditEntry)
}

func NewRelayHandler(engine *keep.Engine, router *Router, logger AuditLogger) *RelayHandler

// HandleToolCall implements mcp.Handler.
func (h *RelayHandler) HandleToolCall(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	// 1. Look up route (tool -> upstream + scope)
	// 2. Build keep.Call{Operation: name, Params: args, Context: ...}
	// 3. Evaluate against engine
	// 4. If deny: return MCP error with rule name + message
	// 5. If redact: apply mutations to args
	// 6. Forward to upstream via route.Client.CallTool
	// 7. Log audit entry
	// 8. Return upstream result
}
```

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestHandler'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/
git commit -m "feat(relay): add policy evaluation handler for tool calls"
```

---

### Task 6: Audit logger -- JSON Lines output

**Files:**
- Create: `internal/audit/logger.go`
- Create: `internal/audit/logger_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/audit/logger_test.go`:
- `TestJSONLogger_Stdout` -- writes JSON Lines to a buffer, each line is valid JSON, fields match AuditEntry
- `TestJSONLogger_File` -- writes to a temp file, contents are valid JSON Lines
- `TestJSONLogger_Concurrent` -- 100 goroutines logging, no interleaved lines

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit ARGS='-run TestJSONLogger'`
Expected: FAIL

- [ ] **Step 3: Implement logger**

Create `internal/audit/logger.go`:

```go
// Package audit provides structured audit logging for Keep evaluations.
package audit

// Logger writes audit entries as JSON Lines.
type Logger struct { /* unexported, wraps io.Writer with a mutex */ }

// NewLogger creates a logger that writes to the given writer.
func NewLogger(w io.Writer) *Logger

// NewLoggerFromConfig creates a logger from a log config (stdout/stderr/file path).
func NewLoggerFromConfig(cfg LogConfig) (*Logger, error)

// Log serializes the audit entry as JSON and writes it as a single line.
func (l *Logger) Log(entry keep.AuditEntry)
```

Thread-safe: uses a mutex to prevent interleaved writes.

- [ ] **Step 4: Run tests**

Run: `make test-unit ARGS='-run TestJSONLogger'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/audit/
git commit -m "feat(audit): add JSON Lines audit logger"
```

---

### Task 7: Binary entry point -- `keep-mcp-relay`

**Files:**
- Create: `cmd/keep-mcp-relay/main.go`

- [ ] **Step 1: Implement main**

Create `cmd/keep-mcp-relay/main.go`:

```go
package main

// 1. Parse flags: --config (required)
// 2. Load relay config from YAML
// 3. Load Keep engine from config.rules_dir (with profiles/packs dirs)
// 4. Build router (connect to upstreams, discover tools)
// 5. Create audit logger
// 6. Create relay handler
// 7. Create MCP server with merged tool list and handler
// 8. Start HTTP server on config.listen
// 9. Handle signals (SIGINT, SIGTERM) for graceful shutdown
```

- [ ] **Step 2: Build**

Run: `go build -o keep-mcp-relay ./cmd/keep-mcp-relay`
Expected: binary builds

- [ ] **Step 3: Commit**

```bash
git add cmd/keep-mcp-relay/
git commit -m "feat(relay): add keep-mcp-relay binary entry point"
```

---

### Task 8: Integration test

**Files:**
- Create: `internal/relay/relay_test.go`
- Create: `internal/relay/testdata/rules/test-tools.yaml`
- Create: `internal/relay/testdata/relay-config.yaml`

- [ ] **Step 1: Write integration test**

Create `internal/relay/relay_test.go` (build tag `//go:build e2e`):
- Start a mock upstream MCP server with two tools: `allowed_tool` and `blocked_tool`
- Start the relay with rules that deny `blocked_tool`
- Connect to the relay as a client
- Call `allowed_tool` -- succeeds, returns upstream result
- Call `blocked_tool` -- returns MCP error with deny message
- Verify audit logs contain both evaluations

- [ ] **Step 2: Run integration test**

Run: `go test -tags=e2e -v ./internal/relay/ -run TestRelayIntegration`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/relay/
git commit -m "test(relay): add end-to-end integration test"
```
