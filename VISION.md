# Vision

## Principles

### API-Level Policy

Keep operates at the API layer, not the transport layer. It sees operations and their parameters -- "create a P0 issue in the HR team" -- not hosts, ports, or TCP connections. This makes policy portable: the same engine and the same rules work in an MCP relay, an LLM provider gateway, or directly in agent application code.

### Deny, Audit, Tune

Agents that hit a policy boundary are denied immediately with structured feedback. No human-in-the-loop approval queues. The agent can work around the problem, retry with different parameters, or fail -- just like hitting any other API error.

The tuning loop is asynchronous: audit logs capture every evaluation, operators review, policies evolve. Deploy in `audit_only` mode first, watch what would be blocked, switch to `enforce` when confident.

### Safe Defaults

New scopes default to `audit_only` -- observe before enforce. Rules are explicit: if no rule matches, the call proceeds. Deny rules short-circuit. Every evaluation produces an audit entry.

- **Policy is declarative.** YAML rule files. No code changes to support new APIs -- write a profile.
- **Evaluation is deterministic.** CEL expressions have no side effects, terminate in bounded time, and produce the same result for the same input. The exception is `rateCount()`, which maintains local counters.
- **The engine is stateless.** No network calls, no database queries during evaluation. The integration layer handles transport.

### Composition

Keep is the policy engine. It doesn't own transport, credentials, or sandboxing.

- **Moat** handles network-level isolation and credential injection.
- **Keep** handles operation-level policy on structured calls.
- **agentsh** (or similar) handles syscall-level enforcement.

Each layer has a clear boundary. Keep composes with Moat as a sidecar, but runs independently too.

### Progressive Enhancement

Start with observation:

```yaml
scope: linear-tools
mode: audit_only
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."
```

Review audit logs. Switch to `enforce` when ready:

```yaml
mode: enforce
```

Add profiles for readability. Add starter packs for common APIs. Add the LLM gateway when you need bidirectional filtering. Each layer is optional.

## Non-Goals

Keep does not:

- **Manage transport.** No HTTP servers, no TLS termination, no credential injection. The convenience binaries (`keep-mcp-relay`, `keep-llm-gateway`) handle transport. The engine is a library.
- **Replace API token scopes.** Keep narrows what agents can do within the access tokens already grant. It doesn't issue tokens or manage authentication.
- **Moderate model output.** Keep can filter what the model sees (request direction) and what the model wants to do (response direction -- tool calls). It does not evaluate the model's natural language responses for safety or appropriateness.
- **Orchestrate agents.** No scheduling, routing, or multi-agent coordination. One call in, one decision out.
- **Store state across evaluations.** Each evaluation is independent. The exception is `rateCount()`, which maintains local counters for sliding window rate limits.

These concerns belong elsewhere -- in the agent framework, the orchestration layer, or the model provider's own safety systems.
