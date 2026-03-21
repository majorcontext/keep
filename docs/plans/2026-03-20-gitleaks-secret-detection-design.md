# Gitleaks Secret Detection Integration

**Goal:** Integrate gitleaks as a library to provide built-in secret detection for both redaction rules and CEL expressions, replacing hand-rolled regex patterns with ~160 vendor-specific patterns.

**Approach:** Thin wrapper in `internal/secrets` around gitleaks `detect.Detector`, surfaced as `secrets: true` on redact rules and `hasSecrets()` CEL function.

---

## Architecture

A single `secrets.Detector` instance is created at engine initialization and shared by both the redact system and the CEL environment. The gitleaks `detect.Detector` is safe for concurrent use (uses internal mutexes and semgroup).

```
Engine init
  └─ secrets.NewDetector()
       └─ gitleaks detect.NewDetectorDefaultConfig()
  └─ stored on Engine, passed to Evaluator
  └─ passed to cel.NewEnv (for hasSecrets() function)
```

The gitleaks dependency is contained entirely within `internal/secrets`. No other package imports gitleaks directly.

## Package: `internal/secrets`

### Types

```go
// Detector wraps the gitleaks detector for secret detection.
type Detector struct {
    d *detect.Detector
}

// Finding represents a detected secret.
type Finding struct {
    RuleID      string // e.g. "aws-access-key", "stripe-secret-key"
    Description string // human-readable description from gitleaks
    Match       string // the matched text
}
```

### Functions

```go
// NewDetector creates a Detector with gitleaks default config (~160 patterns).
// Returns error if gitleaks initialization fails.
func NewDetector() (*Detector, error)

// Detect scans text and returns all secret findings.
// Returns nil if no secrets found. Safe to call on nil receiver (returns nil).
func (d *Detector) Detect(text string) []Finding

// Redact scans text, replaces each secret with [REDACTED:<RuleID>],
// and returns the redacted string plus a slice of findings.
// The caller is responsible for constructing Mutations from the findings.
// Safe to call on nil receiver (returns original text, nil findings).
func (d *Detector) Redact(text string) (string, []Finding)
```

Note: `Redact` returns `[]Finding`, not `[]redact.Mutation`, to avoid a dependency from `internal/secrets` to `internal/redact`. The engine constructs `Mutation` values from the findings.

### Replacement format

Each detected secret is replaced with `[REDACTED:<RuleID>]` where `RuleID` comes from the gitleaks rule (e.g. `[REDACTED:aws-access-key]`, `[REDACTED:github-pat]`).

## Config changes

### RedactSpec

Add `Secrets` field to `internal/config.RedactSpec`:

```go
type RedactSpec struct {
    Target   string          `yaml:"target"`
    Secrets  bool            `yaml:"secrets,omitempty"`     // new
    Patterns []RedactPattern `yaml:"patterns,omitempty"`    // omitempty added
}
```

The `Patterns` YAML tag gains `omitempty` so that `secrets: true` rules without custom patterns serialize cleanly.

### Validation change

Update `validateRedact` in `internal/config/validate.go`: a redact rule is valid if it has `secrets: true` OR non-empty `patterns` (or both). Currently it rejects empty patterns unconditionally.

### Rule YAML

```yaml
rules:
  - name: redact-secrets
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true          # gitleaks-powered detection

  - name: redact-secrets-with-custom
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true          # gitleaks-powered detection
      patterns:              # optional custom patterns, run after secrets
        - match: "internal-[a-z]+-key"
          replace: "[REDACTED:INTERNAL]"
```

`secrets` and `patterns` compose: secrets run first, then custom patterns run on the already-redacted text. Note: custom patterns will see `[REDACTED:...]` strings from the secrets pass — users should avoid patterns that would match these placeholders.

## CEL function: `hasSecrets()`

```
hasSecrets(string) -> bool
```

Returns true if gitleaks detects any secrets in the input string. Used in `when:` clauses for deny/log rules:

