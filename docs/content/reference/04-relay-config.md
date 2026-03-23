---
title: "Relay configuration reference"
navTitle: "Relay config"
description: "Complete schema reference for keep-mcp-relay.yaml configuration files."
keywords: ["keep", "relay", "configuration", "mcp", "yaml"]
---

# Relay configuration reference

The relay configuration file controls how `keep-mcp-relay` listens for MCP connections, routes tool calls to upstream servers, and logs audit events. Pass the file path with `--config`.

## Top-level fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `listen` | `string` | Yes | -- | Address and port to listen on (e.g., `":8090"`, `"127.0.0.1:8090"`). |
| `rules_dir` | `string` | Yes | -- | Path to the directory containing Keep rule files. |
| `profiles_dir` | `string` | No | `""` | Path to the directory containing profile YAML files. |
| `packs_dir` | `string` | No | `""` | Path to the directory containing starter pack files. |
| `routes` | `list` | Yes | -- | One or more route definitions. Must not be empty. |
| `log` | `object` | No | See below | Log format and output configuration. |

## routes

Each route maps a scope to an upstream MCP server. The relay connects to each upstream at startup and discovers available tools.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `scope` | `string` | Yes | -- | Scope name matching a scope declared in your rule files. |
| `upstream` | `string` | Conditional | -- | URL of a remote MCP server (SSE transport). Mutually exclusive with `command`. |
| `command` | `string` | Conditional | -- | Command to launch a local MCP server (stdio transport). Mutually exclusive with `upstream`. |
| `args` | `list` | No | `[]` | Arguments passed to `command`. Ignored when `upstream` is set. |
| `auth` | `object` | No | `null` | Authentication for the upstream connection. |

Exactly one of `upstream` or `command` must be set per route.

### auth

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | `string` | Yes | -- | Authentication type (e.g., `"bearer"`). |
| `token_env` | `string` | No | `""` | Name of the environment variable that holds the token value. |
| `header` | `string` | No | `""` | Custom header name for the token. Defaults to `Authorization` for bearer auth. |

## log

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `format` | `string` | No | `"json"` | Log format. |
| `output` | `string` | No | `"stdout"` | Output destination. A file path writes audit logs to that file. |

## Complete example

```yaml
listen: ":8090"
rules_dir: "./rules"
profiles_dir: "./profiles"
packs_dir: "./packs"

routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
    auth:
      type: bearer
      token_env: "LINEAR_API_KEY"

  - scope: slack-tools
    upstream: "https://slack-mcp.example.com"

  - scope: sqlite-tools
    command: "uvx"
    args: ["mcp-server-sqlite", "--db-path", "./data.db"]

log:
  format: json
  output: stdout
```

This configuration starts the relay on port 8090 with three upstream routes. The `linear-tools` route authenticates with a bearer token read from the `LINEAR_API_KEY` environment variable. The `sqlite-tools` route launches a local MCP server via `uvx` using stdio transport.

## Related pages

- [CLI reference](01-cli.md) -- `keep-mcp-relay` flags and signals
- [MCP relay guide](../guides/01-mcp-relay.md) -- step-by-step setup
- [Scopes](../concepts/03-scopes.md) -- how scopes bind rules to routes
