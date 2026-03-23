---
title: "CLI reference"
navTitle: "CLI"
description: "Complete reference for all Keep CLI commands and flags."
keywords: ["keep", "cli", "commands", "reference", "validate", "test"]
---

# CLI reference

Keep ships three binaries: `keep` (rule authoring), `keep-mcp-relay` (MCP proxy), and `keep-llm-gateway` (LLM proxy).

## keep

```
keep <command> [flags]
```

### keep validate

Validate rule files, profiles, and starter packs. Loads and compiles all rules, reporting any errors.

```
keep validate <rules-dir> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--profiles` | `string` | `""` | Path to profiles directory |
| `--packs` | `string` | `""` | Path to starter packs directory |

**Output:** `OK (<N> scopes, <scope-a>, <scope-b>: 0 errors)`

**Exit codes:** `0` on success, `1` on validation error.

```bash
$ keep validate ./rules --profiles ./profiles
OK (2 scopes, linear-tools, anthropic-gateway: 0 errors)
```

### keep test

Test rules against fixture files. All scopes are evaluated in enforce mode regardless of their configured mode, so `audit_only` rules fire as if enforced.

```
keep test <rules-dir> --fixtures <path> [flags]
```

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--fixtures` | `string` | `""` | Yes | Path to fixtures file or directory |
| `--profiles` | `string` | `""` | No | Path to profiles directory |
| `--packs` | `string` | `""` | No | Path to starter packs directory |

Each test case compares the engine's decision, rule name, message, and mutations against expected values in the fixture.

**Output format:**

```
<fixture-file>:
  PASS  <test-name>
  FAIL  <test-name>
        <reason>

<N> tests, <N> passed, <N> failed
```

**Exit codes:** `0` when all tests pass, `1` when any test fails.

```bash
$ keep test ./rules --fixtures ./fixtures
linear.yaml:
  PASS  allow-read-issue
  PASS  deny-delete-issue
  FAIL  redact-email
        expected rule: redact-pii
        got rule:      (none)

3 tests, 2 passed, 1 failed
```

### keep version

Print build information.

```
keep version
```

**Output format:**

```
version: <version>
commit:  <commit>
date:    <date>
```

## keep-mcp-relay

MCP (Model Context Protocol) relay proxy. Sits between an MCP client and one or more upstream MCP servers, evaluating every tool call against Keep rules.

```
keep-mcp-relay --config <path>
```

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--config` | `string` | `""` | Yes | Path to relay config file |

The config file specifies `rules_dir`, `listen` address, upstream `routes`, and logging options. See [MCP relay guide](../guides/01-mcp-relay.md) for config file format.

**Signals:**

| Signal | Behavior |
|--------|----------|
| `SIGHUP` | Reload rules from disk (upstream connections unchanged) |
| `SIGINT` / `SIGTERM` | Graceful shutdown (30s timeout) |

**Exit codes:** `0` clean shutdown, `1` runtime error, `2` missing `--config`.

## keep-llm-gateway

HTTP reverse proxy for LLM provider APIs. Intercepts tool-use requests in the LLM response stream and evaluates them against Keep rules before the agent acts.

```
keep-llm-gateway --config <path>
```

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--config` | `string` | `""` | Yes | Path to gateway config file |

The config file specifies `rules_dir`, `listen` address, LLM `provider`, `scope`, and logging options. See [MCP relay guide](../guides/01-mcp-relay.md) for a similar config structure.

**Environment variables:**

| Variable | Description |
|----------|-------------|
| `KEEP_VERBOSE` | Enable verbose packet logging to stderr. Set to `full` to disable string truncation. |
| `KEEP_DEBUG` | Path to a debug log file. Enables structured debug logging via `slog`. |

**Signals:**

| Signal | Behavior |
|--------|----------|
| `SIGHUP` | Reload rules from disk |
| `SIGINT` / `SIGTERM` | Graceful shutdown (30s timeout) |

**Exit codes:** `0` clean shutdown, `1` runtime error, `2` missing `--config`.
