---
title: "Environment variables"
navTitle: "Environment"
description: "Environment variables that configure Keep runtime behavior."
keywords: ["keep", "environment", "variables", "configuration"]
---

# Environment variables

Environment variables control runtime behavior for Keep binaries. They are read at startup and cannot be changed while a process is running.

## keep-llm-gateway

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KEEP_VERBOSE` | Enable verbose packet logging to stderr. Prints human-readable request and response dumps with ANSI color. Set to any non-empty value to enable. Set to `"full"` to disable string truncation (default truncation: 120 characters). | Unset (disabled) | `KEEP_VERBOSE=1` or `KEEP_VERBOSE=full` |
| `KEEP_DEBUG` | Path to a debug log file. Enables structured debug logging via `slog` with `TextHandler` at debug level. When both `KEEP_VERBOSE` and `KEEP_DEBUG` are set, Go's default logger output redirects to the debug file so stderr stays clean for verbose packet output. | Unset (disabled) | `KEEP_DEBUG=./debug.log` |

## Route authentication

Relay routes that use `auth.token_env` read the token value from the named environment variable at startup.

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| *(user-defined)* | Token value for upstream authentication. The variable name is set in the relay config under `routes[].auth.token_env`. | -- | `LINEAR_API_KEY=lin_api_...` |

## Usage examples

Start the gateway with verbose output:

```bash
$ KEEP_VERBOSE=1 keep-llm-gateway --config keep-llm-gateway.yaml
```

Start the gateway with full verbose output and debug logging:

```bash
$ KEEP_VERBOSE=full KEEP_DEBUG=./debug.log keep-llm-gateway --config keep-llm-gateway.yaml
```

## Related pages

- [CLI reference](01-cli.md) -- flags and signals for all Keep binaries
- [Gateway configuration](05-gateway-config.md) -- `keep-llm-gateway.yaml` schema
- [Relay configuration](04-relay-config.md) -- `keep-mcp-relay.yaml` schema
