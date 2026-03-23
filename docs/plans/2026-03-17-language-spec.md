# Keep Language Specification

**Configuration and Expression Language Reference**
**Draft v0.1 -- March 2026**

---

## Overview

Keep's configuration has two layers:

1. **Rule files** -- YAML documents that define policy. Loaded by the engine. Pure policy: no transport, no network, no protocol details.
2. **Expression language** -- CEL (Common Expression Language) expressions embedded in rule `when` clauses. Evaluated against the call object at runtime.

This document specifies both.

---

## Part 1: Rule File Format

Rule files are YAML documents. The engine loads all `.yaml` and `.yml` files from a rules directory and indexes them by scope name.

### Top-level structure

```yaml
scope: string               # required -- unique name for this rule set
profile: string              # optional -- name of a profile to load (field aliases)
mode: enforce | audit_only   # optional -- default: audit_only
on_error: closed | open      # optional -- default: closed (deny on CEL eval error)
defs: map(string, string)    # optional -- named constants substituted into CEL expressions
packs: []PackRef             # optional -- starter packs to import
rules: []Rule                # required -- list of rules
```

### Scope

A scope is a named collection of rules. Integration layers (relay, gateway, library callers) specify which scope to evaluate against when they call the engine. Scope names must be unique across all loaded rule files.

```yaml
scope: linear-tools
```

Scope names must match `[a-z][a-z0-9-]*` (lowercase, alphanumeric, hyphens). Maximum 64 characters.

### Mode

```yaml
mode: enforce        # rules are evaluated and enforced (deny blocks, redact mutates)
mode: audit_only     # rules are evaluated and logged but never enforced (default)
```

New scopes default to `audit_only`. This supports the deny/audit/tune workflow: deploy with observation, review logs, switch to enforce.

### On Error

```yaml
on_error: closed     # deny the call when a CEL expression errors (default)
on_error: open       # allow the call when a CEL expression errors
```

Controls behavior when a CEL `when` expression fails at evaluation time (e.g., type mismatch, unexpected null). In `closed` mode (the default), an evaluation error is treated as a deny -- the call is blocked immediately with an error message. In `open` mode, the error is recorded but the rule is treated as not matched, and evaluation continues.

In `audit_only` mode, errors are always treated as not matched regardless of `on_error`.

### Defs

`defs` is a map of named string constants. Each key is substituted as an identifier in CEL `when` expressions, using the same resolution mechanism as profile aliases. This allows extracting repeated values (lists, strings, thresholds) into named constants at the top of the file.

```yaml
scope: email-tools
defs:
  internal_domains: '["example.com", "example.org"]'
  max_recipients: "10"

rules:
  - name: external-recipients
    match:
      when: "params.to.exists(addr, !(addr in internal_domains))"
    action: deny
    message: "External recipients are not permitted."

  - name: too-many-recipients
    match:
      when: "size(params.to) > max_recipients"
    action: deny
    message: "Too many recipients."
```

Defs values are raw strings that are textually substituted into the expression before compilation. The value must be a valid CEL sub-expression (e.g., a list literal, string literal, or integer). Defs are resolved after profile aliases, so they can coexist with profiles.

Def names follow the same rules as alias names: `[a-z][a-z0-9_]*`, maximum 32 characters.

### Rules

A rule is the atomic unit of policy:

```yaml
rules:
  - name: string               # required -- unique within scope
    description: string         # optional -- human-readable explanation
    match:                      # required -- when does this rule apply?
      operation: string         # optional -- glob pattern for operation name
      when: string              # optional -- CEL expression evaluated against the call
    action: deny | log | redact # required -- what to do when the rule matches
    message: string             # optional -- returned to caller on deny
    redact:                     # required if action is redact
      target: string            # field path to scan
      patterns: []Pattern       # patterns to match and replace
```

### Rule evaluation order

1. Rules are evaluated in the order they appear in the file.
2. Pack rules are evaluated before inline rules.
3. Evaluation is **short-circuit on deny**: the first deny stops evaluation and returns immediately.
4. All `redact` rules that match are applied (mutations accumulate).
5. All `log` rules that match are recorded.
6. If no rule denies, the call is allowed.

### Name

Rule names must be unique within a scope. Used in audit logs and denial responses.

```yaml
- name: no-auto-p0
```

