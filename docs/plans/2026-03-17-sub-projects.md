# Keep -- Sub-Project Decomposition

**Master build plan for the Keep policy engine.**

---

## Sub-Projects

Keep breaks into seven sub-projects with a clear dependency chain. Each is independently plannable, testable, and shippable.

```
                    docs/content (independent)
                         |
  config ──> engine ──> keep CLI
                  \
                   ├──> keep-mcp-relay
                   └──> keep-llm-gateway
```

### Build order

| Phase | Sub-Project | Depends On | Milestone |
|-------|------------|------------|-----------|
| 1 | Config | — | M0 |
| 2 | Engine | Config | M0 |
| 3 | `keep` CLI | Engine | M0 |
| 4 | `keep-mcp-relay` | Engine | M0 |
| 5 | `keep-llm-gateway` | Engine | M1 |
| 6 | docs/content | All (for accuracy) | M0-M1 |

Phases 1-4 are M0. Phase 5 is M1. Phase 6 runs in parallel throughout.

---

## 1. Config

**What:** YAML parsing for rule files, profiles, and starter packs. Pure data structures, validation, and loading. No evaluation.

**Boundary:** Takes a directory path, returns parsed and validated Go structs. Does not compile CEL expressions -- that's the engine's job. Does not know about transport (no relay/gateway config here).

**Packages:**
- `internal/config` -- rule file, profile, and starter pack parsing
- `internal/config/testdata/` -- fixture YAML files for tests

**Key types:**
- `RuleFile` (scope, mode, on_error, profile ref, pack refs, rules)
- `Rule` (name, description, match, action, message, redact block)
- `Match` (operation glob, when expression string)
- `RedactSpec` (target path, patterns)
- `Profile` (name, aliases map)
- `StarterPack` (name, profile ref, rules)
- `PackRef` (name, overrides map)

**Validates:**
- YAML syntax
- Required fields
- Name formats (`[a-z][a-z0-9-]*`, max 64)
- Scope uniqueness
- Rule name uniqueness within scope
- Profile alias format (`[a-z][a-z0-9_]*`, max 32)
- Aliases target `params.*` only
- RE2 pattern compilation (redact patterns)
- Pack references resolve
- Override targets exist in referenced pack
- Expression size limit (max 2048 chars, string length only -- no CEL compilation)
- Field path syntax (dot-separated identifiers)

**Does not validate:** CEL expression correctness (deferred to engine).

**Delivers:** Parsed, validated structs ready for the engine to consume. Errors include file path, line context, and actionable messages.

---

## 2. Engine

**What:** The core policy engine. Compiles CEL expressions, evaluates calls against rules, manages rate counters, produces audit entries. This is the `keep` Go module's public API.

**Boundary:** Takes parsed config structs (from sub-project 1) and a `Call`, returns an `EvalResult`. Does not do I/O beyond initial file loading (delegates to config package). Does not know about MCP, HTTP, or LLM protocols.

**Packages:**
- `keep.go` (top-level public API: `Load`, `Engine`, `Evaluate`, `ApplyMutations`)
- `internal/engine/` -- evaluation logic, scope indexing, rule matching
- `internal/cel/` -- CEL environment setup, custom functions, expression compilation
- `internal/rate/` -- in-memory sliding window counter store
- `internal/redact/` -- regex-based field redaction
- `internal/audit/` -- `AuditEntry` construction (not output -- callers handle that)

**Public API (from PRD v0.5):**
- `Load(rulesDir string, opts ...Option) (*Engine, error)`
- `(*Engine).Evaluate(call Call, scope string) (EvalResult, error)`
- `(*Engine).Reload() error`
- `(*Engine).Scopes() []string`
- `ApplyMutations(params map[string]any, mutations []Mutation) map[string]any`
- Types: `Call`, `Context`, `EvalResult`, `Decision`, `Mutation`, `AuditEntry`, `RuleResult`

**CEL environment:**
- Standard: field access, comparison, logic, collections, string functions
- Custom: `inTimeWindow()`, `dayOfWeek()`, `containsAny()`, `containsPII()`, `containsPHI()` (stub), `rateCount()`, `estimateTokens()`
- Profile alias resolution (rewrite identifiers before compilation)
- Null-safe field access

**Rate counter store:**
- In-memory, per-engine instance
- Sliding window with configurable max (24h)
- Periodic GC (60s)
- Thread-safe

**Evaluation flow:**
1. Look up scope
2. Collect rules (pack rules first, then inline)
3. Filter by operation glob
4. Evaluate `when` expressions
5. Short-circuit on deny
6. Accumulate redactions
7. Record log matches
8. Build `AuditEntry`
9. Return `EvalResult`

**Error handling:**
- Load-time: fatal (return error, no engine)
- Eval-time: per-scope `on_error` (closed/open)

---

## 3. `keep` CLI

**What:** Command-line tool for validating rules and testing policy. Imports the engine.

**Boundary:** CLI layer only. All logic is in the engine. The CLI handles argument parsing, output formatting, and exit codes.

**Package:**
- `cmd/keep/` -- Cobra command tree

**Commands:**

`keep validate <rules-dir> [--profiles <dir>] [--packs <dir>]`
- Loads rules via `keep.Load()`, reports errors or success
- Output: file-by-file validation report
- Exit: 0 success, 1 validation errors, 2 filesystem errors

`keep test <rules-dir> --fixtures <path> [--profiles <dir>] [--packs <dir>]`
- Loads rules, loads fixture YAML files, evaluates each test case
- Fixture format: YAML with `scope`, `tests[]` containing `name`, `call`, `expect`
- Output: per-test PASS/FAIL with details on failure
- Exit: 0 all pass, 1 any fail, 2 load errors
- Always evaluates in enforce mode regardless of scope's `mode` setting