```yaml
- name: block-secrets-in-tool-output
  match:
    operation: "llm.tool_result"
    when: "hasSecrets(params.content)"
  action: deny
  message: "Tool output contains secrets"
```

Registered in `internal/cel/env.go` via `cel.Function("hasSecrets", ...)`. The `Detector` is passed to `NewEnv` via a new `WithSecretDetector(*secrets.Detector)` option.

`hasSecrets` must be added to `reservedDefNames` in `internal/config/validate.go` to prevent user-defined `defs` from shadowing it.

## Engine wiring

The `Detector` is created unconditionally in `keep.Load()` and stored on the `Engine`:

```go
func Load(rulesDir string, opts ...Option) (*Engine, error) {
    // ...
    detector, err := secrets.NewDetector()
    if err != nil {
        return nil, fmt.Errorf("secrets detector: %w", err)
    }
    // pass to CEL env
    celEnv, err := cel.NewEnv(cel.WithSecretDetector(detector), ...)
    // store on engine, passed to Evaluator for redact rules
    engine.secrets = detector
}
```

The `Evaluator` (or `compiledRule`) receives the `*secrets.Detector` so it can call `detector.Redact()` when processing rules with `Secrets: true`.

Always-on: no configuration needed, no lazy initialization.

## Data flow: redact

1. Rule matches a call (operation + optional when)
2. Engine's evaluator checks `rule.Redact.Secrets`
3. If true: calls `detector.Redact(fieldValue)` → redacted string + findings
4. Engine constructs `[]redact.Mutation` from findings (original=finding.Match, replaced=`[REDACTED:<RuleID>]`, field=target)
5. Applies mutations to params via `redact.ApplyMutations`
6. Then runs any custom `patterns:` via `redact.Apply()` on the already-redacted params
7. All mutations accumulate across rules as before
8. Decision becomes `Redact` if any mutations were produced

## Data flow: CEL hasSecrets()

1. `hasSecrets(params.text)` in a `when:` clause
2. Calls `detector.Detect(text)`
3. Returns `len(findings) > 0`
4. No mutations — just a boolean for deny/log routing

## Testing

### `internal/secrets`
- `TestDetect_AWSKey` — detects `AKIA[0-9A-Z]{16}`
- `TestDetect_GitHubPAT` — detects `ghp_[A-Za-z0-9]{36}`
- `TestDetect_StripeKey` — detects `sk_live_...`
- `TestDetect_NoMatch` — returns nil for clean text
- `TestDetect_Multiple` — multiple secrets in one string
- `TestRedact_ReplacesWithRuleID` — replacement format is `[REDACTED:<RuleID>]`
- `TestRedact_ReturnsFindings` — findings have correct RuleID/Match values
- `TestNilDetector` — nil receiver is a safe no-op

### `internal/redact`
- `TestApply_SecretsPlusPatterns` — secrets and custom patterns compose (secrets first)

### `internal/cel`
- `TestHasSecrets_True` — returns true when text contains a secret
- `TestHasSecrets_False` — returns false for clean text

### `internal/engine`
- Integration test: rule with `secrets: true` produces `Redact` decision with mutations
- Integration test: rule with `hasSecrets()` in `when:` produces correct match/no-match

### `internal/config`
- Validation test: `secrets: true` with no patterns is valid
- Validation test: `secrets: false` with no patterns is invalid (existing behavior)

### Demo update
- Replace hand-rolled regex patterns in `examples/llm-gateway-demo/rules/demo.yaml` with `secrets: true`

## Dependencies

- `github.com/zricethezav/gitleaks/v8` (MIT license)
- Only imported by `internal/secrets`

## Out of scope

- Custom gitleaks config/rules (use default ~160 patterns; custom patterns stay in YAML)
- Entropy-only detection without regex (gitleaks handles this internally)
- Verification of detected secrets (calling APIs to check if keys are live)