Names must match `[a-z][a-z0-9-]*`. Maximum 64 characters.

### Match

The `match` block determines whether a rule applies to a given call. Both fields are optional -- if omitted, the rule matches all calls in the scope.

**`operation`** -- a glob pattern matched against the call's operation name:

```yaml
match:
  operation: "create_issue"         # exact match
  operation: "create_*"             # glob: any operation starting with create_
  operation: "llm.tool_*"           # glob: llm.tool_result, llm.tool_use
  operation: "*"                    # matches everything (same as omitting)
```

Glob syntax: `*` matches any sequence of characters. `?` matches any single character. No regex -- use `when` for complex operation matching.

**`when`** -- a CEL expression that must evaluate to `true` for the rule to fire. The expression is evaluated against the call object (see Part 2):

```yaml
match:
  when: "params.priority == 0"
  when: "params.name == 'bash' && params.input.command.matches('rm -rf')"
  when: "!inTimeWindow(now, '09:00', '18:00', 'America/Los_Angeles')"
```

If both `operation` and `when` are present, both must match. If neither is present, the rule always matches (useful for blanket logging or redaction).

### Action

**`deny`** -- blocks the call. The engine returns `decision: "deny"` with the rule name and message. The integration layer translates this into the appropriate protocol error (MCP error, HTTP error, etc.).

```yaml
action: deny
message: "P0 issues must be created by a human. Use priority 1 or lower."
```

`message` is optional but strongly recommended for deny rules. It is returned to the agent and appears in the audit log.

**`log`** -- allows the call but records it in the audit log with this rule's name. Used for observation and soft monitoring.

```yaml
action: log
```

**`redact`** -- allows the call but mutates specified fields before forwarding. Requires a `redact` block:

```yaml
action: redact
redact:
  target: "params.content"
  patterns:
    - match: "AKIA[0-9A-Z]{16}"
      replace: "[REDACTED:AWS_KEY]"
    - match: "(?i)(password|secret|api_key|token)\\s*[=:]\\s*\\S+"
      replace: "[REDACTED:SECRET]"
```

`target` is a dot-path to the field to scan (see Field Paths below). The field value must be a string. Each pattern is a regex matched against the field value and replaced inline.

Multiple redact rules can match the same call. Mutations are applied in rule order. Each subsequent redact operates on the already-mutated value.

### Redact block

```yaml
redact:
  target: string          # required -- dot-path to the string field to scan
  secrets: bool           # optional -- enable gitleaks-based automatic secret detection
  patterns:               # optional -- list of patterns (required if secrets is false/omitted)
    - match: string       # required -- regex pattern (RE2 syntax)
      replace: string     # required -- replacement string
```

**`secrets`** -- when set to `true`, enables automatic secret detection on the target field using the gitleaks engine (~160 built-in patterns covering AWS keys, private keys, API tokens, database credentials, etc.). Detected secrets are replaced with `[REDACTED:<rule-id>]` where `rule-id` is the gitleaks rule identifier (e.g., `aws-access-key`). Secret detection runs before custom `patterns`, so custom patterns operate on already-redacted text. A redact block may use `secrets: true` alone, `patterns` alone, or both.

```yaml
# Automatic secret detection only
- name: strip-secrets
  match:
    operation: "llm.tool_result"
  action: redact
  redact:
    target: "params.content"
    secrets: true

# Combined: automatic secrets plus custom patterns
- name: strip-all-sensitive
  action: redact
  redact:
    target: "params.content"
    secrets: true
    patterns:
      - match: "(?i)ssn:\\s*\\d{3}-\\d{2}-\\d{4}"
        replace: "[REDACTED:SSN]"
```

Regex syntax is RE2 (linear-time, no backtracking). This is the same dialect used by Go's `regexp` package and CEL's `matches()` function.

### Patterns

The `match` field in a redact pattern is an RE2 regex. The `replace` field is a literal string (no capture group references in v1 -- the entire match is replaced).

```yaml
patterns:
  - match: "AKIA[0-9A-Z]{16}"
    replace: "[REDACTED:AWS_KEY]"

  - match: "(?i)(password|secret|api_key|token)\\s*[=:]\\s*\\S+"
    replace: "[REDACTED:SECRET]"

  - match: "-----BEGIN (RSA |EC )?PRIVATE KEY-----[\\s\\S]*?-----END \\1PRIVATE KEY-----"
    replace: "[REDACTED:PRIVATE_KEY]"
```

