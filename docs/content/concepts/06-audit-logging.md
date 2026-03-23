---
title: "Audit logging"
navTitle: "Audit logging"
description: "How Keep logs every policy evaluation with structured JSON entries — what's captured and what's protected."
keywords: ["keep", "audit", "logging", "observability", "compliance"]
---

# Audit logging

Every policy evaluation produces a structured JSON log entry. Whether the decision is allow, deny, or redact, the audit log captures the full evaluation context: which rules were checked, which matched, and what the engine decided. This record exists independently of the decision itself -- a call that is allowed still generates an audit entry.

## Why it matters

Audit logs serve three purposes. First, they let you observe policy behavior before turning enforcement on. Second, they provide an immutable record of every decision the engine made, which matters for compliance. Third, they surface anomalous agent behavior -- repeated denials, unexpected operations, or redaction patterns that suggest misconfiguration.

## What's captured

Each audit entry contains these fields:

| Field | Description |
|-------|-------------|
| `Timestamp` | When the evaluation occurred |
| `Scope` | The scope the call was evaluated against |
| `Operation` | The operation name from the call |
| `AgentID` | Identity of the agent making the call |
| `UserID` | Identity of the user on whose behalf the agent acts |
| `Direction` | Whether the call is inbound or outbound |
| `Decision` | The outcome: `allow`, `deny`, or `redact` |
| `Rule` | The name of the rule that determined the decision |
| `Message` | The deny message or rule annotation |
| `RulesEvaluated` | Every rule checked, with match/no-match/skipped status |
| `ParamsSummary` | A truncated JSON snapshot of the call parameters |
| `Enforced` | Whether the decision was actually applied |
| `RedactSummary` | Paths and replacement values for redacted fields |

The `RulesEvaluated` array records every rule the engine checked for the call, not just the one that matched. Each entry includes the rule name, whether it matched, and its action. Rules that did not match the operation are marked as skipped. Rules that encountered a CEL evaluation error include the error message.

Here is an example JSON log entry for a denied call:

```json
{
  "Timestamp": "2026-03-23T14:05:22Z",
  "Scope": "linear-tools",
  "Operation": "delete_issue",
  "AgentID": "my-agent",
  "UserID": "user-42",
  "Direction": "outbound",
  "Decision": "deny",
  "Rule": "no-delete",
  "Message": "Issue deletion is not permitted. Archive instead.",
  "RulesEvaluated": [
    {"Name": "no-delete", "Matched": true, "Action": "deny"},
    {"Name": "no-auto-p0", "Skipped": true}
  ],
  "ParamsSummary": "{\"issue_id\":\"LIN-1234\"}",
  "Enforced": true
}
```

## Security properties

Audit entries are designed so they never contain pre-redaction secrets.

**Parameter truncation.** `ParamsSummary` is a JSON serialization of the call parameters, truncated to 256 runes. When truncated, an ellipsis marker (`...`) is appended. This bounds the size of each log entry and limits exposure of large parameter payloads.

**Redact summary.** The `RedactSummary` field records only the field path (e.g., `params.text`) and the post-redaction replacement text. It never includes the original value. The replacement text contains `[REDACTED:...]` placeholders where sensitive content was removed.

**Mutation stripping.** The mutations returned in the public `EvalResult` have their `Original` field cleared before leaving the engine. The pre-redaction value exists only during evaluation and is not persisted anywhere.

These properties hold regardless of output destination. Whether the audit log goes to stdout or a file, the entries contain the same safe subset of information.

## Output destinations

The audit logger writes JSON Lines (one JSON object per line) to one of three destinations:

- `stdout` -- the default. Writes to standard output.
- `stderr` -- writes to standard error, useful when stdout carries protocol traffic (e.g., the MCP relay in stdio mode).
- A file path -- creates or appends to the file with `0600` permissions (owner read/write only). This prevents other users on the system from reading audit entries.

The logger is thread-safe. Concurrent evaluations serialize writes under a mutex, so entries are never interleaved.

## Audit-only mode

When a scope is configured with `mode: audit_only`, the engine evaluates all rules and records the decision it *would* have made, but does not enforce it. Deny decisions are logged with `"Decision": "deny"` and `"Enforced": false`, while the actual call is allowed through.

In audit-only mode:

- All rules are evaluated, even after a deny match. In enforce mode, a deny short-circuits evaluation. In audit-only mode, the engine continues through all rules so the `RulesEvaluated` array is complete.
- Redact rules are skipped entirely -- mutations are neither computed nor applied. The `Decision` field shows `redact` only if a deny rule also matched, but `Enforced` is `false` and the call parameters are not modified.
- CEL evaluation errors are treated as non-matches rather than triggering fail-closed behavior.

> **Note:** CEL functions with side effects still execute in audit-only mode. `rateCount()` increments counters even when the decision is not enforced. This is a known limitation.

## Operational use

**Tune rules before enforcement.** Deploy with `mode: audit_only`, send production traffic, and review the audit log. Look for unexpected denials (rules too broad) or missing denials (rules too narrow). When the log shows the decisions you expect, switch to `mode: enforce`.

**Detect anomalous behavior.** Monitor audit logs for patterns: repeated denials from the same agent, operations outside the expected set, or sudden spikes in redaction. These patterns may indicate a misconfigured agent or an unexpected prompt injection.

**Satisfy compliance requirements.** Audit entries provide a timestamped, structured record of every policy decision. The `RulesEvaluated` array shows not just what was decided, but which rules were checked and why they did or did not match. This level of detail supports post-incident review and audit trails.

## Related concepts

- [Rules and scopes](./03-scopes.md) -- how rules are organized and evaluated
- [Redaction](./04-redaction.md) -- how field-level redaction works
