---
title: "Introduction"
navTitle: "Introduction"
description: "Keep is an API-level policy engine for AI agents — deny, redact, or log structured API calls."
keywords: ["keep", "policy engine", "ai agents", "mcp", "llm gateway"]
---

# Introduction

Keep is an API-level policy engine for AI agents. It evaluates structured API calls against declarative YAML rules and returns allow, deny, or redact decisions. Policy is enforced at the API layer, outside the model's control.

## The core model

Every interaction flows through the same pattern:

1. A structured **call** enters the engine — an operation name, parameters, and context
2. The engine matches the call against **rules** grouped by **scope**
3. Each rule produces a **decision**: allow, deny, or redact

A rule file defines a scope and its rules:

```yaml
scope: linear-tools
mode: enforce
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

When an agent calls `delete_issue`, Keep denies it. When an agent calls `create_issue` with `params.priority == 0`, Keep denies it. Everything else in the `linear-tools` scope is allowed.

Rules use CEL (Common Expression Language) expressions in the `when` field. CEL is non-Turing-complete — expressions evaluate in bounded time with no side effects.

## Deployment modes

Keep runs in three modes. The policy engine is the same in all three — the difference is how calls enter it.

| Mode | What it does | Use when |
|------|-------------|----------|
| `keep` Go library | Embeds the engine in your Go application. You construct calls and evaluate them directly. | You build your own agent infrastructure in Go. |
| `keep-mcp-relay` | Proxies between agents and MCP servers. Each tool call is evaluated as a Keep call before reaching the upstream server. | Agents use MCP (Model Context Protocol) tool servers. |
| `keep-llm-gateway` | Proxies between agents and LLM providers. Decomposes message payloads into per-block calls — filtering both what the model sees and what it tries to do. | You want to filter requests and responses to an LLM provider (Anthropic). |

### Go library

Import the engine and call `Evaluate()` directly:

```go
engine, err := keep.Load("./rules")
if err != nil {
    log.Fatal(err)
}
defer engine.Close()

result, err := engine.Evaluate(call, "linear-tools")
if result.Decision == keep.Deny {
    fmt.Println(result.Message)
}
```

### MCP relay

The relay sits between agents and upstream MCP servers. One listen port, multiple upstreams, each mapped to a scope:

```yaml
# keep-mcp-relay.yaml
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"
  - scope: slack-tools
    upstream: "https://slack-mcp.example.com"
```

```bash
keep-mcp-relay --config keep-mcp-relay.yaml
```

### LLM gateway

The gateway sits between agents and an LLM provider. The agent sets `ANTHROPIC_BASE_URL` to point at Keep:

```yaml
# keep-llm-gateway.yaml
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway
```

```bash
keep-llm-gateway --config keep-llm-gateway.yaml
```

## How modes relate

Rule files are pure policy — no transport details. The same rules work across all three modes. Integration configs (`keep-mcp-relay.yaml`, `keep-llm-gateway.yaml`) handle transport separately.

```
┌─────────┐     ┌──────────────────┐     ┌──────────┐
│  Agent   │────▶│  keep-mcp-relay  │────▶│ MCP      │
│          │     │  (rules + proxy) │     │ Server   │
└─────────┘     └──────────────────┘     └──────────┘

┌─────────┐     ┌──────────────────┐     ┌──────────┐
│  Agent   │────▶│ keep-llm-gateway │────▶│ LLM      │
│          │     │  (rules + proxy) │     │ Provider │
└─────────┘     └──────────────────┘     └──────────┘

┌─────────────────────────────────┐
│  Your Go app                    │
│  engine.Evaluate(call, scope)   │
└─────────────────────────────────┘
```

## Next steps

- [Installation](./02-installation.md) — install Keep and its binaries
- [Quick start](./03-quick-start.md) — write your first rules and run the relay