### Field paths

Field paths use dot notation to navigate the call object:

```
params.title                    # top-level param field
params.input.command            # nested param field
params.to                       # array field (used with collection functions in CEL)
context.agent_id                # context field
context.direction               # "request" | "response" | null
context.labels.sandbox_id       # label value
```

Field paths are used in:
- `redact.target` -- identifies the field to scan for patterns
- CEL expressions in `when` -- full dot-path access to any call field
- Profile aliases -- map short names to field paths

### Profiles

A profile is a YAML file that maps short alias names to field paths. Loaded by name from a profiles directory.

```yaml
# profiles/linear.yaml
name: linear
aliases:
  team:         "params.teamId"
  assignee:     "params.assigneeId"
  priority:     "params.priority"
  title:        "params.title"
  description:  "params.description"
  labels:       "params.labels"
  state:        "params.stateId"
  project:      "params.projectId"
```

When a scope references a profile, the engine resolves aliases before evaluating CEL expressions:

```yaml
scope: linear-tools
profile: linear
rules:
  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "priority == 0"          # resolved to params.priority == 0
```

Alias resolution rules:
1. If an identifier in a CEL expression matches an alias name, replace it with the alias path.
2. If it matches a built-in function name, leave it as-is.
3. If it matches `params.*` or `context.*`, leave it as-is (explicit paths always work).
4. Aliases must not shadow built-in functions or `params`/`context` keywords.

Alias names must match `[a-z][a-z0-9_]*`. Maximum 32 characters.

### Starter packs

A starter pack is a YAML file containing a reusable set of rules:

```yaml
# starter-packs/linear-safe-defaults.yaml
name: linear-safe-defaults
profile: linear
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "priority == 0"
    action: deny
    message: "P0 issues must be created by a human."
```

Referenced from a rule file:

```yaml
scope: linear-tools
profile: linear
packs:
  - name: linear-safe-defaults
  - name: linear-safe-defaults
    overrides:
      no-auto-p0: disabled                       # remove this rule entirely
      no-delete:
        message: "Deletion blocked. Use archive."  # override just the message
        when: "params.reason != 'duplicate'"       # add/replace the when clause
```

Pack reference schema:

```yaml
packs:
  - name: string               # required -- matches the pack's name field
    overrides:                  # optional -- per-rule overrides
      <rule-name>: disabled     # remove the rule
      <rule-name>:              # partial override (merged with pack rule)
        when: string            # replace the when clause
        message: string         # replace the message
        action: string          # replace the action
```

Override merge semantics:
- `disabled` removes the rule entirely.
- Any other override is a shallow merge: specified fields replace the pack rule's fields, unspecified fields are inherited.
- You cannot override `name` or `operation` -- use a new inline rule instead.

Pack rules are evaluated before inline rules. Within packs, rules are evaluated in the order they appear in the pack file.

---

## Part 2: Expression Language (CEL)

Keep uses CEL (Common Expression Language) for `when` expressions. CEL is a non-Turing-complete expression language designed for evaluating boolean conditions against structured data. It guarantees termination, has no side effects, and evaluates in linear time.

Reference: https://github.com/google/cel-spec

Keep uses CEL with the following custom environment.

### Input variables

Every CEL expression has access to these variables:

```
params      map(string, dyn)     // the call's parameters
context     Context              // the call's metadata
now         timestamp            // context.timestamp, for use in temporal functions
```

Context fields:

```
context.agent_id      string
context.user_id       string (may be empty)
context.timestamp     timestamp
context.scope         string
context.direction     string ("request" | "response" | "")
context.labels        map(string, string)
```

When a profile is loaded, its aliases are available as top-level variables:

```
// with linear profile loaded:
priority    == params.priority
team        == params.teamId
title       == params.title
```

### Types

CEL is strongly typed. Keep's environment uses these types:

