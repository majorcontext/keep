# `keep-llm-gateway` Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the LLM gateway that sits between agent runtimes and LLM providers, decomposing message payloads into per-block Keep calls for policy evaluation.

**Architecture:** An HTTP reverse proxy that intercepts requests to the LLM provider API. For each request/response, the gateway decomposes the message payload into flat per-block calls (one per tool_result, tool_use, plus a summary), evaluates each against the Keep engine, and forwards the (potentially mutated) payload or blocks the call. Bidirectional: filters both what the model sees and what the model wants to do.

**Tech Stack:** Go, `net/http` and `net/http/httputil` for proxying, `keep` engine package, Anthropic Messages API as the launch provider.

**Depends on:** Config (sub-project 1) and Engine (sub-project 2) must be complete. This is an M1 sub-project.

---

### Task 1: Gateway config parsing

**Files:**
- Create: `internal/gateway/config/config.go`
- Create: `internal/gateway/config/config_test.go`

- [ ] **Step 1: Write failing tests**

Tests for parsing `keep-llm-gateway.yaml`:
- `TestParse_Valid` -- full config parses
- `TestParse_MissingProvider` -- error
- `TestParse_MissingUpstream` -- error
- `TestParse_MissingScope` -- error
- `TestParse_DecomposeDefaults` -- omitted decompose section gets defaults (tool_result=true, tool_use=true, text=false, request_summary=true, response_summary=true)
- `TestParse_InvalidProvider` -- only "anthropic" and "openai" accepted

- [ ] **Step 2: Implement config**

```go
type GatewayConfig struct {
	Listen      string          `yaml:"listen"`
	RulesDir    string          `yaml:"rules_dir"`
	ProfilesDir string          `yaml:"profiles_dir,omitempty"`
	PacksDir    string          `yaml:"packs_dir,omitempty"`
	Provider    string          `yaml:"provider"`
	Upstream    string          `yaml:"upstream"`
	Scope       string          `yaml:"scope"`
	Decompose   DecomposeConfig `yaml:"decompose,omitempty"`
	Log         LogConfig       `yaml:"log,omitempty"`
}

type DecomposeConfig struct {
	ToolResult      *bool `yaml:"tool_result,omitempty"`      // default true
	ToolUse         *bool `yaml:"tool_use,omitempty"`         // default true
	Text            *bool `yaml:"text,omitempty"`             // default false
	RequestSummary  *bool `yaml:"request_summary,omitempty"`  // default true
	ResponseSummary *bool `yaml:"response_summary,omitempty"` // default true
}
```

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/gateway/
git commit -m "feat(gateway): add gateway config parsing"
```

---

### Task 2: Anthropic message types

**Files:**
- Create: `internal/gateway/anthropic/types.go`
- Create: `internal/gateway/anthropic/types_test.go`

- [ ] **Step 1: Define Anthropic Messages API types**

```go
// MessagesRequest is the Anthropic /v1/messages request body.
type MessagesRequest struct {
	Model     string    `json:"model"`
	System    any       `json:"system,omitempty"`    // string or []ContentBlock
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	// ... other fields passed through
}

type Message struct {
	Role    string         `json:"role"`
	Content any            `json:"content"` // string or []ContentBlock
}

