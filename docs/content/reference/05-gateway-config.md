---
title: "Gateway configuration reference"
navTitle: "Gateway config"
description: "Complete schema reference for keep-llm-gateway.yaml configuration files."
keywords: ["keep", "gateway", "configuration", "llm", "anthropic", "yaml"]
---

# Gateway configuration reference

The gateway configuration file controls how `keep-llm-gateway` proxies LLM API traffic, evaluates policy on decomposed message blocks, and logs audit events. Pass the file path with `--config`.

## Top-level fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `listen` | `string` | Yes | -- | Address and port to listen on (e.g., `":8080"`, `"127.0.0.1:8080"`). |
| `rules_dir` | `string` | Yes | -- | Path to the directory containing Keep rule files. |
| `profiles_dir` | `string` | No | `""` | Path to the directory containing profile YAML files. |
| `packs_dir` | `string` | No | `""` | Path to the directory containing starter pack files. |
| `provider` | `string` | Yes | -- | LLM provider name. Currently accepted: `"anthropic"`. |
| `upstream` | `string` | Yes | -- | Base URL of the LLM provider API (e.g., `"https://api.anthropic.com"`). |
| `scope` | `string` | Yes | -- | Scope name matching a scope declared in your rule files. |
| `decompose` | `object` | No | See below | Controls which message block types the gateway decomposes for evaluation. |
| `log` | `object` | No | See below | Log format and output configuration. |

## decompose

The gateway decomposes LLM requests and responses into individual blocks (tool use, tool result, text, summaries) and evaluates each against Keep rules. The `decompose` section controls which block types are evaluated.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `tool_use` | `bool` | No | `true` | Evaluate tool-use blocks in LLM responses. |
| `tool_result` | `bool` | No | `true` | Evaluate tool-result blocks in requests. |
| `text` | `bool` | No | `false` | Evaluate text blocks in messages. |
| `request_summary` | `bool` | No | `true` | Evaluate a summary call for each inbound request. |
| `response_summary` | `bool` | No | `true` | Evaluate a summary call for each outbound response. |

See [LLM decomposition](../concepts/05-llm-decomposition.md) for how the gateway maps message blocks to Keep calls.

## log

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `format` | `string` | No | `"json"` | Log format. |
| `output` | `string` | No | `"stdout"` | Output destination. A file path writes audit logs to that file. |

## Complete example

```yaml
listen: ":8080"
rules_dir: "./rules"
profiles_dir: "./profiles"
packs_dir: "./packs"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway

decompose:
  tool_use: true
  tool_result: true
  text: false
  request_summary: true
  response_summary: true

log:
  format: json
  output: stdout
```

This configuration proxies Anthropic API traffic on port 8080. The gateway decomposes tool-use and tool-result blocks for policy evaluation but skips plain text blocks. Audit events are written to stdout in JSON format.

## Related pages

- [CLI reference](01-cli.md) -- `keep-llm-gateway` flags, signals, and environment variables
- [LLM gateway guide](../guides/02-llm-gateway.md) -- step-by-step setup
- [LLM decomposition](../concepts/05-llm-decomposition.md) -- how messages are decomposed into calls
- [Environment variables](06-environment.md) -- `KEEP_VERBOSE` and `KEEP_DEBUG`
