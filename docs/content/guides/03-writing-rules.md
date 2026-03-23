---
title: "Writing rules"
navTitle: "Writing rules"
description: "How to write Keep policy rules — match conditions, actions, expressions, and common patterns."
keywords: ["keep", "rules", "policy", "writing", "patterns", "deny", "redact"]
---

# Writing rules

This guide walks through rule file structure, match conditions, actions, and common patterns. By the end you will have working rules and know how to test them.

## Rule file structure

A rule file is a YAML document with a scope, a mode, and a list of rules.

```yaml
scope: linear-tools
mode: enforce
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 issues must be created by a human."

  - name: audit-reads
    match:
      operation: "search_*"
    action: log
```

### Top-level fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `scope` | yes | -- | Unique name for this rule set |
| `mode` | no | `audit_only` | `enforce` applies rules; `audit_only` logs without enforcing |
| `on_error` | no | `closed` | `closed` denies on CEL eval errors; `open` skips the rule |
| `defs` | no | -- | Named constants substituted into CEL expressions |
| `rules` | yes | -- | Ordered list of rules |

New scopes default to `audit_only`. Deploy with observation first, review logs, then switch to `enforce`.

## Match conditions

The `match` block has two optional fields. If both are present, both must match. If neither is present, the rule matches every call in the scope.

### Operation patterns

`operation` is a glob pattern matched against the call's operation name.

```yaml
match:
  operation: "delete_issue"       # exact match
```

```yaml
match:
  operation: "create_*"           # any operation starting with create_
```

```yaml
match:
  operation: "llm.tool_*"         # matches llm.tool_result, llm.tool_use
```

```yaml
match:
  operation: "*"                  # matches everything (same as omitting)
```

Glob syntax supports `*` (any sequence of characters) and `?` (any single character). For more complex matching, use a `when` expression instead.

### When expressions

`when` is a CEL (Common Expression Language) expression that must evaluate to `true` for the rule to fire. Expressions have access to `params`, `context`, and `now`.

```yaml
match:
  operation: "create_issue"
  when: "params.priority == 0"
```

```yaml
match:
  operation: "llm.tool_use"
  when: "params.name == 'bash' && params.input.command.contains('rm -rf')"
```

See [Expressions](../concepts/02-expressions.md) for the full CEL reference, including custom functions like `containsAny()`, `rateCount()`, and `inTimeWindow()`.

## Actions

Every rule has one action: deny, redact, or log.

| Action | Stops call | Mutates params | Audit logged |
|--------|-----------|----------------|--------------|
| `deny` | Yes | No | Yes |
| `redact` | No | Yes | Yes |
| `log` | No | No | Yes |

Rules are sorted by operation specificity -- exact matches evaluate before glob patterns, which evaluate before catch-all rules. Within the same specificity tier, rules preserve their file order. The first deny short-circuits evaluation immediately. All matching redact and log rules are applied.

### Deny

Block the call and return a structured error to the agent.

```yaml
- name: block-writes
  match:
    operation: "write_query"
  action: deny
  message: "Database is read-only. Write operations are not permitted."
```

Always include a `message` on deny rules. The message is returned to the agent and appears in the audit log.

### Redact

Allow the call but scrub sensitive content from a field before forwarding.

```yaml
- name: redact-secrets
  match:
    operation: "llm.tool_result"
  action: redact
  redact:
    target: "params.content"
    patterns:
      - match: "AKIA[0-9A-Z]{16}"
        replace: "[REDACTED:AWS_KEY]"
```

The `redact` block requires:

- `target` -- dot-path to the string field to scan (e.g., `params.content`, `params.text`)
- `patterns` -- list of RE2 regex patterns with replacement strings

For automatic secret detection, use `secrets: true` instead of (or alongside) manual patterns. This scans the target field using ~160 built-in patterns covering AWS keys, private keys, API tokens, and more.

```yaml
- name: strip-secrets
  match:
    operation: "llm.tool_result"
  action: redact
  redact:
    target: "params.content"
    secrets: true
```

Multiple redact rules can match the same call. Mutations are applied in rule order.

### Log

Allow the call and record it in the audit log.

