---
title: "Scopes and organization"
navTitle: "Scopes"
description: "How Keep organizes rules into scopes, with profiles for aliasing and starter packs for reuse."
keywords: ["keep", "scopes", "profiles", "starter packs", "organization"]
---

# Scopes and organization

Keep organizes rules into **scopes**. A scope is a named collection of rules bound to a class of traffic -- one rule file, one scope, one policy boundary. Scopes are the unit that deployment modes (relay, gateway, library) bind to routes, providers, or direct `Evaluate()` calls.

## Scopes

Each rule file declares exactly one scope at the top level:

```yaml
scope: linear-tools
mode: enforce
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
```

The scope name identifies this set of rules. When the engine evaluates a call, it looks up the scope by name and runs the matching rules. If the scope does not exist, evaluation returns an error listing the available scopes.

### One scope per file, no duplicates

The loader reads all YAML files from the rules directory and indexes them by scope name. If two files declare the same scope, loading fails with an error identifying both files. This constraint prevents rules from scattering across files in ways that are hard to audit -- every rule that applies to a scope lives in a single file.

### Scopes and deployment modes

Deployment modes bind scopes to traffic, but the binding mechanism differs:

- The **MCP relay** maps each route to a scope. A route for `linear-tools` evaluates calls against the `linear-tools` scope before forwarding to the upstream MCP server.
- The **LLM gateway** binds a single scope to the provider connection. All calls through the gateway evaluate against that scope.
- The **Go library** passes the scope name directly to `engine.Evaluate(call, "linear-tools")`.

The scope is the seam between policy and transport. Rule files define policy. Deployment configs define where traffic comes from. Scopes connect the two.

## Transport independence

Rule files contain only policy -- no URLs, no listen addresses, no routing. A rule file that works in the relay also works in the gateway or as a library. This separation is deliberate.

Consider a scope `linear-tools` with rules that deny issue deletion and block P0 creation. Those rules make sense regardless of whether the call arrives via an MCP relay, an LLM gateway that decomposes tool-use blocks, or a Go application that constructs calls directly. The policy is about what the agent is allowed to do, not how the call reaches the engine.

Transport details live in deployment configs (`keep-mcp-relay.yaml`, `keep-llm-gateway.yaml`). Rule files never reference them. This means you can develop and test rules locally with `keep validate`, then deploy the same files to any mode without modification.

## Profiles

A profile is a YAML file that maps short alias names to parameter field paths. Profiles make rules more readable and portable across APIs with different field structures.

```yaml
name: linear
aliases:
  priority: "params.priority"
  status: "params.status"
  assignee: "params.assignee_id"
```

A rule file references a profile by name:

```yaml
scope: linear-tools
profile: linear
rules:
  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "priority == 0"
    action: deny
```

Without the profile, that `when` expression would be `params.priority == 0`. With the profile, `priority` resolves to `params.priority` during compilation. The compiled expression is identical either way -- profiles are syntactic sugar applied at load time.

### Why profiles exist

Different APIs represent the same concept with different field paths. A priority field might be `params.priority` in one API and `params.fields.priority.id` in another. Profiles let you write rules using short, meaningful names and swap the underlying paths by changing the profile. If an API restructures its fields, update the profile and the rules continue to work.

Alias names follow strict rules: lowercase letters, digits, and underscores (`[a-z][a-z0-9_]*`), at most 32 characters. Alias names that collide with CEL built-ins or Keep-specific functions are rejected to prevent shadowing. Alias targets must start with `params.` -- they map into the call's parameter namespace, not into arbitrary data.

## Starter packs

A starter pack is a reusable set of rules that a rule file can import. Packs provide sensible defaults for common APIs. Rule files reference packs and optionally override individual rules.

A pack file:

```yaml
name: linear-defaults
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 issues require human triage."
```

A rule file that uses the pack:

```yaml
scope: linear-tools
mode: enforce
packs:
  - name: linear-defaults
    overrides:
      no-auto-p0:
        action: log
        message: "P0 creation logged for review."
rules:
  - name: require-label
    match:
      operation: "create_issue"
      when: "!has(params.label_ids)"
    action: deny
```

### How resolution works

The loader merges pack rules and inline rules into a single list. Pack rules come first, in the order packs are listed. Inline rules follow. When overrides are present, they modify the pack's rules before merging:

- A map override replaces specific fields on a rule. Overridable fields are `when`, `message`, and `action`. Fields like `name` and `operation` are not overridable -- they define the rule's identity.
- The string `"disabled"` removes the rule entirely from the merged list.

After resolution, the engine compiles and evaluates the merged list as if all rules were written inline. There is no runtime distinction between pack rules and inline rules.

### When to use packs

Packs are useful when multiple deployments share a common policy baseline. A team can publish a pack with organization-wide defaults, and individual deployments extend or relax rules as needed. Disabling a rule is explicit (`"disabled"`) rather than implicit, so auditors can see what was changed and why.

## How the pieces fit together

A typical deployment uses all three concepts:

- **Profiles** in `./profiles/` define aliases for each API's field structure
- **Starter packs** in `./packs/` define reusable rule baselines
- **Rule files** in `./rules/` declare scopes, reference profiles and packs, and add deployment-specific rules

The loader reads all three directories, validates cross-references (every referenced profile and pack must exist), resolves packs into merged rule lists, and compiles CEL expressions with profile aliases applied. The result is a set of evaluators indexed by scope name, ready for any deployment mode.
