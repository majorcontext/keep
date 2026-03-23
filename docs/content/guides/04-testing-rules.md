---
title: "Testing rules"
navTitle: "Testing rules"
description: "Test Keep policy rules against fixture files to verify expected behavior before deployment."
keywords: ["keep", "testing", "fixtures", "validation", "ci"]
---

# Testing rules

Rule files define what your policy should do. Fixture files verify that it actually does it. Running `keep test` evaluates calls from fixture files against your rules and reports whether the outcomes match expectations.

## Prerequisites

- `keep` CLI [installed](../getting-started/02-installation.md)
- A rules directory with at least one rule file

## Fixture file format

A fixture file is a YAML file with a `scope` and a list of `tests`. Each test describes a call and the expected result.

```yaml
scope: linear-tools
tests:
  - name: "allow normal issue creation"
    call:
      operation: "create_issue"
      params:
        title: "Fix auth bug"
        teamId: "TEAM-ENG"
        priority: 1
    expect:
      decision: allow

  - name: "deny P0 creation"
    call:
      operation: "create_issue"
      params:
        title: "Outage"
        teamId: "TEAM-ENG"
        priority: 0
    expect:
      decision: deny
      rule: no-auto-p0

  - name: "deny deletion"
    call:
      operation: "delete_issue"
      params:
        issueId: "ISSUE-123"
    expect:
      decision: deny
      rule: no-delete
```

### Fields

**Top-level:**

| Field | Required | Description |
|-------|----------|-------------|
| `scope` | Yes | Default scope for all tests in the file. Individual tests can override it. |
| `tests` | Yes | List of test cases. |

**Each test case:**

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Descriptive name shown in test output. |
| `call.operation` | Yes | The operation string to evaluate. |
| `call.params` | Yes | Parameters passed to the rule engine. Use `{}` for empty params. |
| `call.context` | No | Override context fields like `agent_id`, `user_id`, `scope`, `direction`, `labels`, or `timestamp`. |
| `expect.decision` | Yes | Expected decision: `allow`, `deny`, or `redact`. |
| `expect.rule` | No | Expected rule name that produced the decision. |
| `expect.message` | No | Substring expected in the result message. |
| `expect.mutations` | No | Expected mutations from a redact action (list of `path` and `replaced` pairs). |

The file-level `scope` applies to every test that does not set `call.context.scope`. Each test gets a default `agent_id` of `"test"` unless overridden.

## Running tests

Point `keep test` at your rules directory and fixture files:

```bash
$ keep test ./rules --fixtures ./fixtures
```

The `--fixtures` flag accepts a single `.yaml` file or a directory. When given a directory, Keep loads all `.yaml` and `.yml` files in it.

```bash
# Test against a single fixture file
$ keep test ./rules --fixtures ./fixtures/linear-tests.yaml

# Test against all fixture files in a directory
$ keep test ./rules --fixtures ./fixtures
```

## Test output

Each test prints `PASS` or `FAIL` with the test name. A summary line follows.

A passing run:

```
linear-tests.yaml:
  PASS  allow normal issue creation
  PASS  deny P0 creation
  PASS  deny deletion

3 tests, 3 passed, 0 failed
```

A failing run:

```
linear-tests.yaml:
  PASS  allow normal issue creation
  FAIL  deny P0 creation
        expected: deny (rule: no-auto-p0)
        got:      allow (rule: )
  PASS  deny deletion

3 tests, 2 passed, 1 failed
```

`keep test` exits with a non-zero status when any test fails.

## Organizing fixtures

Two common patterns:

- **One file per scope.** Name each file after the scope it tests (`linear-tests.yaml`, `anthropic-tests.yaml`). This groups related tests and keeps the file-level `scope` meaningful.
- **One file per scenario.** For scopes with many rules, split fixtures by scenario (`linear-creation.yaml`, `linear-deletion.yaml`). Each file still sets the same `scope`.

Store fixture files alongside your rules or in a sibling directory:

```
├── rules/
│   ├── linear.yaml
│   └── anthropic.yaml
└── fixtures/
    ├── linear-tests.yaml
    └── anthropic-tests.yaml
```

## CI integration

Add `keep test` to your CI pipeline to catch rule regressions on every change. The non-zero exit code on failure integrates with any CI system.

```yaml
# GitHub Actions example
- name: Test policy rules
  run: keep test ./rules --fixtures ./fixtures
```

If your rules reference profiles or starter packs, pass them explicitly:

```bash
$ keep test ./rules --fixtures ./fixtures --profiles ./profiles --packs ./packs
```

> **Note:** `keep test` runs all scopes in `enforce` mode regardless of the mode set in rule files. This ensures that `audit_only` rules are evaluated and tested the same way as enforced rules.

## Debugging failures

When a test fails, the output shows what was expected and what the engine returned.

**Decision mismatch** -- the engine returned a different decision than expected:

```
  FAIL  deny P0 creation
        expected: deny (rule: no-auto-p0)
        got:      allow (rule: )
```

Check that your rule's `match` block covers the operation and params in the fixture. Verify the `when` expression evaluates to true for the test's params.

**Rule mismatch** -- the decision is correct but a different rule produced it:

```
  FAIL  deny P0 creation
        expected rule: no-auto-p0
        got rule:      block-all-creates
```

A broader rule is matching first. Rules evaluate in order within a scope -- move the more specific rule above the broader one.

**Message mismatch** -- the decision and rule match but the message does not contain the expected substring:

```
  FAIL  deny P0 creation
        expected message to contain: "P0 issues"
        got message: "denied by policy"
```

Update the rule's `message` field or adjust `expect.message` to match the actual output.

Start with `keep validate ./rules` to confirm your rules are syntactically valid, then run `keep test` to verify behavior.
