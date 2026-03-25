---
title: "Rule file reference"
navTitle: "Rule file"
description: "Complete schema reference for Keep rule files — all fields, types, defaults, and constraints."
keywords: ["keep", "rule file", "schema", "reference", "yaml", "configuration"]
---

# Rule file reference

A rule file is a YAML document that declares a scope and its rules. The engine loads all `.yaml` and `.yml` files from a rules directory and indexes them by scope name.

## Top-level fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `scope` | `string` | Yes | — | Unique name for this rule set. Must match `[a-z][a-z0-9-]*`, max 64 characters. |
| `profile` | `string` | No | — | Name of a profile to load. Maps short aliases to field paths in CEL expressions. |
| `mode` | `string` | No | `audit_only` | `enforce` — rules are enforced (deny blocks, redact mutates). `audit_only` — rules are evaluated and logged but never enforced. |
| `on_error` | `string` | No | `closed` | `closed` — deny the call when a CEL expression errors. `open` — allow the call and skip the rule on error. In `audit_only` mode, errors are always treated as not matched. |
| `defs` | `map(string, string)` | No | — | Named constants substituted into CEL `when` expressions before compilation. |
| `packs` | `[]PackRef` | No | — | Starter packs to import. Pack rules are evaluated before inline rules. |
| `case_sensitive` | `bool` | No | `false` | When `false` (default), all string values in `params` and `context` are lowered before CEL evaluation. Set to `true` for exact-case matching. |
| `rules` | `[]Rule` | Yes | — | List of rules. Max 500 per scope. Evaluated in order; deny short-circuits. |

## Rule fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | `string` | Yes | — | Unique within scope. Must match `[a-z][a-z0-9-]*`, max 64 characters. Used in audit logs and denial responses. |
| `description` | `string` | No | — | Human-readable explanation. |
| `match` | `Match` | No | — | When this rule applies. If omitted, the rule matches all calls in the scope. |
| `action` | `string` | Yes | — | `deny` — block the call. `log` — allow and record. `redact` — allow and mutate fields. |
| `message` | `string` | No | — | Returned to the caller on deny. Recommended for deny rules. |
| `redact` | `RedactSpec` | Conditional | — | Required when `action` is `redact`. Defines what to redact and how. |

## Match block

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `operation` | `string` | No | — | Glob pattern matched against the call's operation name. |
| `when` | `string` | No | — | CEL expression evaluated against the call. Must return `bool`. Max 2048 characters. |

If both fields are present, both must match. If neither is present, the rule always matches.

### Operation glob syntax

| Pattern | Matches |
|---------|---------|
| `create_issue` | Exact match |
| `create_*` | Any operation starting with `create_` |
| `llm.tool_*` | `llm.tool_result`, `llm.tool_use`, etc. |
| `*` | All operations (same as omitting `operation`) |

Wildcards: `*` matches any sequence of characters. `?` matches any single character. Regex is not supported in `operation` — use `when` for complex matching.

## Redact block

Required when `action` is `redact`. Must have `secrets: true`, non-empty `patterns`, or both.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `target` | `string` | Yes | — | Dot-path to the string field to scan. Must be a `params.*` path. |
| `secrets` | `bool` | No | `false` | Enable automatic secret detection (~160 built-in patterns via gitleaks). Detected secrets are replaced with `[REDACTED:<rule-id>]`. Runs before custom `patterns`. |
| `patterns` | `[]Pattern` | No | — | List of regex patterns to match and replace. Max 50 patterns per redact block. |

### Pattern fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `match` | `string` | Yes | RE2 regex pattern. Must not be empty. |
| `replace` | `string` | Yes | Literal replacement string. The entire match is replaced (no capture group references). |

## Defs

Named string constants substituted into CEL `when` expressions before compilation. Each value must be a valid CEL sub-expression (list literal, string literal, integer, etc.).

### Def name constraints

| Constraint | Value |
|------------|-------|
| Pattern | `[a-z][a-z0-9_]*` |
| Max length | 64 characters |
| Value max length | 2048 characters |
| Value | Must not be empty |

