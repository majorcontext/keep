# Case-Insensitive Matching for the Keep Engine

**Date:** 2026-03-24
**Status:** Proposed

## Problem

Rule authors can write CEL expressions like `params.name == 'bash'` that silently fail when the upstream sends `"Bash"` or `"BASH"`. The `lower()` function exists but must be remembered — forgetting it creates rules that pass testing but miss real traffic. This is a dangerous failure mode for a security policy engine.

## Design

### Default: case-insensitive matching

The engine normalizes all string values to lowercase before CEL evaluation. This applies to:

1. **Operation glob matching** — both pattern and operation name are lowered before `path.Match()`.
2. **`params` map** — all string values are deep-lowered recursively (including nested maps and slices).
3. **`context` map** — all string fields (`agent_id`, `user_id`, `direction`, `scope`, labels) are lowered.

Rule authors write lowercase string literals. `params.name == 'bash'` matches regardless of upstream casing.

### Preserved originals for secrets and redaction

The evaluator preserves a reference to the original (pre-normalization) params before lowering: `originalParams := call.Params`. This original map is used in two places:

- **`hasSecrets()` CEL function** — needs original-case values because gitleaks patterns match exact token formats (e.g., `AKIA[0-9A-Z]{16}`, `sk-live-...`). The function is modified to accept an `originalParams` reference at environment construction time, and looks up the original value by field path rather than using the (lowered) CEL input. In `case_sensitive` scopes, originals and normalized values are identical.
- **Redaction stage** — both gitleaks secret detection (`ev.secrets.Redact()`) and custom regex patterns (`redact.Apply()`) operate on `originalParams`, not the normalized `celParams`. This ensures patterns match the actual casing and redacted output preserves the original text structure. After redaction produces mutations, those mutations are applied to both `originalParams` (for subsequent redact rules) and `celParams` (so CEL `when` clauses in later rules see redacted content).

**`matches()` (standard CEL)** operates on lowered values, consistent with all other CEL comparisons. Rule authors writing regex patterns should use case-insensitive character classes (e.g., `[a-z]` instead of `[A-Z]`) or expect lowercase input. Note: existing rules with patterns like `[A-Z]{3}-\\d+` will stop matching — the linter cannot catch this automatically since it's inside a regex, but the migration guide should call this out.

### Escape hatch: per-scope `case_sensitive`

Scopes can opt out of normalization:

```yaml
scope: credential-vault
case_sensitive: true
mode: enforce
rules:
  - name: match-exact-token
    match:
      operation: "vault.lookup"
      when: "params.token == 'sk-live-abc123'"
    action: deny
```

When `case_sensitive: true`, the engine skips all normalization for that scope. Inputs reach CEL with their original casing. This is the pre-change behavior.

### Validation: warn on uppercase literals

`keep validate` gains a new lint check that warns when CEL expressions contain uppercase characters in string literals:

```
Warning: rule "block-bash" contains uppercase string literal 'Bash'.
  Inputs are lowered by default — use lowercase literals or set case_sensitive: true.
```

This check is skipped for scopes with `case_sensitive: true`.

The warning applies to string literals in `when` expressions, operation patterns, and non-regex `defs` values. It does not apply to regex patterns in `redact.patterns[].match` (those operate on original values). For `defs`, the linter warns after alias/defs resolution so that expanded expressions are checked.

### Scope of normalization

| Layer | Normalized? | Notes |
|-------|-------------|-------|
| `Call.Operation` | Yes | Lowered before glob matching |
| `Call.Params` string values | Yes | Deep recursive lowering; map keys are NOT lowered (keys are field names controlled by consumers, not user input) |
| `Call.Context` string fields | Yes | `agent_id`, `user_id`, `direction`, `scope`, label values |
| `Call.Context.Timestamp` | N/A | Not a string |
| `Call.Context.Labels` keys | Yes | Label keys are lowered |
| String literals in CEL | No | Rule author responsibility; validated by linter |
| Regex patterns (`redact.patterns[].match`) | No | Operate on original values |
| Gitleaks secret detection | No | Operates on original values |
| Redact replacement strings | No | Output preserves original structure |