```yaml
- name: audit-all
  match:
    operation: "*"
  action: log
```

Log rules are useful as a catch-all at the end of a rule file to capture all traffic for observability.

## Using defs

`defs` defines named constants that are substituted into CEL expressions. Extract repeated values into defs to keep rules readable.

```yaml
scope: demo-gateway
mode: enforce

defs:
  destructive_patterns: "['rm -rf', 'DROP TABLE', 'TRUNCATE', 'mkfs']"
  network_commands: "['curl ', 'wget ', 'nc ', 'ssh ', 'ncat ']"

rules:
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: >
        lower(params.name) == 'bash'
        && containsAny(lower(params.input.command), destructive_patterns)
    action: deny
    message: "Destructive command blocked by policy."

  - name: block-networking
    match:
      operation: "llm.tool_use"
      when: >
        lower(params.name) == 'bash'
        && containsAny(lower(params.input.command), network_commands)
    action: deny
    message: "Network access is blocked by policy."
```

Def values are raw strings substituted before compilation. Each value must be a valid CEL sub-expression -- a list literal, string literal, or integer.

## Common patterns

### Block destructive operations

```yaml
- name: no-delete
  match:
    operation: "delete_issue"
  action: deny
  message: "Issue deletion is not permitted."
```

### Redact passwords from responses

```yaml
- name: redact-passwords
  match:
    operation: "read_query"
    when: "context.direction == 'response'"
  action: redact
  redact:
    target: "params.content"
    patterns:
      - match: "hunter2|p@ssw0rd!|letmein123"
        replace: "********"
```

### Rate limit an operation

```yaml
- name: issue-creation-rate
  match:
    operation: "create_issue"
    when: "rateCount('linear:create:' + context.agent_id, '1h') > 20"
  action: deny
  message: "Rate limit exceeded. Maximum 20 issues per hour."
```

> **Note:** `rateCount()` uses a local counter store. Counters are not shared across relay or gateway instances.

### Time-based restrictions

```yaml
- name: off-hours-deny
  match:
    operation: "create_*"
    when: "!inTimeWindow(now, '09:00', '18:00', 'America/Los_Angeles')"
  action: deny
  message: "Issue creation is restricted to business hours (9am-6pm PT)."
```

### Block PII in prompts

```yaml
defs:
  email_pattern: "'[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}'"

rules:
  - name: block-pii-in-prompts
    match:
      operation: "llm.text"
      when: >
        context.direction == 'request'
        && params.text.matches(email_pattern)
    action: deny
    message: "PII detected in prompt. Use opaque customer IDs instead."
```

### Content filtering with containsAny

```yaml
- name: no-sensitive-content
  match:
    operation: "create_issue"
    when: >
      containsAny(params.title, ['acquisition', 'merger', 'RIF', 'layoff'])
  action: deny
  message: "Issue contains sensitive business terms. Create manually."
```

## Testing your rules

Keep validates rule files and runs them against test fixtures.

### Validate rule syntax

```bash
$ keep validate ./rules
```

This checks YAML structure, CEL expression compilation, and scope uniqueness. Fix any errors before deploying.

### Write test fixtures

Create fixture files alongside your rules. Each fixture specifies a call and the expected decision.

```yaml
# fixtures/linear-test.yaml
scope: linear-tools
tests:
  - name: "blocks issue deletion"
    call:
      operation: "delete_issue"
      params: {}
    expect:
      decision: "deny"
      rule: "no-delete"

  - name: "allows normal issue creation"
    call:
      operation: "create_issue"
      params:
        priority: 2
        title: "Fix login bug"
    expect:
      decision: "allow"

  - name: "blocks P0 creation"
    call:
      operation: "create_issue"
      params:
        priority: 0
        title: "Outage"
    expect:
      decision: "deny"
      rule: "no-auto-p0"
```

### Run tests

```bash
$ keep test ./rules --fixtures ./fixtures
```

This evaluates each fixture call against the rules and reports pass/fail for every test case. Use `keep test` in CI to catch policy regressions before deployment.

### Iterate with audit mode

Start with `mode: audit_only` in production. Review audit logs to see which rules would fire. Once you are confident the rules behave correctly, switch to `mode: enforce`.