type ContentBlock struct {
	Type       string         `json:"type"` // text, tool_use, tool_result
	Text       string         `json:"text,omitempty"`
	ID         string         `json:"id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	ToolUseID  string         `json:"tool_use_id,omitempty"`
	Content    any            `json:"content,omitempty"` // string or []ContentBlock
}

// MessagesResponse is the Anthropic /v1/messages response body.
type MessagesResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	// ... other fields passed through
}
```

- [ ] **Step 2: Write round-trip tests**

Marshal and unmarshal real Anthropic request/response payloads to verify types are correct.

- [ ] **Step 3: Commit**

```bash
git add internal/gateway/anthropic/
git commit -m "feat(gateway): add Anthropic Messages API types"
```

---

### Task 3: Request decomposition

**Files:**
- Create: `internal/gateway/anthropic/decompose.go`
- Create: `internal/gateway/anthropic/decompose_test.go`

- [ ] **Step 1: Write failing tests**

- `TestDecomposeRequest_Summary` -- request produces an `llm.request` call with model, system, token_estimate, tool_result_count
- `TestDecomposeRequest_ToolResults` -- two tool_result blocks produce two `llm.tool_result` calls with tool_name and content
- `TestDecomposeRequest_TextBlocks` -- text blocks skipped by default (decompose.text=false)
- `TestDecomposeRequest_TextBlocksEnabled` -- text blocks emitted when decompose.text=true
- `TestDecomposeRequest_NoToolResults` -- request with only text, only summary emitted

- [ ] **Step 2: Implement decomposition**

```go
// DecomposeRequest breaks an Anthropic Messages API request into
// flat Keep calls. Returns one llm.request summary + one call per
// content block (based on decompose config).
func DecomposeRequest(req *MessagesRequest, scope string, cfg DecomposeConfig) []keep.Call
```

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/gateway/anthropic/
git commit -m "feat(gateway): add Anthropic request decomposition"
```

---

### Task 4: Response decomposition

**Files:**
- Modify: `internal/gateway/anthropic/decompose.go`
- Create: `internal/gateway/anthropic/decompose_response_test.go`

- [ ] **Step 1: Write failing tests**

- `TestDecomposeResponse_Summary` -- response produces an `llm.response` call with stop_reason and tool_use_count
- `TestDecomposeResponse_ToolUse` -- tool_use block produces an `llm.tool_use` call with name and input
- `TestDecomposeResponse_MultipleBlocks` -- text + tool_use, only tool_use decomposed (text=false)
- `TestDecomposeResponse_Direction` -- all calls have `context.direction = "response"`

- [ ] **Step 2: Implement response decomposition**

```go
// DecomposeResponse breaks an Anthropic Messages API response into
// flat Keep calls. Returns one llm.response summary + one call per
// content block.
func DecomposeResponse(resp *MessagesResponse, scope string, cfg DecomposeConfig) []keep.Call
```

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/gateway/anthropic/
git commit -m "feat(gateway): add Anthropic response decomposition"
```

---

### Task 5: Payload reassembly after redaction

**Files:**
- Create: `internal/gateway/anthropic/reassemble.go`
- Create: `internal/gateway/anthropic/reassemble_test.go`

- [ ] **Step 1: Write failing tests**

- `TestReassembleRequest_NoMutations` -- original request unchanged
- `TestReassembleRequest_RedactToolResult` -- tool_result content replaced with redacted value
- `TestReassembleRequest_MultipleMutations` -- two tool_results redacted, both patched
- `TestReassembleResponse_RedactToolUse` -- tool_use input patched with redacted values

- [ ] **Step 2: Implement reassembly**

```go
// ReassembleRequest patches redaction mutations back into the original
// Anthropic request payload. Returns a new request with mutated content.
func ReassembleRequest(req *MessagesRequest, results []BlockResult) *MessagesRequest

// ReassembleResponse patches mutations into the response payload.
func ReassembleResponse(resp *MessagesResponse, results []BlockResult) *MessagesResponse

// BlockResult pairs a decomposed call with its evaluation result.
type BlockResult struct {
	BlockIndex int            // index into the content blocks
	Call       keep.Call
	Result     keep.EvalResult
}
```

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/gateway/anthropic/
git commit -m "feat(gateway): add payload reassembly after redaction"
```

---

### Task 6: HTTP proxy handler

**Files:**
- Create: `internal/gateway/proxy.go`
- Create: `internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing tests**

- `TestProxy_AllowRequest` -- request passes policy, forwarded to upstream, response returned
- `TestProxy_DenyRequest` -- request denied (e.g., context too large), returns HTTP 400 with structured error
- `TestProxy_RedactRequest` -- tool_result redacted, upstream receives mutated payload
- `TestProxy_DenyResponse` -- response tool_use denied (e.g., destructive bash), returns HTTP 400
- `TestProxy_RedactResponse` -- response content redacted before returning to agent
- `TestProxy_PassthroughNonMessages` -- non-/v1/messages requests forwarded without evaluation
- `TestProxy_AuditLogged` -- every evaluation logged

Use `httptest.NewServer` for mock Anthropic upstream.

- [ ] **Step 2: Implement proxy handler**

```go
// Proxy is the LLM gateway HTTP handler.
type Proxy struct {
	engine   *keep.Engine
	scope    string
	upstream *url.URL
	decompose DecomposeConfig
	logger   audit.Logger
}

func NewProxy(engine *keep.Engine, cfg *config.GatewayConfig, logger *audit.Logger) (*Proxy, error)

// ServeHTTP intercepts /v1/messages requests, decomposes, evaluates,
// and forwards. All other paths are reverse-proxied without evaluation.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

Request flow:
1. Read and parse request body as `MessagesRequest`
2. Decompose into Keep calls
3. Evaluate each call against the engine
4. If any deny: return HTTP 400 with JSON error (rule name, message)
5. If any redact: reassemble request with mutations
6. Forward to upstream
7. Read and parse response as `MessagesResponse`
8. Decompose response into Keep calls
9. Evaluate each call
10. If any deny: return HTTP 400 (block the response)
11. If any redact: reassemble response with mutations
12. Return (potentially mutated) response to agent
13. Log all audit entries

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/gateway/
git commit -m "feat(gateway): add HTTP proxy with bidirectional policy evaluation"
```

---

### Task 7: Binary entry point and integration test

**Files:**
- Create: `cmd/keep-llm-gateway/main.go`
- Create: `internal/gateway/gateway_test.go`

- [ ] **Step 1: Implement main**

Similar structure to the relay: parse flags, load config, load engine, create proxy, start HTTP server, handle signals.

- [ ] **Step 2: Write integration test**

Create `internal/gateway/gateway_test.go` (build tag `//go:build e2e`):
- Start a mock Anthropic upstream
- Start the gateway with rules that redact AWS keys and deny destructive bash
- Send a request with a tool_result containing an AWS key -- verify upstream receives redacted content
- Send a request that produces a response with `rm -rf` tool_use -- verify the agent receives a deny error
- Verify audit logs contain all evaluations

- [ ] **Step 3: Build and commit**

```bash
git add cmd/keep-llm-gateway/ internal/gateway/
git commit -m "feat(gateway): add keep-llm-gateway binary and integration test"
```
