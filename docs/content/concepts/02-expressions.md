---
title: "Expressions"
navTitle: "Expressions"
description: "How Keep uses CEL expressions for policy conditions — bounded, type-safe, and side-effect free."
keywords: ["keep", "cel", "expressions", "policy language", "custom functions"]
---

# Expressions

Keep uses CEL (Common Expression Language) for the `when` field in rules. CEL is a non-Turing-complete expression language designed by Google for exactly this kind of use case -- evaluating policy predicates in environments where safety and performance are non-negotiable.

CEL has no loops, no variable assignment, no unbounded recursion, and no side effects. Every expression evaluates in bounded time proportional to the size of its input. It is statically typed, catching errors at compile time rather than at call evaluation time.

This matters for a policy engine. Rules run on every API call. An expression that allocates unbounded memory or enters an infinite loop is a denial-of-service vulnerability in your own infrastructure. CEL eliminates that class of problem by construction -- the language itself makes it impossible to write an expression that does not terminate.

## What you write

A `when` expression receives two objects -- `params` and `context` -- and returns a boolean. If it returns `true`, the rule matches.

```yaml
rules:
  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
```

`params` contains the call's parameters -- the arguments the agent passed to the operation. `context` contains metadata about the call -- agent identity, timestamps, and any other ambient information the integration provides. A third variable, `now`, carries the call's timestamp for use with temporal functions.

Both `params` and `context` are dynamic maps. Any field access works at parse time; missing fields at evaluation time return `false` rather than an error. This means `params.some_field == "x"` is safe even when `some_field` is absent from the call. You do not need to guard against missing keys -- the engine handles that for you.

Expressions must return a boolean. If an expression returns a non-boolean type, evaluation fails with an error. This is caught at compile time for most cases, since CEL is statically typed.

## Built-in operators and methods

CEL provides standard operators and methods that work without any Keep-specific extensions. These cover the majority of policy conditions.

**Comparison and logic:** `==`, `!=`, `<`, `>`, `<=`, `>=`, `&&`, `||`, `!`, ternary (`condition ? a : b`). Comparisons are type-safe -- comparing a string to an integer is a compile-time error, not a runtime surprise.

**String methods:** `.contains()`, `.startsWith()`, `.endsWith()`, `.matches()` (RE2 regex), `.size()`. The `.matches()` method uses RE2 syntax, which guarantees linear-time evaluation -- consistent with CEL's bounded execution model.

**Collection operators:** `.exists()`, `.all()`, `.filter()`, `.map()`, `.size()`. These work on lists and let you express conditions like "at least one label is in this set" without writing loops. For example, `params.labels.exists(l, l == "urgent")` checks whether any label equals `"urgent"`.

**Membership:** `in` tests whether a value exists in a list. `params.team in ['TEAM-ENG', 'TEAM-INFRA']` returns true if the team is one of those values.

**Arithmetic:** `+`, `-`, `*`, `/`, `%` work on integers and doubles. Useful for threshold checks like `estimateTokens(params.body) > 10000`.

Keep adds custom functions for cases where standard CEL falls short -- time-of-day checks, rate counting, content scanning, and domain matching.

## Custom functions

Standard CEL covers comparison, string matching, and collection operations. But policy rules often need domain-specific checks -- is this request happening during business hours? Has this agent exceeded its rate limit? Does this response contain secrets?

Keep registers additional functions in its CEL environment to address these needs. They fall into four categories.

### Temporal

`inTimeWindow` and `dayOfWeek` evaluate time-based conditions. Both accept IANA timezone strings (like `"America/New_York"`) and operate on the `now` variable, which is injected from the call's timestamp.

Use these for policies like "deny deployments outside business hours" or "allow write operations only on weekdays." `inTimeWindow` takes start and end times as `"HH:MM"` strings in 24-hour format and returns true if the current time falls within that window. `dayOfWeek` returns a lowercase weekday name like `"monday"` or `"friday"`.

> **Note:** `inTimeWindow` does not support midnight-wrapping windows. A start time of `"22:00"` with an end time of `"06:00"` always returns false. Express overnight windows as two separate rules.

### Rate limiting

`rateCount` increments a counter keyed by a string and returns the count within a sliding window. The window is specified as a duration string like `"1h"`, `"30m"`, or `"30s"` (minimum 1 second, maximum 24 hours). The key is arbitrary -- use it to partition counts by agent, operation, or any combination.

This is the one function with a side effect -- it always increments the counter, even in `audit_only` mode. This is a known trade-off: suppressing the increment in audit mode would require threading enforcement state into the CEL function binding, and the counter would drift from reality if toggled.

Counters are local to the process and held in memory. Multiple relay or gateway instances maintain independent counts. If you need globally coordinated rate limits, enforce them upstream.

