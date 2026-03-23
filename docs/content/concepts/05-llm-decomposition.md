---
title: "LLM decomposition"
navTitle: "LLM decomposition"
description: "How Keep decomposes LLM messages into per-block policy calls for fine-grained evaluation."
keywords: ["keep", "llm", "gateway", "anthropic", "decomposition", "messages api"]
---

# LLM decomposition

An Anthropic Messages API request is a nested structure: a list of messages, each containing a list of content blocks. A single request might carry user text, tool results from a previous turn, images, and system instructions all at once. Policy needs to evaluate these pieces individually -- is this tool result safe? Does this text contain PII? Is this tool use allowed?

The LLM gateway solves this by decomposing each API request and response into multiple flat Keep calls, evaluating each one independently, and then reassembling the results.

## Why decomposition matters

Without decomposition, a policy rule sees the entire Messages API payload as a single blob. That forces rules into awkward patterns: parsing nested JSON, iterating over arrays, and handling multiple content types in one expression.

Decomposition flattens the structure. Each content block becomes its own call with a typed operation and relevant parameters. Rules stay simple and focused -- one rule per concern, one content block per evaluation.

## The decomposition model

One API request produces multiple Keep calls. One API response produces another set. Each call has an `operation` that identifies the block type and `params` that carry the block's content.

```
Anthropic Messages API request
│
├── llm.request          (summary: model, token estimate, message count)
├── llm.text             (user message, block 0)
├── llm.tool_result      (tool result, block 1)
└── llm.text             (user message, block 2)

Anthropic Messages API response
│
├── llm.response         (summary: stop reason, tool use count)
├── llm.text             (assistant text, block 0)
└── llm.tool_use         (tool call, block 1)
```

### Call types

| Operation | Direction | Params | When emitted |
|-----------|-----------|--------|--------------|
| `llm.request` | request | `model`, `system`, `token_estimate`, `tool_result_count`, `message_count` | Once per request (summary) |
| `llm.text` | request or response | `text`, `role` | Once per text content block |
| `llm.tool_result` | request | `tool_name`, `tool_use_id`, `content` | Once per tool result block |
| `llm.tool_use` | response | `name`, `input` | Once per tool use block |
| `llm.response` | response | `stop_reason`, `tool_use_count` | Once per response (summary) |

Every call carries a `context.direction` field set to `"request"` or `"response"`, identifying which side of the LLM interaction it belongs to.

### Concrete example

An agent sends a two-message conversation to Claude. The first message is user text; the second contains a tool result from a previous turn. Claude responds with text and a new tool call.

The gateway decomposes this into seven calls:

```
Request decomposition (4 calls):

  [0] llm.request     { model: "claude-sonnet-4-20250514", token_estimate: 312, message_count: 2 }
  [1] llm.text        { text: "Summarize the open issues", role: "user" }
  [2] llm.tool_result { tool_name: "list_issues", content: "[{id: 1, ...}]" }
  [3] llm.text        { text: "Here are the results", role: "user" }

Response decomposition (3 calls):

  [0] llm.response    { stop_reason: "tool_use", tool_use_count: 1 }
  [1] llm.text        { text: "I found 3 open issues. Let me get more details.", role: "assistant" }
  [2] llm.tool_use    { name: "get_issue", input: { id: 42 } }
```

Each of these calls is evaluated against the rules in the gateway's configured scope. A rule matching `llm.tool_use` with `when: 'params.name == "delete_issue"'` fires only on tool use blocks, leaving text and tool results untouched.

## Bidirectional filtering

The gateway filters both directions of the LLM interaction:

- **Request filtering** controls what the model sees. Rules evaluate text blocks and tool results before they reach the LLM provider. A rule could redact PII from user messages or deny requests that carry sensitive tool output.
- **Response filtering** controls what the model tries to do. Rules evaluate the model's text output and tool calls before they reach the agent. A rule could deny specific tool invocations or redact content from the model's response.

Request calls carry `context.direction: "request"`. Response calls carry `context.direction: "response"`. Rules can match on direction to apply different policies to each side:

```yaml
# Redact SSNs from user messages sent to the model
- name: redact-ssn-in-context
  match:
    operation: "llm.text"
    when: 'context.direction == "request" && params.text.matches("\\d{3}-\\d{2}-\\d{4}")'
  action: redact
  redact:
    fields: ["params.text"]

# Block the model from calling dangerous tools
- name: no-delete-tools
  match:
    operation: "llm.tool_use"
    when: 'params.name.startsWith("delete_")'
  action: deny
  message: "Destructive tool calls are not permitted."
```

## Reassembly

After evaluation, the gateway patches results back into the original message structure. The behavior depends on the decision:

- **Allow** -- the block passes through unchanged.
- **Redact** -- the redacted content replaces the original block content in the message. The rest of the request or response is unchanged. For example, if a text block's `params.text` is redacted, the modified text is written back into the corresponding content block at its original position.
- **Deny** -- any single deny decision blocks the entire request or response. The gateway returns a structured error to the caller instead of forwarding the payload.

The gateway tracks each decomposed call's position in the original message structure (message index and block index) so that redacted values are written back to the correct location. This position tracking is maintained through the entire evaluate-and-patch cycle -- even when some block types are disabled in the decompose config, the remaining blocks retain their correct positions.

Reassembly preserves the original payload structure. Fields not covered by decomposition (model, max_tokens, tools, metadata, system prompt) pass through untouched. The gateway only modifies content blocks that a rule acted on.

## Configuration

The `decompose` section of the gateway config controls which block types are decomposed into separate calls. Each option can be set to `true` or `false`.

```yaml
# keep-llm-gateway.yaml
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
```

| Option | Default | What it controls |
|--------|---------|-----------------|
| `tool_result` | `true` | Emit `llm.tool_result` calls for tool result blocks in requests |
| `tool_use` | `true` | Emit `llm.tool_use` calls for tool use blocks in responses |
| `text` | `false` | Emit `llm.text` calls for text content blocks |
| `request_summary` | `true` | Emit the `llm.request` summary call |
| `response_summary` | `true` | Emit the `llm.response` summary call |

Text decomposition is off by default. Most policies focus on tool interactions, and text blocks are the most numerous content type. Enable it when rules need to inspect message text -- PII detection, content filtering, or prompt injection checks.

Disabling a block type means no calls are emitted for that type and no rules can match against it. The blocks pass through unmodified.

> **Note:** Summary calls (`llm.request` and `llm.response`) are useful for coarse-grained policies like token budget enforcement or blocking specific models. They evaluate before the per-block calls for their direction.

## Related concepts

- [Introduction](../getting-started/01-introduction.md) -- overview of Keep's core model and deployment modes
- [Rules](./01-rules.md) -- rule structure, match conditions, and actions
- [Expressions](./02-expressions.md) -- CEL expression syntax for `when` conditions