Def names must not shadow reserved names. See [Reserved names](#reserved-names).

## Pack references

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Matches the starter pack's `name` field. |
| `overrides` | `map(string, any)` | No | Per-rule overrides. Key is the rule name in the pack. |

### Override values

| Value | Effect |
|-------|--------|
| `disabled` | Remove the rule entirely. |
| Map with `when`, `message`, or `action` | Shallow merge — specified fields replace the pack rule's fields, unspecified fields are inherited. |

You cannot override `name` or `operation`. Use a new inline rule instead.

## Constraints summary

| Constraint | Limit |
|------------|-------|
| Scope name pattern | `[a-z][a-z0-9-]*` |
| Scope name max length | 64 characters |
| Rule name pattern | `[a-z][a-z0-9-]*` |
| Rule name max length | 64 characters |
| Def name pattern | `[a-z][a-z0-9_]*` |
| Def name max length | 64 characters |
| Max rules per scope | 500 |
| Max patterns per redact block | 50 |
| Max `when` expression length | 2048 characters |
| Max def value length | 2048 characters |
| Regex syntax | RE2 (linear-time, no backtracking) |
| Rule names | Unique within scope |
| Scope names | Unique across all loaded rule files |

## Reserved names

Def names and profile alias names must not use any of the following identifiers:

| Category | Names |
|----------|-------|
| Top-level variables | `params`, `context`, `now` |
| CEL standard functions | `size`, `has`, `matches`, `startsWith`, `endsWith`, `contains`, `exists`, `all`, `filter`, `exists_one` |
| Keep custom functions | `containsAny`, `estimateTokens`, `inTimeWindow`, `rateCount`, `lower`, `upper`, `matchesDomain`, `dayOfWeek`, `hasSecrets` |
| CEL type identifiers | `int`, `uint`, `double`, `bool`, `string`, `bytes`, `list`, `map`, `type`, `null_type` |
| Literal keywords | `true`, `false`, `null` |

## Evaluation order

1. Pack rules are evaluated before inline rules.
2. Rules are evaluated in file order.
3. First `deny` match short-circuits — the call is blocked immediately.
4. All matching `redact` rules are applied in order (mutations accumulate).
5. All matching `log` rules are recorded.
6. If no rule denies, the call is allowed.

## Field paths

Field paths use dot notation to navigate the call object. Used in `redact.target` and CEL `when` expressions.

| Path | Description |
|------|-------------|
| `params.title` | Top-level parameter field |
| `params.input.command` | Nested parameter field |
| `params.to` | Array field (use collection functions in CEL) |
| `context.agent_id` | Agent identity |
| `context.direction` | `"request"`, `"response"`, or `""` |
| `context.labels.sandbox_id` | Label value |

Redact targets must start with `params.`.

## Complete example

```yaml
scope: linear-tools
profile: linear
mode: enforce
on_error: closed

defs:
  allowed_teams: '["team-eng", "team-infra"]'
  max_issues_per_hour: "20"

packs:
  - name: linear-safe-defaults
    overrides:
      no-auto-p0: disabled

rules:
  - name: team-allowlist
    description: "Restrict issue creation to approved teams"
    match:
      operation: "create_issue"
      when: "!(team in allowed_teams)"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure."

  - name: issue-creation-rate
    description: "Prevent runaway issue creation"
    match:
      operation: "create_issue"
      when: "rateCount('linear:create:' + context.agent_id, '1h') > max_issues_per_hour"
    action: deny
    message: "Rate limit exceeded. Maximum 20 issues per hour."

  - name: strip-secrets
    description: "Redact secrets from tool results"
    match:
      operation: "*"
    action: redact
    redact:
      target: "params.content"
      secrets: true
      patterns:
        - match: "(?i)ssn:\\s*\\d{3}-\\d{2}-\\d{4}"
          replace: "[REDACTED:SSN]"

  - name: audit-all-reads
    description: "Log all search and get operations"
    match:
      operation: "search_*"
    action: log

  - name: audit-gets
    match:
      operation: "get_*"
    action: log
```