### Content detection

`containsAny` checks whether a string contains any term from a list, case-insensitively. It is a convenience over writing multiple `.contains()` calls chained with `||`.

`estimateTokens` returns a rough token count by dividing the character count (Unicode rune count) by four. This is an approximation, not a tokenizer -- it is fast enough to run on every call and accurate enough for threshold-based policies like "flag responses over 10,000 tokens."

`hasSecrets` runs gitleaks pattern detection against a string and returns true if it finds credentials, API keys, or other secret material. Use it to prevent agents from leaking secrets in generated content or tool call parameters.

> **Note:** `hasSecrets` uses regex patterns, not semantic analysis. It catches common credential formats but not every possible secret.

### String manipulation

`lower` and `upper` convert strings to lowercase or uppercase. These are useful for case-insensitive comparisons where you want exact matching rather than the substring search that `containsAny` provides.

`matchesDomain` extracts the domain from an email address and checks it against a list of allowed domains, including subdomains. `matchesDomain("user@eng.example.com", ["example.com"])` returns `true` because `eng.example.com` is a subdomain of `example.com`. This is useful for identity-based policies -- restricting operations to agents associated with specific organizational domains.

## Defs

Rules often repeat the same constants -- a list of allowed teams, a maximum priority value, a set of blocked terms. Duplicating these values across rules is error-prone: update one occurrence and forget another, and policy silently drifts.

The `defs` field at the top of a rule file solves this. Define a named constant once and reference it in any expression within that file.

```yaml
scope: linear-tools
defs:
  allowed_teams: "['TEAM-ENG', 'TEAM-INFRA']"
  max_priority: "1"
rules:
  - name: team-restriction
    match:
      operation: "create_issue"
      when: "!(params.team in allowed_teams)"
    action: deny

  - name: priority-cap
    match:
      operation: "create_issue"
      when: "params.priority < max_priority"
    action: deny
```

Defs are text substitution. Before a `when` expression is compiled, Keep replaces unqualified identifiers that match def names with their values. In the example above, `allowed_teams` becomes `['TEAM-ENG', 'TEAM-INFRA']` and `max_priority` becomes `1`. The resulting expression is then compiled and type-checked as normal CEL.

The substitution is scoped carefully. Field access paths like `params.allowed_teams` are not replaced -- only bare identifiers that are not preceded by a dot. String literals (both single- and double-quoted) are left untouched, so a def named `foo` inside a string like `"check foo"` is not rewritten.

Def names must be lowercase with underscores (`[a-z][a-z0-9_]*`) and cannot shadow built-in variables (`params`, `context`, `now`) or Keep's custom functions. Validation catches these conflicts at load time.

This is intentionally limited. Defs are constants, not macros. They do not support nesting, parameterization, or computed values. The constraint keeps rule files readable -- every `when` expression is still a valid CEL expression after substitution, and you can always understand a rule by mentally inlining the def values.

## Compilation and evaluation

Expressions go through two distinct phases: compilation and evaluation.

During compilation, Keep parses the expression, resolves any defs, type-checks it against the known variables (`params`, `context`, `now`) and registered functions, and produces an evaluable program. Errors at this stage -- syntax mistakes, type mismatches, references to unknown functions -- surface immediately during startup or validation with `keep validate`. A rule file with a broken expression never enters service.

At request time, the compiled program evaluates against the call's data. This separation means evaluation carries no parsing overhead -- just the cost of walking the expression tree and comparing values. The practical result is that rule evaluation adds negligible latency to API calls, regardless of how many rules exist in a scope.

## Bounded execution as a feature

The restrictions in CEL are not limitations to work around. They are the reason Keep uses it.

A policy engine that accepts arbitrary code requires sandboxing, resource limits, timeouts, and monitoring to prevent a single bad expression from affecting all traffic. CEL avoids this by making dangerous expressions impossible to write. There are no loops to run forever, no allocations to exhaust memory, no network calls to hang on.

The result: every `when` expression compiles once at load time and evaluates in microseconds at call evaluation time. There is no review process to decide which expressions are "safe enough" to deploy. There is no timeout configuration to tune. The language guarantees safety for all valid programs.

This trade-off -- giving up general computation in exchange for guaranteed termination -- is what makes it practical to evaluate policy on every API call without performance concerns or operational risk. If you find yourself needing a feature that CEL does not support, that is usually a signal that the logic belongs outside the expression layer -- in a custom function, in the calling application, or in a separate service.

## Related concepts

- [Calls and evaluation](01-calls-and-evaluation.md) -- how expressions fit into the broader rule evaluation model, including match semantics, rule ordering, and actions (deny, redact, log)
- [Scopes](03-scopes.md) -- how rules are organized into scopes, including profiles that map short names to `params.*` paths
