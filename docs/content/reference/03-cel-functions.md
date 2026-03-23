---
title: "CEL functions reference"
navTitle: "CEL functions"
description: "Complete reference for all custom CEL functions available in Keep policy expressions."
keywords: ["keep", "cel", "functions", "reference", "expressions"]
---

# CEL functions reference

Keep rule expressions use CEL (Common Expression Language) with custom functions for temporal logic, rate limiting, content detection, string manipulation, and secret detection.

## Standard CEL

All built-in CEL operators, macros, and functions are available in Keep expressions. This includes:

- Arithmetic: `+`, `-`, `*`, `/`, `%`
- Comparison: `==`, `!=`, `<`, `>`, `<=`, `>=`
- Logical: `&&`, `||`, `!`
- String methods: `.contains()`, `.startsWith()`, `.endsWith()`, `.matches()`, `.size()`
- List operators: `.exists()`, `.all()`, `.filter()`, `.map()`, `in`
- Ternary: `condition ? a : b`

See the [CEL language spec](https://github.com/google/cel-spec/blob/master/doc/langdef.md) for the full list.

## Input variables

Every expression has access to three variables:

| Variable | Type | Description |
|----------|------|-------------|
| `params` | `dyn` | Call parameters -- fields vary by operation |
| `context` | `dyn` | Call metadata (agent ID, timestamp, etc.) |
| `now` | `timestamp` | Current timestamp, injected from `context.timestamp` |

Missing field accesses on `params` or `context` evaluate to `false` rather than returning an error.

## Temporal functions

### `inTimeWindow`

```
inTimeWindow(now, start, end, tz) -> bool
```

Returns `true` if `now` falls within the time-of-day window `[start, end)` in the given timezone.

| Parameter | Type | Description |
|-----------|------|-------------|
| `now` | `timestamp` | Timestamp to check (use the `now` variable) |
| `start` | `string` | Start time in `"HH:MM"` format (24-hour) |
| `end` | `string` | End time in `"HH:MM"` format (24-hour) |
| `tz` | `string` | IANA timezone name (e.g., `"America/New_York"`) |

**Example:**

```cel
inTimeWindow(now, "09:00", "17:00", "America/New_York")
```

> **Note:** Midnight-wrapping is not supported. If `end <= start`, the function returns `false`. To match overnight windows, use two rules or combine with `!`.

### `dayOfWeek`

```
dayOfWeek(now) -> string
dayOfWeek(now, tz) -> string
```

Returns the lowercase weekday name (e.g., `"monday"`, `"friday"`). The one-argument form uses UTC; the two-argument form converts to the given IANA timezone first.

| Parameter | Type | Description |
|-----------|------|-------------|
| `now` | `timestamp` | Timestamp to check (use the `now` variable) |
| `tz` | `string` | Optional. IANA timezone name |

**Example:**

```cel
dayOfWeek(now) == "saturday" || dayOfWeek(now) == "sunday"
```

```cel
dayOfWeek(now, "Asia/Tokyo") == "monday"
```

## Rate limiting

### `rateCount`

```
rateCount(key, window) -> int
```

Increments a counter for `key` and returns the number of hits within `window`. Use this to enforce rate limits per agent, per operation, or any arbitrary grouping.

| Parameter | Type | Description |
|-----------|------|-------------|
| `key` | `string` | Counter key -- typically built from context fields |
| `window` | `string` | Time window: `"30s"`, `"5m"`, `"1h"`. Min `1s`, max `24h` |

**Example:**

```cel
rateCount(context.agent_id + ":create_issue", "1h") > 100
```

> **Note:** `rateCount` always increments the counter, including during `audit_only` evaluation. Counters are local to the process and are not shared across instances.

## Content detection

### `containsAny`

```
containsAny(field, terms) -> bool
```

Returns `true` if `field` contains any of the strings in `terms`. Matching is case-insensitive.

| Parameter | Type | Description |
|-----------|------|-------------|
| `field` | `string` | Text to search |
| `terms` | `list(string)` | Substrings to match against |

**Example:**

```cel
containsAny(params.body, ["password", "secret", "api_key"])
```

### `estimateTokens`

```
estimateTokens(field) -> int
```

Returns a rough token count for `field`, calculated as `len(field) / 4`.

| Parameter | Type | Description |
|-----------|------|-------------|
| `field` | `string` | Text to estimate |

**Example:**

```cel
estimateTokens(params.prompt) > 10000
```

> **Note:** This is a byte-length heuristic, not a tokenizer. Actual token counts vary by model.

## String manipulation

### `lower`

```
lower(field) -> string
```

Returns `field` converted to lowercase.

**Example:**

```cel
lower(params.status) == "urgent"
```

### `upper`

```
upper(field) -> string
```

Returns `field` converted to uppercase.

**Example:**

```cel
upper(params.priority_label) == "P0"
```

### `matchesDomain`

```
matchesDomain(email, domains) -> bool
```

Returns `true` if the domain part of `email` matches any entry in `domains`. Subdomain matching is supported: `"eng.example.com"` matches `"example.com"`.

| Parameter | Type | Description |
|-----------|------|-------------|
| `email` | `string` | Email address to check |
| `domains` | `list(string)` | Allowed domain names |

**Example:**

```cel
matchesDomain(context.user_email, ["example.com", "example.org"])
```

> **Note:** Returns `false` if `email` does not contain an `@` character. Comparison is case-insensitive.

## Secret detection

### `hasSecrets`

```
hasSecrets(field) -> bool
```

Returns `true` if the field contains patterns that look like secrets (API keys, tokens, passwords). Detection uses gitleaks pattern rules.

| Parameter | Type | Description |
|-----------|------|-------------|
| `field` | `string` | Text to scan |

**Example:**

```cel
hasSecrets(params.message)
```

> **Note:** Pattern-based detection. It catches common secret formats but does not guarantee detection of all secrets, and false positives are possible.