| Type | Examples | Notes |
|---|---|---|
| `bool` | `true`, `false` | |
| `int` | `0`, `1`, `42`, `-1` | 64-bit signed |
| `double` | `3.14`, `0.5` | 64-bit float |
| `string` | `"hello"`, `'hello'` | Single or double quotes |
| `bytes` | `b"..."` | Rarely used in Keep |
| `list` | `[1, 2, 3]`, `["a", "b"]` | Homogeneous recommended |
| `map` | `{"key": "value"}` | String keys |
| `null_type` | `null` | |
| `timestamp` | | Via `context.timestamp` |
| `duration` | | Via duration functions |

### Operators

**Comparison:** `==`, `!=`, `<`, `>`, `<=`, `>=`

**Arithmetic:** `+`, `-`, `*`, `/`, `%`

**Logical:** `&&`, `||`, `!`

**Ternary:** `condition ? true_value : false_value`

**Membership:** `x in [list]`, `key in map`

**String concatenation:** `"hello" + " " + "world"`

### Standard string functions

```
s.contains(substr)          // true if s contains substr
s.startsWith(prefix)        // true if s starts with prefix
s.endsWith(suffix)          // true if s ends with suffix
s.matches(regex)            // true if s matches RE2 regex
s.size()                    // length of string
```

Examples:

```cel
params.text.contains("@here")
params.branch.startsWith("agent/")
params.to[0].endsWith("@company.com")
params.content.matches("AKIA[0-9A-Z]{16}")
```

### Standard collection functions

```
list.size()                          // number of elements
list.exists(x, predicate)           // true if any element satisfies predicate
list.all(x, predicate)              // true if all elements satisfy predicate
list.filter(x, predicate)           // returns elements matching predicate
list.map(x, transform)             // returns transformed elements
list.exists_one(x, predicate)      // true if exactly one element matches
```

Keep provides `any()` and `all()` as aliases for `exists()` and `all()`:

```cel
// these are equivalent:
params.to.exists(addr, addr.endsWith("@company.com"))
params.to.any(addr, addr.endsWith("@company.com"))

// these are equivalent:
params.to.all(addr, addr.endsWith("@company.com"))
```

Examples:

```cel
params.to.any(addr, addr.endsWith("@competitor.com"))
params.labels.all(l, l != "secret")
size(params.to) + size(params.cc) > 10
```

### Custom functions: temporal

All temporal functions take an explicit `now` parameter. `now` is a top-level CEL variable of type `timestamp`, automatically bound to `context.timestamp` at eval time.

```
inTimeWindow(now: timestamp, start: string, end: string, tz: string) -> bool
```

Returns true if `now` falls within the given time window. `start` and `end` are `HH:MM` in 24-hour format. `tz` is an IANA timezone name.

```cel
inTimeWindow(now, "09:00", "18:00", "America/Los_Angeles")
inTimeWindow(now, "00:00", "06:00", "UTC")
```

The window does not wrap across midnight. `inTimeWindow(now, "22:00", "06:00", tz)` is always false. To express overnight windows, use `||`:

```cel
inTimeWindow(now, "22:00", "23:59", "US/Eastern") || inTimeWindow(now, "00:00", "06:00", "US/Eastern")
```

```
dayOfWeek(now: timestamp) -> string
```

Returns the lowercase day name from `now` in UTC: `"monday"`, `"tuesday"`, etc.

```cel
dayOfWeek(now) in ["saturday", "sunday"]
!(dayOfWeek(now) in ["saturday", "sunday"])
```

```
dayOfWeek(now: timestamp, tz: string) -> string
```

Returns the day name in the specified timezone:

```cel
dayOfWeek(now, "America/Los_Angeles") == "friday"
```

### Custom functions: content patterns

```
containsAny(field: string, terms: list(string)) -> bool
```

Returns true if the string field contains any of the given terms (case-insensitive substring match):

```cel
containsAny(params.title, ["acquisition", "merger", "RIF", "layoff"])
containsAny(params.text, ["<!here>", "<!channel>"])
```

PII and PHI detection is handled via explicit regex patterns in redact rules targeting specific fields, rather than built-in functions. This avoids a false sense of security from shallow regex wrappers and makes the detection logic transparent and auditable. See the redact block syntax in Part 1.

### Custom functions: string manipulation

```
lower(s: string) -> string
```

Returns the lowercase version of the string:

```cel
lower(params.status) == "active"
```

```
upper(s: string) -> string
```

Returns the uppercase version of the string:

```cel
upper(params.code) == "US"
```

```
matchesDomain(email: string, domains: list(string)) -> bool
```

