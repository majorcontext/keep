---
title: "Running the LLM gateway"
navTitle: "LLM gateway"
description: "Run Keep as an LLM proxy between agents and the Anthropic API with bidirectional policy filtering."
keywords: ["keep", "llm", "gateway", "anthropic", "proxy", "streaming"]
---

This guide walks through setting up `keep-llm-gateway` as a policy-enforcing proxy between an AI agent and the Anthropic API. By the end you will have a running gateway that blocks personally identifiable information (PII), redacts secrets, and filters dangerous tool use.

## Prerequisites

- Keep installed (`keep-llm-gateway` binary on your `PATH`)
- An Anthropic API key exported as `ANTHROPIC_API_KEY`

## 1. Write the gateway config

Create a file called `gateway.yaml`:

```yaml
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: my-gateway
```

| Field | Purpose |
|-------|---------|
| `listen` | Address and port the gateway binds to |
| `rules_dir` | Directory containing rule files |
| `provider` | LLM provider protocol (`anthropic`) |
| `upstream` | URL of the upstream API |
| `scope` | Scope name that rules are evaluated against |

The gateway also accepts optional fields:

- `profiles_dir` -- directory containing profile definitions
- `packs_dir` -- directory containing starter packs
- `log.format` -- log format, defaults to `json`
- `log.output` -- log destination, defaults to `stdout`; set to a file path to write audit logs to disk

## 2. Write rules

Create `./rules/gateway.yaml`. Rules target four LLM operations:

| Operation | Direction | Fires on |
|-----------|-----------|----------|
| `llm.request` | request | The full API request before it is sent upstream |
| `llm.text` | both | Each text content block (request and response) |
| `llm.tool_use` | response | Each tool-use block the model emits |
| `llm.tool_result` | request | Each tool-result block the agent sends back |

> **Tip:** Use `context.direction` (values: `request` or `response`) to restrict a rule to one direction when the operation fires on both.

Here is a rule file with practical examples:

```yaml
scope: my-gateway
mode: enforce

defs:
  email_pattern: "'[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}'"

rules:
  # Redact secrets (API keys, tokens) from tool results
  # before they reach the model
  - name: redact-secrets-in-tool-results
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      secrets: true

  # Block prompts that contain email addresses
  - name: block-pii-in-prompts
    match:
      operation: "llm.text"
      when: >
        context.direction == 'request'
        && matches(params.text, email_pattern)
    action: deny
    message: "PII detected. Use opaque identifiers instead of email addresses."

  # Block destructive bash commands from the model
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: >
        lower(params.name) == 'bash'
        && containsAny(lower(params.input.command),
           ['rm -rf', 'drop table', 'truncate', 'mkfs'])
    action: deny
    message: "Destructive command blocked by policy."

  # Block outbound network commands
  - name: block-networking
    match:
      operation: "llm.tool_use"
      when: >
        lower(params.name) == 'bash'
        && containsAny(lower(params.input.command),
           ['curl ', 'wget ', 'nc ', 'ssh '])
    action: deny
    message: "Network access blocked. Use approved integrations."

  # Audit-log every LLM operation
  - name: audit-all
    match:
      operation: "llm.*"
    action: log
```

Validate the rules before starting the gateway:

```bash
$ keep validate ./rules
```

## 3. Start the gateway

```bash
$ keep-llm-gateway --config gateway.yaml
```

The gateway logs its listen address, provider, and scope on startup:

```
keep-llm-gateway listening on :8080 (provider: anthropic, scope: my-gateway)
```

### Hot-reload rules

Send `SIGHUP` to reload rule files without restarting:

```bash
$ kill -HUP $(pgrep keep-llm-gateway)
```

If the reload fails (syntax error, missing file), the gateway keeps the previous rules and logs the error.

### Debug and verbose modes

- `KEEP_VERBOSE=1` prints each evaluated call and decision to stderr.
- `KEEP_DEBUG=/tmp/gateway-debug.log` writes detailed debug logs to the given file.

## 4. Point your agent at the gateway

Set the base URL so your agent's HTTP client sends requests through the gateway instead of directly to Anthropic:

```bash
$ export ANTHROPIC_BASE_URL=http://localhost:8080
```

The gateway forwards the `x-api-key` and `anthropic-version` headers to the upstream API. No changes to your agent code are required.

## 5. Streaming

The gateway supports streaming responses (SSE). When a streaming request arrives, the gateway buffers the complete response, evaluates rules against the assembled content blocks, and then streams the result to the client. If a rule denies or redacts content, the modification applies before any bytes reach the agent.

From the agent's perspective, streaming works identically to a direct Anthropic connection.

## 6. Verify

Send a request that triggers a rule. For example, include an email address in the prompt to trigger the PII rule:

```bash
$ curl -s http://localhost:8080/v1/messages \
    -H "x-api-key: $ANTHROPIC_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -H "content-type: application/json" \
    -d '{
      "model": "claude-sonnet-4-20250514",
      "max_tokens": 100,
      "messages": [{"role": "user", "content": "Email jane@example.com about the project."}]
    }' | jq .
```

The gateway returns an error response and does not forward the request upstream:

```json
{
  "type": "error",
  "error": {
    "type": "request_denied",
    "message": "PII detected. Use opaque identifiers instead of email addresses."
  }
}
```

The audit log records the denial with the rule name, scope, operation, and timestamp.

## Troubleshooting

**Gateway fails to start with "scope not found"**
The `scope` in `gateway.yaml` must match the `scope` declared in at least one rule file in `rules_dir`. Check for typos and run `keep validate ./rules`.

**Agent receives 502 errors**
The gateway cannot reach the upstream URL. Verify `upstream` in your config and check network connectivity.

**Rules not taking effect after editing**
The gateway loads rules at startup. Send `SIGHUP` to reload, or restart the process.

## Related guides

- [Expressions](../concepts/02-expressions.md) -- CEL expression syntax for `when` conditions
- [Scopes](../concepts/03-scopes.md) -- how scopes bind rules to traffic