---

## 4. `keep-mcp-relay`

**What:** MCP proxy that sits between agent and upstream MCP servers. Evaluates policy on every tool call.

**Boundary:** MCP transport only. Uses the engine for policy evaluation. Handles MCP protocol (Streamable HTTP), upstream connections, tool discovery, and routing.

**Packages:**
- `cmd/keep-mcp-relay/` -- binary entry point, config loading
- `internal/relay/` -- MCP transport, upstream management, routing table
- `internal/relay/config/` -- relay-specific config parsing (`keep-mcp-relay.yaml`)

**Key behaviors:**
- Startup: connect to all upstreams, discover tools, build routing table
- Tool name collision: fail to start with explicit error
- Tool call: look up upstream + scope, build `Call`, evaluate, forward or deny
- Deny: return MCP error with rule name + message
- Redact: mutate tool input, forward to upstream
- MCP scope: tools/list, tools/call, capability negotiation (no resources/prompts/sampling)
- Audit: write JSON Lines to configured output (stdout default)
- Upstream health: log warning if unreachable at startup, MCP error if unreachable during operation

**Config:**
```yaml
listen: ":8090"
rules_dir: "./rules"
profiles_dir: "./profiles"     # optional
packs_dir: "./starter-packs"   # optional
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
    auth:
      type: bearer
      token_env: "LINEAR_API_KEY"
log:
  format: json
  output: stdout
```

---

## 5. `keep-llm-gateway`

**What:** HTTP proxy between agent runtime and LLM provider. Decomposes message payloads into per-block calls for flat rule evaluation.

**Boundary:** LLM transport only. Uses the engine for policy evaluation. Handles HTTP proxying, message decomposition, payload reassembly, and bidirectional filtering.

**Packages:**
- `cmd/keep-llm-gateway/` -- binary entry point, config loading
- `internal/gateway/` -- HTTP proxy, decomposition, reassembly
- `internal/gateway/anthropic/` -- Anthropic messages API decomposer
- `internal/gateway/config/` -- gateway-specific config parsing

**Key behaviors:**
- Request: decompose into `llm.request` summary + per-block `llm.tool_result` calls
- Response: decompose into `llm.response` summary + per-block `llm.tool_use` calls
- Deny of any decomposed call: block entire request/response
- Redact: patch mutated content back into payload before forwarding
- Direction: request = agent-to-model, response = model-to-agent
- Provider: Anthropic at launch, OpenAI as fast follow (M3)

**Config:**
```yaml
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway
decompose:
  tool_result: true
  tool_use: true
  text: false
  request_summary: true
  response_summary: true
log:
  format: json
  output: stdout
```

---

## 6. docs/content

**What:** Documentation for majorcontext.com/keep. Markdown files with YAML frontmatter, auto-hosted.

**Boundary:** Content only. No code. Follows the style guide in `docs/STYLE-GUIDE.md` and the same markdown format as the Moat docs.

**Structure:**
```
docs/content/
  getting-started/
    01-introduction.md        # What Keep is, who it's for
    02-installation.md        # Install keep, keep-mcp-relay, keep-llm-gateway
    03-first-policy.md        # Write a rule file, validate, test
    04-mcp-relay.md           # Run the relay with a real MCP server
  concepts/
    01-policy-engine.md       # How evaluation works, the Call object
    02-expressions.md         # CEL in Keep: what's available, how it works
    03-deny-audit-tune.md     # The workflow: observation → enforcement
    04-integrations.md        # MCP relay, LLM gateway, library -- when to use which
    05-defense-in-depth.md    # Keep + Moat + agentsh layering
  guides/
    01-linear-mcp.md          # Policy for Linear MCP tools
    02-slack-mcp.md           # Policy for Slack MCP tools
    03-llm-gateway.md         # Filtering LLM requests/responses
    04-library-usage.md       # Using Keep as a Go library in agent code
    05-testing-policies.md    # Writing fixtures, running keep test
    06-profiles-packs.md      # Creating profiles and starter packs
  reference/
    01-cli.md                 # keep validate, keep test
    02-rule-files.md          # Complete rule file format
    03-expressions.md         # CEL reference (all functions, types, operators)
    04-relay-config.md        # keep-mcp-relay.yaml reference
    05-gateway-config.md      # keep-llm-gateway.yaml reference
    06-audit-log.md           # Audit log JSON schema
```

**Frontmatter format:**
```yaml
---
title: "Page title"
description: "One sentence for SEO and link previews."
keywords: ["keep", "relevant", "keywords"]
---
```

---

## Cross-Cutting Concerns

**Go module:** `github.com/majorcontext/keep`. Single module for the entire repo. CLIs are `cmd/keep`, `cmd/keep-mcp-relay`, `cmd/keep-llm-gateway`.

**Testing:** Each sub-project has its own unit tests. E2E tests (in `internal/e2e/`) test the full stack: load rules, evaluate calls, verify results. The `keep` CLI tests exercise validate and test commands against fixture directories. Relay and gateway get integration tests with mock MCP/HTTP servers.

**Starter content:** The Linear profile and starter pack ship as the first examples. They live in `profiles/linear.yaml` and `starter-packs/linear-safe-defaults.yaml` at the repo root. Additional profiles/packs are added with each guide.

**Linting:** golangci-lint v2 with default config. `go fmt` enforced.

**CI:** GitHub Actions. `make lint`, `make test-unit`, `make build` on every PR. E2E tests on merge to main.
