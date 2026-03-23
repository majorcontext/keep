---
title: "Calls and evaluation"
navTitle: "Evaluation"
description: "How Keep normalizes API calls and evaluates them against policy rules."
keywords: ["keep", "calls", "evaluation", "policy", "deny", "redact", "allow"]
---

# Calls and evaluation

Keep is a policy engine for AI agents. It intercepts calls that agents make -- tool invocations, LLM requests, library function calls -- evaluates them against declarative rules, and returns a decision: allow, deny, or redact.

This page explains the data model that makes that work and the evaluation logic that produces decisions.

## The call abstraction

Every interaction an agent has with an external service is normalized into a single structure: the **call**. A call has three parts:

- **Operation** -- a string naming what the agent is trying to do. For an MCP tool call this might be `create_issue`. For an LLM gateway request it might be `chat_completion`. The operation is how rules select which calls they apply to.
- **Params** -- a flat or nested map of the call's payload. For a tool call, these are the tool's input arguments. For an LLM request, this is the message body. Rules inspect params with CEL expressions and redact rules mutate them.
- **Context** -- metadata about the call's origin: agent identity, user identity, timestamp, scope name, direction (inbound or outbound), and arbitrary labels. Context fields let rules enforce policy based on who is making the call, not just what the call contains.

This normalization is the reason Keep works across transport layers. An MCP relay, an HTTP gateway, and a direct library call all construct the same `Call` structure before handing it to the engine. The rules don't know or care how the call arrived.

```go
type Call struct {
    Operation string
    Params    map[string]any
    Context   CallContext
}
```

## Rule matching and evaluation order

Rules are defined in YAML files, grouped by scope. Each rule has a match condition and an action. The match condition has two parts: an operation pattern (exact string or glob like `create_*`) and an optional `when` clause written in CEL.

At load time, the engine sorts rules by operation specificity:

1. **Exact operation** -- rules targeting a specific operation like `delete_issue` are evaluated first.
2. **Glob patterns** -- rules with wildcards like `create_*` or `llm.*` are evaluated next.
3. **Catch-all** -- rules with no operation (matching everything) are evaluated last.

Within the same specificity tier, rules preserve their order from the YAML file. This means you can write a specific deny rule and a broader catch-all rule in the same file, and the specific rule always fires first regardless of where it appears.

For each rule, the engine first checks whether the operation pattern matches the call's operation. If it does, and the rule has a `when` clause, the engine evaluates the CEL expression against the call's params and context. A rule matches only when both conditions are satisfied.

## Decisions

Evaluation produces one of three decisions:

**Allow** is the default. When no rule matches a call, the engine returns allow. There is no explicit allow action in rule files -- allow is what happens when policy has nothing to say about a call.

**Deny** blocks the call. The caller receives a structured result containing the rule name and an optional message explaining why the call was denied. Deny is a short-circuit: the first deny rule that matches ends evaluation immediately. No further rules run. This guarantees that a deny cannot be overridden by a later rule.

**Redact** modifies the call's params before it reaches the upstream service. Unlike deny, redact does not short-circuit. All matching redact rules run in order, and their mutations accumulate. Each subsequent redact rule sees the params as modified by previous rules. The caller receives the list of mutations to apply. This composability matters when multiple rules target different fields -- one rule might strip PII from a text field while another removes secrets from a code block.

**Log** is not a decision the caller sees. Log rules record that a match occurred in the audit trail but do not affect the call. Like redact, log does not short-circuit -- evaluation continues to the next rule.

The following table summarizes how each action affects evaluation flow:

| Action   | Stops evaluation | Mutates params | Returned to caller |
|----------|-----------------|----------------|-------------------|
| `deny`   | Yes             | No             | Yes (as deny)     |
| `redact` | No              | Yes            | Yes (as redact)   |
| `log`    | No              | No             | No (allow)        |

## Fail behavior

CEL expressions can fail at evaluation time -- a field might be missing, a type might not match, or a function might receive invalid input. The `on_error` setting controls what happens when this occurs.

**Closed** (the default) treats evaluation errors as denials. If a CEL expression in any rule fails, the engine immediately denies the call with a message identifying the failing rule and the error. This is the conservative choice: if the engine cannot determine whether a call is safe, it blocks it.

**Open** treats evaluation errors as non-matches. The failing rule is skipped and evaluation continues with the next rule. This is appropriate when you prefer availability over strictness -- a malformed param should not block all calls if the rule was only meant to catch a specific pattern.

The setting applies to all rules in a scope. You configure it in the rule file header:

```yaml
scope: my-scope
on_error: open
```

When unset, behavior defaults to closed.

## Audit-only mode

Every scope has a mode: `enforce` or `audit_only`. In enforce mode, deny and redact decisions are applied -- calls are blocked or mutated. In audit-only mode, the engine evaluates every rule and records what would have happened, but the decision returned to the caller is always allow.

This distinction is recorded in the audit entry. Each entry includes an `Enforced` field: `true` in enforce mode, `false` in audit-only mode. The audit entry's `Decision` field still reflects the policy outcome (deny or redact), even when the call was allowed through. This lets you review audit logs to understand what enforce mode would do before you turn it on.

Audit-only mode evaluates all rules, including rules that follow a deny match. In enforce mode, deny short-circuits and stops evaluation. In audit-only mode, the engine continues past the deny match so the audit trail captures every rule that would have fired. This gives a complete picture of policy behavior for tuning.

One thing to note: CEL functions with side effects still execute in audit-only mode. Rate-limiting counters, for example, increment regardless of mode. The audit trail records the true state, but the counters do not distinguish between audited and enforced evaluations.

```yaml
scope: my-scope
mode: audit_only
```

When unset, mode defaults to `audit_only`. The `keep test` command forces enforce mode via the `WithForceEnforce()` Go option to override all scopes during testing.

## What the caller receives

The engine returns an `EvalResult` containing:

- The **decision** (allow, deny, or redact).
- The **rule name** that produced the decision, if any.
- A **message** explaining the decision, for deny rules.
- A list of **mutations** to apply, for redact decisions. The caller is responsible for applying mutations to the original params before forwarding the call.
- An **audit entry** with the full evaluation trace: every rule checked, whether it matched, and the final outcome.

The caller -- whether a relay, gateway, or application code -- uses the decision to determine what to do next. A deny means the call should not proceed. A redact means the call should proceed with modified params. An allow means the call should proceed unchanged.