Returns true if the email address belongs to one of the given domains. Extracts the domain from the email (after `@`) and checks for exact match or subdomain match (case-insensitive). Returns false if the input is not a valid email address.

```cel
matchesDomain(params.to, ["example.com", "company.org"])
matchesDomain(params.from, ["competitor.com"])         // also matches user@sub.competitor.com
```

### Custom functions: secret detection

```
hasSecrets(field: string) -> bool
```

Returns true if the gitleaks engine detects any secrets in the string. Uses the same ~160 built-in patterns as `secrets: true` in the redact block. Useful in `when` clauses to deny or log calls that contain secrets without redacting them.

```cel
hasSecrets(params.content)
hasSecrets(params.input.command)
```

Requires a secret detector to be configured in the engine. If no detector is configured, returns false.

### Custom functions: rate limiting

```
rateCount(key: string, window: string) -> int
```

Returns the number of calls matching the given key within the sliding time window. The key is an arbitrary string, typically constructed from scope and agent identity:

```cel
rateCount("linear:create:" + context.agent_id, "1h")
rateCount("gmail:send:" + context.agent_id, "1h")
rateCount("anthropic:calls:" + context.agent_id, "24h")
```

Window format: integer followed by unit. Supported units:
- `s` -- seconds
- `m` -- minutes
- `h` -- hours

Examples: `"30s"`, `"5m"`, `"1h"`, `"24h"`

Maximum window: `24h`. Minimum: `1s`.

Implementation note: `rateCount()` is the one function that is not purely stateless. It requires a local counter store (in-memory or embedded KV). The counter is incremented on every call that reaches this function, regardless of the rule's final evaluation result. This means rate counters reflect attempted calls, not just allowed ones.

### Custom functions: utility

```
size(v: string | list | map) -> int
```

Returns the length of a string, list, or map. This is standard CEL.

```cel
size(params.to) > 10
size(params.content) > 100000
```

```
estimateTokens(v: string) -> int
```

Returns a rough token count for a string (characters / 4, approximately). Not a precise tokenizer -- intended for order-of-magnitude checks.

```cel
estimateTokens(params.content) > 50000
```

### Null safety

CEL expressions fail-open on missing fields by default. In Keep's configuration, the engine wraps field access to return `null` for missing fields rather than erroring. Use explicit null checks when needed:

```cel
// safe -- returns false if params.branch doesn't exist
has(params.branch) && params.branch == "main"

// also safe -- Keep's null-safe wrapper makes this work
params.branch == "main"    // false if params.branch is null
```

The `has()` macro is the standard CEL way to test field existence:

```cel
has(params.assigneeId)
has(context.labels.sandbox_id)
```

### Expression size limits

Maximum expression length: 2048 characters. Expressions exceeding this limit are rejected at load time. If you hit this limit, decompose into multiple rules.

Maximum expression evaluation time: 5ms. Expressions that exceed this are terminated and treated as a deny (fail-closed).

---

## Part 3: Integration Config Files

Integration configs are loaded by the convenience binaries (`keep-mcp-relay`, `keep-llm-gateway`), not by the engine. They define transport concerns only.

### MCP relay config

```yaml
# keep-mcp-relay.yaml

listen: ":8090"                    # required -- address:port to listen on
rules_dir: "./rules"              # required -- path to rule files directory
profiles_dir: "./profiles"        # optional -- path to profiles directory
packs_dir: "./starter-packs"      # optional -- path to starter packs directory

routes:                            # required -- list of upstream MCP servers
  - scope: linear-tools            # required -- binds to a scope in rule files
    upstream: "https://mcp.linear.app/mcp"
    auth:                          # optional -- auth for upstream connection
      type: bearer
      token_env: "LINEAR_API_KEY"  # read token from environment variable

  - scope: slack-tools
    upstream: "https://slack-mcp.example.com"
    auth:
      type: bearer
      token_env: "SLACK_BOT_TOKEN"

  - scope: github-tools
    upstream: "https://api.githubcopilot.com/mcp/"
    auth:
      type: bearer
      token_env: "GITHUB_TOKEN"

  - scope: sqlite-tools             # stdio subprocess transport
    command: "uvx"
    args: ["mcp-server-sqlite", "--db-path", "./data.db"]

log:                               # optional -- audit log config
  format: json                     # json | text
  output: stdout                   # stdout | stderr | file path
```

