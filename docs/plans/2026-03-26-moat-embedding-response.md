# Response to Keep Embedding API Spec

**From:** Keep maintainers
**Date:** 2026-03-26

## What we shipped

Five features in the public `github.com/majorcontext/keep` package for embedding:

### 1. `LoadFromBytes(data []byte, opts ...Option) (*Engine, error)`

Constructs an Engine from raw YAML bytes. No filesystem access.

```go
eng, err := keep.LoadFromBytes([]byte(`
scope: mcp-tools
mode: enforce
rules:
  - name: no-delete
    match:
      operation: "delete_*"
    action: deny
    message: "Destructive operations are blocked."
`))
```

Pack references are rejected — all rules must be inline.

### 2. `ValidateRuleBytes(data []byte) error`

Validates a Keep rule file without compiling an engine. Checks YAML structure, field validation, and CEL expression compilation. Use this at `moat run` time to fail on invalid rules before the container starts:

```go
if err := keep.ValidateRuleBytes(policyYAML); err != nil {
    // Fail at moat run time, not container startup
    return fmt.Errorf("invalid policy: %w", err)
}
```

### 3. `RuleSet` builder

Programmatic rule construction without touching YAML:

```go
rs := keep.NewRuleSet("mcp-tools", "enforce")
rs.Allow("Read", "Glob", "Grep")
rs.Deny("Bash", "Edit", "Write")
eng, err := rs.Compile()
```

Semantics:
- **Deny only:** listed operations are blocked, everything else is allowed
- **Allow only:** listed operations are allowed, everything else is denied
- **Both:** deny takes precedence for overlapping entries; unlisted operations are denied
- **Empty:** no rules, everything is allowed

`Compile()` accepts the same options as `LoadFromBytes`: `WithMode`, `WithAuditHook`.

### 4. `WithAuditHook(func(AuditEntry)) Option`

Callback invoked synchronously after every successful `Evaluate`. Route audit events into Moat's telemetry:

```go
eng, err := keep.LoadFromBytes(ruleYAML,
    keep.WithAuditHook(func(entry keep.AuditEntry) {
        telemetry.EmitPolicyEvent(entry)
    }),
)
```

Not called when `Evaluate` returns an error. Keep it fast — runs synchronously.

### 5. `WithMode(mode string) Option`

Overrides the mode for all scopes. `"enforce"` or `"audit_only"`.

---

## Integration patterns

### Pattern 1: RuleSet builder (recommended for simple policies)

```go
rs := keep.NewRuleSet("mcp-tools", "enforce")
rs.Allow("list_issues", "get_issue", "search_issues")
rs.Deny("delete_issue", "close_issue")

eng, err := rs.Compile(
    keep.WithAuditHook(func(e keep.AuditEntry) {
        log.Info("policy", "op", e.Operation, "decision", e.Decision)
    }),
)
if err != nil {
    return err
}
defer eng.Close()
```

### Pattern 2: YAML rules (for complex policies with CEL, redaction, etc.)

```go
eng, err := keep.LoadFromBytes(policyYAML, keep.WithMode("enforce"))
```

### Pattern 3: Validation at deploy time

```go
// In moat run, before starting the container:
if err := keep.ValidateRuleBytes(policyYAML); err != nil {
    fmt.Fprintf(os.Stderr, "invalid policy rules: %v\n", err)
    os.Exit(1)
}
```

### Per-request evaluation

```go
result, err := eng.Evaluate(keep.Call{
    Operation: toolName,
    Params:    toolParams,
    Context: keep.CallContext{
        Timestamp: time.Now(),
        Scope:     "mcp-tools",
    },
}, "mcp-tools")

switch result.Decision {
case keep.Deny:
    // Return error to agent
case keep.Redact:
    toolParams = keep.ApplyMutations(toolParams, result.Mutations)
    // Forward with redacted params
case keep.Allow:
    // Forward as-is
}
```

### For HTTP requests

Keep is protocol-agnostic. Normalize HTTP requests into `Call` at your integration layer:

```go
result, err := eng.Evaluate(keep.Call{
    Operation: req.Method + " " + req.URL.Path,
    Params: map[string]any{
        "method": req.Method,
        "host":   req.Host,
        "path":   req.URL.Path,
    },
}, scopeName)
```

---

## What we didn't do

### No `WithAllowlist` on LoadFromBytes

The spec proposed `keep.LoadFromBytes(yamlBytes, keep.WithAllowlist("Read", "Glob"))`. This conflates two configuration sources — YAML rules and programmatic overrides — in one call. Use the `RuleSet` builder for programmatic policies, `LoadFromBytes` for YAML policies. Don't mix them.

### No `pkg/engine` package

Keep's public API lives at the root package. Import `github.com/majorcontext/keep`.

### No protocol-specific Request type

Keep sees `Call{Operation, Params, Context}`. It doesn't know HTTP from MCP. Moat normalizes at its integration layer.

---

## What we added beyond the spec

### `context.operation` in CEL

The call's operation name is now available in CEL `when` clauses as `context.operation`. This was needed internally for the RuleSet builder's allowlist semantics, but it's useful for hand-written rules too:

```yaml
rules:
  - name: audit-mutations
    match:
      operation: "*"
      when: "context.operation.startsWith('update_') || context.operation.startsWith('create_')"
    action: log
```

---

## Types reference

```go
// Construction
func LoadFromBytes(data []byte, opts ...Option) (*Engine, error)
func ValidateRuleBytes(data []byte) error
func NewRuleSet(scope, mode string) *RuleSet
func (rs *RuleSet) Allow(ops ...string)
func (rs *RuleSet) Deny(ops ...string)
func (rs *RuleSet) Compile(opts ...Option) (*Engine, error)

// Options
func WithMode(mode string) Option
func WithAuditHook(hook func(AuditEntry)) Option

// Evaluation
func (e *Engine) Evaluate(call Call, scope string) (EvalResult, error)
func (e *Engine) Scopes() []string
func (e *Engine) Close()
func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any
```

## Concurrency and safety

- `Engine.Evaluate` is goroutine-safe
- `Evaluate` performs no file I/O, network calls, or global state mutation
- `Evaluate` does not panic for well-formed `Call` inputs
- Engines from `LoadFromBytes` and `RuleSet.Compile` are immutable (no `Reload`)
- `rateCount()` counters (if used in CEL rules) are mutex-protected

## Go module

```
require github.com/majorcontext/keep v0.2.1
```

Import graph: CEL (`cel-go`), YAML (`gopkg.in/yaml.v3`), gitleaks patterns. Does NOT pull in relay, gateway, CLI, or HTTP server code.
