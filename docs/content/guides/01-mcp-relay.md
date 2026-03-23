---
title: "Running the MCP relay"
navTitle: "MCP relay"
description: "Run Keep as an MCP proxy between agents and upstream MCP servers with policy enforcement."
keywords: ["keep", "mcp", "relay", "proxy", "tool calls", "policy"]
---

# Running the MCP relay

The MCP relay sits between an AI agent and one or more upstream Model Context Protocol (MCP) servers. Every tool call passes through the Keep policy engine before reaching the upstream, and every response is evaluated before returning to the agent.

This guide walks through configuring the relay, writing rules, and verifying that policy enforcement works.

## Prerequisites

- Keep installed (`keep-mcp-relay` binary on your `PATH`)
- An upstream MCP server to proxy to -- either an HTTP endpoint or a local command that speaks MCP over stdio

## Write the relay config

Create a file called `relay.yaml`. The relay needs a listen address, a rules directory, and at least one route.

```yaml
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
    auth:
      type: bearer
      token_env: "LINEAR_API_KEY"
  - scope: slack-tools
    upstream: "https://slack-mcp.example.com"
log:
  format: json
  output: stdout
```

Each route maps a **scope** to an upstream MCP server. The scope ties the route to a set of rules -- the engine evaluates rules whose scope matches the route's scope.

### Config fields

| Field | Required | Description |
|-------|----------|-------------|
| `listen` | Yes | Address to bind the relay's HTTP server (e.g. `:8090`) |
| `rules_dir` | Yes | Path to the directory containing rule files |
| `profiles_dir` | No | Path to profile YAML files |
| `packs_dir` | No | Path to starter pack files |
| `routes` | Yes | List of routes (at least one) |
| `log.format` | No | Log format, defaults to `json` |
| `log.output` | No | Log destination, defaults to `stdout` |

### Route fields

| Field | Required | Description |
|-------|----------|-------------|
| `scope` | Yes | Scope name that binds this route to a set of rules |
| `upstream` | One of `upstream` or `command` | URL of an HTTP-based MCP server |
| `command` | One of `upstream` or `command` | Path to a command that speaks MCP over stdio |
| `args` | No | Arguments passed to the stdio command |
| `auth.type` | No | Authentication type (`bearer`) |
| `auth.token_env` | No | Environment variable containing the auth token |

## Two transport modes

The relay connects to upstreams in two ways.

**HTTP upstream** -- the relay sends MCP requests over HTTP to a remote server:

```yaml
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
    auth:
      type: bearer
      token_env: "LINEAR_API_KEY"
```

Set `auth` when the upstream requires authentication. The relay reads the token from the environment variable specified in `token_env` and sends it as a `Bearer` token.

**Stdio subprocess** -- the relay spawns a local process and communicates over stdin/stdout:

```yaml
routes:
  - scope: demo-sqlite
    command: "uvx"
    args: ["mcp-server-sqlite", "--db-path", "./data.db"]
```

This is useful for local MCP servers or tools distributed as CLI programs. The relay starts the subprocess on launch and manages its lifecycle.

## Write rules

Create a rule file in the `rules_dir` directory. The file's `scope` field must match a route's scope.

Create `./rules/linear.yaml`:

```yaml
scope: linear-tools
mode: enforce

rules:
  # Block deletion of issues
  - name: block-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted. Archive the issue instead."

  # Redact internal account IDs from responses
  - name: redact-account-ids
    match:
      operation: "*"
      when: "context.direction == 'response'"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "ACCT-[0-9]{8}"
          replace: "ACCT-XXXXX"

  # Log all other calls
  - name: audit-all
    match:
      operation: "*"
    action: log
```

Rules are sorted by operation specificity -- exact matches (e.g. `delete_issue`) evaluate before glob patterns (e.g. `*`), which evaluate before catch-all rules. Within the same specificity tier, rules preserve their file order. A **deny** short-circuits evaluation immediately. All matching **redact** and **log** rules are applied:

- **deny** -- the call is blocked and the agent receives a structured error containing the rule name and message
- **redact** -- specified fields are mutated before the call is forwarded (on requests) or before the response is returned to the agent (on responses)
- **log** -- the call is allowed and an audit entry is recorded

Rules with `context.direction == 'response'` in their `when` clause evaluate against the upstream's response rather than the agent's request.

## Start the relay

```bash
$ keep-mcp-relay --config relay.yaml

keep-mcp-relay listening on :8090 (12 tools from 2 upstreams)
```

The relay connects to all configured upstreams, discovers their tools, and starts accepting MCP connections.

To reload rules without restarting (upstream connections stay open):

```bash
$ kill -HUP $(pgrep keep-mcp-relay)
```

The relay logs a confirmation:

```
received SIGHUP, reloading rules (upstream connections unchanged)...
rules reloaded successfully
```

## Connect an agent

Point the agent's MCP client configuration at the relay's listen address. The relay exposes a standard MCP endpoint over HTTP.

For example, in an MCP client config:

```json
{
  "mcpServers": {
    "keep-relay": {
      "url": "http://localhost:8090"
    }
  }
}
```

The agent sees the union of all tools from all configured upstreams. The relay routes each tool call to the correct upstream based on which route originally advertised that tool.

## Verify policy enforcement

Test that deny rules work by invoking a blocked tool call.

If an agent calls `delete_issue` against the config above, the relay evaluates the `block-delete` rule and returns an error:

```
policy denied: Issue deletion is not permitted. Archive the issue instead. (rule: block-delete)
```

The audit log records the decision:

```json
{
  "timestamp": "2026-03-23T14:30:00Z",
  "scope": "linear-tools",
  "operation": "delete_issue",
  "decision": "deny",
  "rule": "block-delete"
}
```

For redact rules, the upstream response passes through normally, but matched patterns are replaced before reaching the agent. A response containing `ACCT-12345678` becomes `ACCT-XXXXX`.

## Troubleshooting

**"relay config: listen is required"** -- the `listen` field is missing from `relay.yaml`. Add a listen address like `:8090`.

**"relay config: routes[0]: either upstream or command is required"** -- each route needs one of `upstream` (HTTP) or `command` (stdio). Both cannot be set on the same route, and one must be present.

**"scope not found"** -- the route's `scope` does not match any rule file's `scope` field. Check that the scope names match exactly between `relay.yaml` and the rule files in `rules_dir`.

**Relay starts but agent gets no tools** -- the upstream MCP server may not be reachable. Check that the `upstream` URL or `command` is correct and that any required credentials are set in the environment.

## Related guides

- [Writing rules](../concepts/02-expressions.md) for CEL expression syntax
- [Starter packs](../reference/) for reusable rule sets
