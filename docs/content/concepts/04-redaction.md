---
title: "Redaction"
navTitle: "Redaction"
description: "How Keep redacts sensitive content from API calls — patterns, secrets, and the mutation lifecycle."
keywords: ["keep", "redaction", "secrets", "mutation", "gitleaks", "sensitive data"]
---

# Redaction

Redaction modifies specific fields in a call's parameters before the call is forwarded. Unlike deny, which blocks the call entirely, redaction allows the call to proceed with sanitized content. The original values never leave the engine.

This page covers how redaction works internally: the mutation lifecycle, the secret detection layer, the security properties that prevent original values from leaking, and the audit trail that records what changed.

## Why redaction exists

Agents interact with APIs using freeform text fields -- message bodies, issue descriptions, code snippets, query parameters. These fields can contain sensitive content that the agent did not intend to expose: credentials copied from context, PII from previous conversation turns, or internal identifiers that should not leave the organization. Redaction addresses this by modifying the content in transit, without interrupting the agent's workflow.

## What redaction does

A redact rule targets a single field in the call's params (e.g., `params.text` or `params.body.content`) and applies regex-based replacements to its string value. The engine returns the modified params alongside the allow decision, and the caller forwards the mutated version to the upstream service.

This is useful when a call is legitimate but its content contains data that should not reach the destination -- credentials embedded in freeform text, personally identifiable information (PII), or values that violate organizational policy.

The distinction from deny matters:

- **Deny** blocks the entire call and returns a structured error to the agent. The upstream service never sees the call.
- **Redact** allows the call to succeed, but replaces specific content before the call reaches the upstream service. The agent receives a normal response.

Redaction is a scalpel where deny is a gate. An agent composing a message that happens to include a leaked API key does not need to be blocked from sending messages entirely -- it needs the key removed from the message content. The operation itself is legitimate; only the content is problematic.

## Mutation lifecycle

Redaction follows a deterministic sequence during evaluation:

1. **Match** -- the engine evaluates the rule's match conditions (operation, `when` expression). If the rule does not match, no redaction occurs.

2. **Locate target field** -- the engine navigates the params map using the dot-separated target path (e.g., `params.message.text`). The `params.` prefix is stripped, and the remaining segments are used to walk the nested map structure. If any segment along the path is missing, or if the final value is not a string, the rule is skipped silently.

3. **Apply secret detection** -- if the rule has `secrets: true`, the gitleaks-based detector runs first on the target field's current value. Each detected secret is replaced with a `[REDACTED:<rule-id>]` placeholder, where `<rule-id>` identifies the gitleaks pattern that matched (e.g., `[REDACTED:aws-access-key]`). The mutations from secret detection are applied to the working params immediately.

4. **Apply regex patterns** -- custom patterns from the rule run sequentially on the text. Each pattern's replacement is applied in order, and the output of one pattern becomes the input to the next. If secret detection already modified the text in the previous step, regex patterns operate on the post-detection result.

