---
title: "Quick start"
navTitle: "Quick start"
description: "Write, validate, and test your first Keep policy rules in under five minutes."
keywords: ["keep", "quick start", "tutorial", "rules", "validate", "test"]
---

# Quick start

This walkthrough covers writing a rule file, validating it, and testing it against fixture data. By the end you will have a working policy that denies two dangerous Linear operations.

## Prerequisites

- `keep` CLI [installed](./02-installation.md)

## 1. Write a rule file

Create a `rules/` directory and add a rule file:

```bash
$ mkdir -p rules
```

Save the following as `rules/linear.yaml`:

```yaml
scope: linear-tools
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

The first rule denies any `delete_issue` call outright. The second uses a CEL expression to deny `create_issue` only when the priority is 0. All other operations are allowed by default.

## 2. Validate your rules

```bash
$ keep validate ./rules

OK (1 scopes, linear-tools: 0 errors)
```

`keep validate` loads every rule file in the directory, compiles CEL expressions, and reports errors. If a file has a syntax error or an invalid expression, the command exits with a non-zero status and prints the problem.

## 3. Write a fixture file

Fixtures let you test rules against sample calls without running a live service. Create a `fixtures/` directory:

```bash
$ mkdir -p fixtures
```

Save the following as `fixtures/linear.yaml`:

```yaml
scope: linear-tools
tests:
  - name: block delete_issue
    call:
      operation: delete_issue
    expect:
      decision: deny
      rule: no-delete
      message: "Issue deletion is not permitted."

  - name: block P0 creation
    call:
      operation: create_issue
      params:
        priority: 0
        title: "Server on fire"
    expect:
      decision: deny
      rule: no-auto-p0

  - name: allow P1 creation
    call:
      operation: create_issue
      params:
        priority: 1
        title: "Fix login timeout"
    expect:
      decision: allow
```

Each test specifies a call (operation and params) and the expected decision. You can also assert the rule name and message.

## 4. Run the tests

```bash
$ keep test ./rules --fixtures ./fixtures

linear.yaml:
  PASS  block delete_issue
  PASS  block P0 creation
  PASS  allow P1 creation

3 tests, 3 passed, 0 failed
```

All three calls are evaluated against the rules in `./rules`. The two deny cases match, and the P1 creation falls through to the default allow.

> **Tip:** `keep test` forces `enforce` mode regardless of what the rule file declares. This means rules with `mode: audit_only` still fire during testing so you can verify behavior before enabling enforcement.

## Next steps

You have rules that validate and pass tests. To enforce them at runtime:

- [Run the MCP relay](../guides/01-mcp-relay.md) -- proxy MCP tool calls through Keep so agents hit your policy before reaching upstream servers
- [Run the LLM gateway](../guides/02-llm-gateway.md) -- sit between your agent and the LLM provider to filter both requests and responses