### Impact on existing custom functions

| Function | Change needed |
|----------|---------------|
| `lower()` / `upper()` | No-op in default mode (input already lowered). Still useful in `case_sensitive` scopes. |
| `containsAny()` | Already case-insensitive. Lowered input is harmless — double-lowering is idempotent. |
| `matchesDomain()` | Already case-insensitive. No change needed. |
| `hasSecrets()` | Modified to look up original-case values via preserved `originalParams` reference. |
| `matches()` | Receives lowered values (consistent with all other comparisons). |
| `estimateTokens()` | Unaffected — character count, not content-sensitive. |
| `rateCount()` | Keys will be lowered (case-insensitive bucketing). Note: this merges previously-separate buckets for mixed-case agent IDs — intentional, since case-insensitive identity is the goal. |
| `inTimeWindow()` / `dayOfWeek()` | Unaffected — operate on timestamps. |

### Implementation location

Normalization lives in `engine.Evaluator.Evaluate()` — the single entry point for all evaluation. This means:

- Consumers (relay, gateway, CLI `test` command) get normalization for free.
- No changes needed in `internal/relay/handler.go` or `internal/gateway/anthropic/decompose.go`.
- The `Evaluator` stores a `caseSensitive bool` field set at construction time. This requires adding `CaseSensitive bool` to `config.RuleFile` and passing it through `NewEvaluator`.

### Data flow

```
Call (original case)
  │
  ├─► Evaluator.Evaluate()
  │     │
  │     ├─► originalParams = call.Params          // preserve reference
  │     ├─► originalOp = call.Operation
  │     │
  │     ├─► if !caseSensitive:
  │     │     ├─► celParams = deepLowerStrings(call.Params)
  │     │     ├─► celCtx = lowerContext(call.Context)
  │     │     └─► normalizedOp = strings.ToLower(call.Operation)
  │     │
  │     ├─► Operation glob matching (uses normalizedOp)
  │     ├─► CEL evaluation (uses celParams, celCtx)
  │     │
  │     └─► Redaction:
  │           ├─► Secret detection: getNestedString(originalParams, keys)
  │           ├─► Regex patterns: redact.Apply(originalParams, ...)
  │           ├─► Mutations applied to BOTH originalParams and celParams
  │           └─► Mutations reference original field values
  │
  └─► EvalResult (mutations reference original values)
```

### Edge cases

- **Nil params:** `deepLowerStrings(nil)` returns nil. CEL's `Eval` already nil-checks params.
- **Non-string values:** `deepLowerStrings` skips non-string values (ints, bools, floats) and only recurses into `map[string]any` and `[]any`.
- **Empty strings:** Lowering is a no-op. No special handling needed.

### Audit trail

The audit entry records the **original** operation name and original params summary (via `paramsSummary(originalParams)` before redaction, or `paramsSummary(originalParams)` after redaction for the post-redaction summary). This preserves actual upstream values for debugging and forensics.

## Non-goals

- AST rewriting of CEL string literals (too complex, non-standard)
- Custom CEL operators (not supported by cel-go)
- Per-rule or per-field case sensitivity toggles (per-scope is sufficient)
- Changing CEL language semantics (we normalize inputs, not the language)

## Migration

This is a behavior change for existing deployments:

1. Rules using `lower()` will still work (double-lowering is idempotent).
2. Rules with uppercase string literals (e.g., `params.name == 'Bash'`) will **break** — they previously matched and will now fail because inputs are lowered. The `validate` linter will catch these.
3. Rules using `matches()` with uppercase character classes (e.g., `matches(params.text, '[A-Z]{3}-\\d+')`) will stop matching because the input is now lowercase. The linter cannot catch this automatically. Rule authors must update regex patterns to use lowercase classes or add `(?i)` flags. This should be called out prominently in upgrade documentation.
4. Scopes that need exact-case matching must add `case_sensitive: true`.

The `validate` command should be run against existing rule sets before upgrading to surface any uppercase literal warnings.