Route fields:

| Field | Required | Description |
|---|---|---|
| `scope` | yes | Scope name -- must match a scope in rule files |
| `upstream` | conditional | URL of the upstream MCP server (HTTP/SSE transport) |
| `command` | conditional | Executable to launch as a stdio subprocess |
| `args` | no | Arguments for the `command` subprocess |
| `auth` | no | Authentication for the upstream connection |
| `auth.type` | yes (if auth) | `bearer`, `header`, or `passthrough` |
| `auth.token_env` | no | Environment variable containing the token |
| `auth.header` | no | Header name for `type: header` |

Each route must specify exactly one transport: either `upstream` (HTTP/SSE) or `command` (stdio subprocess). They are mutually exclusive.

Auth types:
- `bearer` -- sends `Authorization: Bearer <token>` to upstream
- `header` -- sends a custom header with the token value
- `passthrough` -- forwards the agent's auth headers to upstream (for Moat integration)

### LLM gateway config

```yaml
# keep-llm-gateway.yaml

listen: ":8080"                    # required
rules_dir: "./rules"              # required
profiles_dir: "./profiles"        # optional
packs_dir: "./starter-packs"      # optional

provider: anthropic                # required -- anthropic | openai
upstream: "https://api.anthropic.com"  # required
scope: anthropic-gateway           # required -- scope name for rule evaluation

decompose:                         # optional -- controls block-level decomposition
  tool_result: true                # emit llm.tool_result calls (default: true)
  tool_use: true                   # emit llm.tool_use calls (default: true)
  text: false                      # emit llm.text calls (default: false)
  request_summary: true            # emit llm.request summary (default: true)
  response_summary: true           # emit llm.response summary (default: true)

log:
  format: json
  output: stdout
```

Gateway fields:

| Field | Required | Description |
|---|---|---|
| `provider` | yes | LLM provider: `anthropic` or `openai` |
| `upstream` | yes | URL of the LLM provider API |
| `scope` | yes | Scope name for rule evaluation |
| `decompose` | no | Which block types to decompose into Keep calls |

The `decompose` section controls which content blocks are extracted and evaluated as individual Keep calls. Disabling a block type means the engine never sees it -- useful for performance (skip text blocks) or to avoid false positives.

---

## Appendix A: Complete rule file example

```yaml
# rules/linear.yaml
scope: linear-tools
profile: linear
mode: enforce
packs:
  - name: linear-safe-defaults
    overrides:
      no-auto-p0: disabled

rules:
  - name: team-allowlist
    description: "Restrict issue creation to approved teams"
    match:
      operation: "create_issue"
      when: "!(team in ['TEAM-ENG', 'TEAM-INFRA'])"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure."

  - name: no-sensitive-content
    description: "Block issues referencing M&A or workforce changes"
    match:
      operation: "create_issue"
      when: >
        containsAny(title, ['acquisition', 'merger', 'RIF', 'layoff'])
        || containsAny(description, ['acquisition', 'merger', 'RIF', 'layoff'])
    action: deny
    message: "Issue contains sensitive business terms. Create manually."

  - name: issue-creation-rate
    description: "Prevent runaway issue creation"
    match:
      operation: "create_issue"
      when: "rateCount('linear:create:' + context.agent_id, '1h') > 20"
    action: deny
    message: "Rate limit exceeded. Maximum 20 issues per hour."

  - name: no-close-issues
    description: "Only humans can close or cancel issues"
    match:
      operation: "update_issue"
      when: "state in ['done', 'cancelled']"
    action: deny
    message: "Agents cannot close or cancel issues."

  - name: audit-all-reads
    description: "Log all read operations"
    match:
      operation: "search_*"
    action: log

  - name: audit-gets
    match:
      operation: "get_*"
    action: log
```

## Appendix B: Complete LLM scope rule file example