5. **Record mutations** -- if any replacement changed the text, the engine records a `Mutation` with the field path and the new value. The original pre-redaction value is stored temporarily in an internal field for the duration of evaluation but is never exposed externally (see [Security properties](#security-properties)).

6. **Update working params** -- the engine applies mutations to a deep copy of the params map so that subsequent redact rules in the same scope see the already-redacted values, not the originals.

Multiple redact rules can fire on the same call. Each rule operates on the output of the previous one. This means ordering in the rule file matters: a rule that runs second sees the mutations applied by the first. The engine accumulates all mutations across rules into a single list that is returned with the evaluation result.

This sequential model has an important consequence for secret detection. If two rules both target `params.text` and both have `secrets: true`, gitleaks runs twice -- once per rule. On the second run, secrets have already been replaced with `[REDACTED:...]` placeholders, so the detector finds nothing new. This is harmless but redundant. In practice, enable `secrets: true` on the first rule that targets a given field and use only custom patterns on subsequent rules for the same field.

## Secret detection

When a rule sets `secrets: true`, the engine runs automatic secret detection before custom regex patterns. The detector is built on [gitleaks](https://github.com/gitleaks/gitleaks), which provides approximately 160 built-in patterns covering API keys, tokens, passwords, and other credential formats across major services (AWS, GitHub, Slack, Stripe, and many others).

The detector scans the target field's text and returns a list of findings. Each finding includes:

- The matched text (the literal secret value)
- A rule ID identifying which pattern matched (e.g., `aws-access-key`, `github-pat`, `slack-webhook`)
- A human-readable description of the credential type

The engine replaces each match with `[REDACTED:<rule-id>]`, processing longer matches first to prevent partial replacements. This ordering ensures that a long token is not partially replaced by a shorter overlapping pattern.

Secret detection and custom regex patterns are complementary. Secret detection always runs first when enabled, providing a baseline of credential scrubbing. Custom patterns then handle domain-specific content that gitleaks does not cover -- internal account numbers, project-specific tokens, or content matching organizational policy.

> **Note:** Secret detection uses pattern matching, not semantic analysis. It catches known credential formats but does not identify secrets by context alone. A string that looks like an API key but is actually a hash will be redacted. A secret in an unusual format that no pattern covers will pass through.

## Security properties

The central invariant of redaction is that pre-redaction values never leave the engine. The `Original` field on the `Mutation` struct exists only so the engine can track what changed during evaluation. It is never returned to callers.

Three mechanisms enforce this invariant:

**Serialization guard.** The `Original` field carries the `json:"-"` struct tag. This prevents it from appearing in any JSON serialization, even if a caller accidentally marshals the full mutation list.

**Explicit zeroing.** Before the engine returns an `EvalResult`, it iterates over all mutations and sets each `Original` field to the empty string. This ensures the pre-redaction value is not accessible through the returned struct, regardless of serialization format or language-level reflection.

**Type-level separation.** The audit entry uses a separate `RedactedField` type that has no field for the original value at all -- only the field path and the post-redaction text. There is no way to populate the original value in audit output because the type does not have a place to put it.

These layers are deliberately redundant. Any single mechanism is sufficient to prevent leakage; all three operate together as defense in depth.

## Audit trail

Every mutation produces a `RedactedField` entry in the audit log. Each entry records:

- **Path** -- the dot-separated field path that was modified (e.g., `params.text`)
- **Replaced** -- the post-redaction value of the field

The audit entry includes these in its `RedactSummary` alongside the standard audit fields: timestamp, scope, operation, agent identity, user identity, direction, decision, and the list of rules evaluated. This gives operators visibility into which fields were modified and what the downstream service received, without exposing the sensitive content that was removed.

When multiple redact rules modify the same field, the audit trail contains one `RedactedField` entry per mutation. The final entry for a given path shows the value that was actually forwarded. Earlier entries show intermediate states. Together, they trace the full chain of transformations applied to the field.

This design means an operator reviewing audit logs can answer two questions without access to the original content: which fields were modified, and what text was actually sent to the upstream service. The audit trail is safe to store, forward to log aggregators, and include in compliance reports because it contains no pre-redaction secrets.

## How redaction interacts with other actions

Redaction and deny are independent decisions. If a call matches both a redact rule and a deny rule, the deny takes precedence -- the call is blocked, and no redacted params are returned. If a call matches redact and log rules but no deny rule, the call is allowed with redacted params and all matched rules appear in the audit entry.

In `audit_only` mode, redact rules are evaluated and mutations are recorded in the audit log, but the engine does not enforce the mutations. This allows operators to see what would be redacted in production without modifying actual traffic -- useful when tuning patterns or rolling out new rules.

Redaction does not modify the original params map passed to the engine. The engine deep-copies the params before applying mutations, so callers retain access to the unmodified input for their own use. The `EvalResult` contains the mutated params separately.

The deep copy is recursive -- nested maps within params are copied at every level. This prevents aliasing bugs where modifying the redacted params would inadvertently change the caller's original data structure.

## Design trade-offs

**String fields only.** Redaction operates on string values. If a target path resolves to a non-string value (a number, boolean, or nested object), the rule is silently skipped. This keeps the mutation model straightforward -- every mutation is a string-to-string replacement -- but means redaction cannot modify structured sub-objects directly.

**Single target per rule.** Each redact rule targets one field path. To redact multiple fields in the same call, define multiple rules targeting different paths. There is no wildcard or glob syntax for target paths.

**Sequential evaluation.** Pattern evaluation runs sequentially within a single rule and across rules. The engine does not parallelize regex execution. For most workloads this is not a concern, but rules with many complex regex patterns on large text fields add latency proportional to the number of patterns and the size of the text. Patterns are compiled once when the engine loads the rule file and reused for every evaluation.

**Fixed secret patterns.** The gitleaks pattern set is compiled once at engine startup and reused across evaluations. There is no mechanism to add custom patterns to the secret detector at runtime. Use custom regex patterns in the rule definition for organization-specific credential formats that gitleaks does not cover.

**No change, no mutation.** If the regex patterns match but the replacement string produces the same text as the original, the engine treats this as no mutation. No `RedactedField` entry is recorded, and the params are not modified. This avoids false positives in the audit trail.

## Related concepts

- [Evaluation](03-evaluation.md) -- how the engine processes rules, orders actions, and produces decisions
- [Expressions](02-expressions.md) -- CEL expressions used in rule match conditions, including `when` clauses on redact rules
- [Scopes and rules](01-scopes-and-rules.md) -- how rules are organized into scopes and how rule ordering affects evaluation