```yaml
# rules/anthropic.yaml
scope: anthropic-gateway
mode: enforce

rules:
  # -- request direction: what the model sees --

  - name: redact-secrets
    description: "Strip secrets from tool results before they reach the model"
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "AKIA[0-9A-Z]{16}"
          replace: "[REDACTED:AWS_KEY]"
        - match: "(?i)(password|secret|api_key|token)\\s*[=:]\\s*\\S+"
          replace: "[REDACTED:SECRET]"
        - match: "-----BEGIN (RSA |EC )?PRIVATE KEY-----[\\s\\S]*?-----END \\1PRIVATE KEY-----"
          replace: "[REDACTED:PRIVATE_KEY]"

  # PHI detection is not handled by a built-in function. For structured PHI
  # patterns (MRN, ICD codes, labeled fields), use explicit regex patterns in
  # redact rules targeting specific fields. Unstructured PHI detection will be
  # addressed via pluggable predicates (LLM-as-judge) in the future.

  - name: block-db-results
    description: "Don't let the model see raw database query results"
    match:
      operation: "llm.tool_result"
      when: "params.tool_name == 'sql_query'"
    action: deny
    message: "Database query results are not permitted in model context."

  - name: context-size-limit
    match:
      operation: "llm.request"
      when: "params.token_estimate > 150000"
    action: deny
    message: "Context exceeds 150k token limit."

  # -- response direction: what the model does --

  - name: block-destructive-bash
    description: "Prevent destructive shell commands"
    match:
      operation: "llm.tool_use"
      when: >
        params.name == "bash"
        && params.input.command.matches("rm -rf|DROP TABLE|TRUNCATE|mkfs|dd if=")
    action: deny
    message: "Destructive command blocked."

  - name: tool-denylist
    description: "Block networking tools"
    match:
      operation: "llm.tool_use"
      when: "params.name in ['curl', 'wget', 'nc', 'ssh']"
    action: deny
    message: "Networking tools are blocked in this sandbox."

  - name: model-call-rate
    match:
      operation: "llm.request"
      when: "rateCount('anthropic:calls:' + context.agent_id, '1h') > 200"
    action: deny
    message: "Rate limit exceeded. Maximum 200 model calls per hour."

  - name: audit-all
    match:
      operation: "llm.*"
    action: log
```

## Appendix C: Grammar summary (pseudo-BNF)

```
RuleFile       = ScopeDecl ProfileDecl? ModeDecl? OnErrorDecl? DefsDecl? PackRefs? Rules
ScopeDecl      = "scope:" SCOPE_NAME
ProfileDecl    = "profile:" IDENTIFIER
ModeDecl       = "mode:" ("enforce" | "audit_only")
OnErrorDecl    = "on_error:" ("closed" | "open")
DefsDecl       = "defs:" Def+
Def            = ALIAS_NAME ":" STRING
PackRefs       = "packs:" PackRef+
PackRef        = "- name:" IDENTIFIER Overrides?
Overrides      = "overrides:" Override+
Override       = RULE_NAME ":" ("disabled" | OverrideFields)
OverrideFields = (WhenOverride | MessageOverride | ActionOverride)+
Rules          = "rules:" Rule+
Rule           = "- name:" RULE_NAME
                 Description?
                 Match
                 Action
                 Message?
                 RedactBlock?
Match          = "match:" OperationMatch? WhenClause?
OperationMatch = "operation:" GLOB_PATTERN
WhenClause     = "when:" CEL_EXPRESSION
Action         = "action:" ("deny" | "log" | "redact")
Message        = "message:" STRING
RedactBlock    = "redact:" Target SecretsFlag? Patterns?
Target         = "target:" FIELD_PATH
SecretsFlag    = "secrets:" BOOL
Patterns       = "patterns:" Pattern+
Pattern        = "- match:" REGEX "replace:" STRING
Description    = "description:" STRING

ProfileFile    = "name:" IDENTIFIER Aliases
Aliases        = "aliases:" Alias+
Alias          = ALIAS_NAME ":" FIELD_PATH

PackFile       = "name:" IDENTIFIER ProfileDecl? Rules

SCOPE_NAME     = [a-z][a-z0-9-]{0,63}
RULE_NAME      = [a-z][a-z0-9-]{0,63}
IDENTIFIER     = [a-z][a-z0-9_-]{0,63}
ALIAS_NAME     = [a-z][a-z0-9_]{0,31}
GLOB_PATTERN   = string with * and ? wildcards
FIELD_PATH     = dotted path (params.field.subfield)
CEL_EXPRESSION = valid CEL expression (max 2048 chars)
REGEX          = RE2 regular expression
STRING         = YAML string (quoted or unquoted)
```