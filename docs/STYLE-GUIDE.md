# Documentation Style Guide

This guide establishes the voice, tone, and conventions for Keep documentation. Follow these guidelines to ensure consistency across all pages.

## Voice and Tone

### Be Objective
State facts. Avoid hyperbole, marketing language, and subjective claims.

| Avoid | Prefer |
|-------|--------|
| "Keep makes policy management incredibly easy" | "Keep evaluates structured calls against declarative rules" |
| "The blazingly fast engine" | "Rule evaluation adds <10ms p99 latency" |
| "Finally, a solution that actually works" | (Just describe what it does) |
| "Unlike other tools that get this wrong..." | (Describe Keep's approach without comparison) |

Don't use words like: revolutionary, game-changing, seamless, effortless, simple (as a claim), easy (as a claim), powerful, robust, elegant, beautiful, magic/magical.

### Be Respectful
Acknowledge that other tools exist and serve their purposes. Avoid dismissive comparisons.

| Avoid | Prefer |
|-------|--------|
| "Prompt-based guardrails are useless" | "Keep enforces policy externally, independent of prompt instructions" |
| "Unlike traditional approaches that trust the agent..." | "Rules are evaluated before calls reach upstream services" |
| "It's not just another proxy" | "Keep's policy engine is transport-agnostic -- it works as a library, MCP relay, or LLM gateway" |
| "Stop trusting your agents with broad API tokens" | "Rules narrow what agents can do within existing token scopes" |

When comparing approaches, describe what Keep does and let readers draw their own conclusions. Don't tell them what's wrong with their current workflow.

### Be Factual
Make specific, verifiable claims. Avoid generalizations and euphemisms.

| Avoid | Prefer |
|-------|--------|
| "Your agents are safe" | "Deny rules short-circuit evaluation and return structured errors to the agent" |
| "Full visibility into what happened" | "Audit logs capture timestamp, scope, operation, agent identity, rules evaluated, and decision" |
| "Smart policy enforcement" | "CEL expressions evaluate in bounded time with no side effects" |
| "Enterprise-grade security" | (Describe the specific security properties) |

If you can't point to a specific mechanism, the claim is too vague.

### Be Direct
Write in active voice. State what things do, not what they "can" or "may" do.

| Avoid | Prefer |
|-------|--------|
| "You can use `audit_only` mode to observe policy" | "`audit_only` mode evaluates and logs rules without enforcing them" |
| "Keep may automatically reload rule files" | "Keep reloads rule files on change" |
| "It is possible to evaluate rules as a library" | "Import the engine and call `Evaluate()` directly" |

### Be Concise
Eliminate filler words. Every sentence should convey information.

| Avoid | Prefer |
|-------|--------|
| "In order to start a run, you need to..." | "To start a run..." |
| "It's important to note that tokens are never..." | "Tokens are never..." |
| "Basically, what happens is that the proxy..." | "The proxy..." |

### Be Precise
Use specific terms consistently. Avoid synonyms that create ambiguity.

| Term | Definition | Don't use |
|------|------------|-----------|
| **call** | A structured object (operation + params + context) evaluated by the engine | request, event, action |
| **scope** | A named collection of rules bound to a class of traffic | namespace, group, policy set |
| **rule** | An atomic unit of policy: match condition + action | filter, check, constraint |
| **evaluate** | Check a call against rules and return a decision | validate, check, process |
| **deny** | Block a call and return an error to the caller | reject, fail, block |
| **redact** | Mutate specific fields before forwarding | mask, strip, sanitize |
| **profile** | A YAML file mapping short aliases to field paths | schema, mapping, template |
| **starter pack** | A reusable, overridable set of rules for a common API | preset, template, default rules |

### Be Practical
Lead with what users need to do, not theory. Show working examples first, explain after.

```markdown
<!-- Avoid: Theory first -->
Keep uses CEL expressions to evaluate policy. The engine compiles
expressions at load time and evaluates them against a normalized
call object. To use this feature:

<!-- Prefer: Action first -->
Block P0 issue creation:

    - name: no-auto-p0
      match:
        operation: "create_issue"
        when: "params.priority == 0"
      action: deny

The rule fires when priority is 0. The agent receives a structured
denial with the rule name and message.
```

### Be Honest About Limitations
Document what Keep doesn't do, edge cases, and known issues. Users trust documentation that acknowledges limitations.

```markdown
<!-- Good: Acknowledges limitation -->
> **Note:** `containsPHI()` uses regex patterns, not medical NLP.
> It catches common formats (MRN, DOB labels) but not contextual PHI.

<!-- Good: States trade-off -->
`rateCount()` uses a local counter store. Counters are not shared
across relay or gateway instances. If you run multiple instances,
each maintains independent counts.
```

## Formatting Conventions

### Headings
- Use sentence case: "Getting started" not "Getting Started"
- Keep headings short (under 6 words when possible)
- Don't skip levels (h2 → h4)

### Code Blocks
Always specify the language for syntax highlighting:

````markdown
```bash
keep validate ./rules
```

```yaml
scope: linear-tools
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
```

```go
func main() {
    // ...
}
```
````

### Command Examples
Show the command, then the output. Use `$` prefix for commands:

```markdown
    $ keep validate ./rules

    rules/linear.yaml: OK (6 rules, scope: linear-tools)
    rules/anthropic.yaml: OK (8 rules, scope: anthropic-gateway)

    2 files validated, 0 errors
```

For commands without meaningful output, omit the output section.

### Inline Code
Use backticks for:
- Commands: `keep validate`
- Flags: `--config`
- File names: `keep-mcp-relay.yaml`
- Field paths: `params.priority`
- Values: `true`, `enforce`, `audit_only`
- CEL expressions: `params.priority == 0`

Don't use backticks for:
- Product names: Keep, Moat, Docker, GitHub
- General concepts: policy evaluation, audit logging
- Actions when used as concepts: deny, redact, log

### File Paths
- Use relative paths when referring to project files: `./rules/linear.yaml`
- Use `./rules/` for the rules directory convention
- Use absolute paths only when necessary for system paths

### Lists
Use bullet lists for unordered items. Use numbered lists only for sequential steps.

```markdown
<!-- Unordered: features, options, notes -->
Supported actions:
- `deny` -- block the call
- `log` -- allow but record
- `redact` -- allow but mutate fields

<!-- Ordered: steps that must be followed in sequence -->
1. Write rule files in `./rules/`
2. Validate with `keep validate ./rules`
3. Start the relay with `keep-mcp-relay --config keep-mcp-relay.yaml`
```

### Tables
Use tables for structured comparisons. Keep cells concise.

```markdown
| Action | Stops call | Mutates params | Audit logged |
|--------|-----------|----------------|--------------|
| `deny` | Yes | No | Yes |
| `log` | No | No | Yes |
| `redact` | No | Yes | Yes |
```

### Admonitions
Use blockquotes with bold labels for callouts:

```markdown
> **Note:** Additional context that's helpful but not critical.

> **Warning:** Something that could cause problems if ignored.

> **Tip:** A useful shortcut or best practice.
```

## Content Guidelines

### Show Real Output
When documenting commands, use realistic output that matches what users will see. Test commands before documenting them.

### Explain the "Why"
Don't just show what to do—briefly explain why it matters:

```markdown
<!-- Just what -->
Set `mode: audit_only` to observe policy without enforcing it.

<!-- What + why -->
Set `mode: audit_only` to observe policy without enforcing it. This
lets you see what would be blocked in production before turning
enforcement on—review audit logs, tune rules, then switch to `enforce`.
```

### Link to Related Content
Cross-reference related pages. Use relative links:

```markdown
See [Expression Language](../concepts/02-expressions.md) for details
on how CEL expressions are evaluated.
```

### Use Generic Examples
Use placeholder names that don't imply specific technologies or products:

| Avoid | Prefer |
|-------|--------|
| `acme-corp/billing-service` | `my-org/my-project` |
| `openai-agent` | `my-agent` |
| `claude-assistant` | `my-assistant` |

Use `my-agent` for agent identity values (in `context.agent_id`). Use `my-scope` for scope name examples. Use real API names (Linear, Slack, Gmail) when showing realistic rule examples -- the point of Keep is API-level policy, so generic examples often obscure the value.

### Error Messages
When documenting errors, show the full error message and explain how to resolve it:

```markdown
If you see:

    Error: scope "linear-tools" not found in rules directory

No rule file declares this scope. Check that your rule files are in
the configured `rules_dir` and that the scope name matches:

    keep validate ./rules
```

## Section Definitions

The documentation has four sections. Each serves a distinct purpose. Content that doesn't fit a section's purpose belongs elsewhere.

### Getting Started

**Purpose:** Onboard new users from install to first successful run.

**Audience:** Someone who has never used Keep.

**Contains:** Installation instructions, a guided walkthrough, and orientation material. Pages are sequential -- each builds on the previous one.

**Does not contain:** Deep explanations, exhaustive configuration options, or advanced workflows.

### Concepts

**Purpose:** Explain *how things work* and *why they are designed that way*. Build mental models.

**Audience:** Someone who wants to understand the system, not accomplish a specific task.

**Contains:** Architecture, design decisions, trade-offs, threat models, data flow diagrams. Describes mechanisms and explains rationale. May include brief examples to illustrate a point, but examples serve understanding, not task completion.

**Does not contain:** Step-by-step instructions, command output examples, configuration syntax tables, or troubleshooting steps. If a reader needs to *do* something, that content belongs in a guide. If a reader needs to *look up* syntax or options, that belongs in reference.

**Test:** If you removed all code blocks and the page still makes sense, it's a concept page.

### Guides

**Purpose:** Help users accomplish specific tasks. Answer "how do I do X?"

**Audience:** Someone who has a goal and needs steps to reach it.

**Contains:** Prerequisites, step-by-step instructions, working examples with expected output, verification steps, and troubleshooting. May include brief "how it works" context (3-5 sentences) to orient the reader, but the bulk of the page is procedural.

**Does not contain:** Deep architectural explanations, design rationale, or exhaustive option tables. Link to concept pages for "why" and reference pages for "all options."

**Test:** The page should read as a recipe. A reader should be able to follow it start-to-finish and achieve a result.

### Reference

**Purpose:** Provide complete, structured specifications. Answer "what are all the options?"

**Audience:** Someone who knows what they want to do and needs exact syntax, flags, fields, or values.

**Contains:** CLI commands with all flags, configuration file schemas with all fields, environment variable tables, format specifications. Organized for lookup, not reading. Every option documented with type, default, and description.

**Does not contain:** Extended explanations of why things work the way they do, or guided workflows. Brief notes clarifying behavior are fine; multi-paragraph explanations belong in concepts.

**Test:** The page should work as a lookup table. A reader should be able to find any option in under 10 seconds.

## Page Structure

### Getting started pages
1. Brief intro (1-2 sentences)
2. What you'll accomplish
3. Prerequisites (if any)
4. Step-by-step instructions
5. Next steps / related pages

### Concept pages
1. What it is (1-2 paragraphs)
2. Why it matters
3. How it works (with diagrams if helpful)
4. Key details / edge cases
5. Related concepts

### Guide pages
1. What you'll accomplish
2. Prerequisites
3. Step-by-step walkthrough
4. Verification / testing
5. Troubleshooting common issues
6. Related guides

### Reference pages
1. Brief description
2. Complete specification
3. Examples for each option
4. Notes and caveats

## Terminology

### Capitalize
- Keep (the product)
- Moat (when referencing the sandbox)
- CEL (Common Expression Language)
- GitHub, GitLab, Linear, Slack
- macOS, Linux, Windows

### Don't Capitalize
- scope, rule, profile, starter pack
- call, operation, params, context
- deny, redact, log (as actions)
- audit log, evaluation

### Abbreviations
Spell out on first use, then use abbreviation:

- CEL (Common Expression Language)
- MCP (Model Context Protocol)
- LLM (large language model)
- CLI (command-line interface)
- API (application programming interface)
- PII (personally identifiable information)
- PHI (protected health information)

Common abbreviations that don't need expansion:
- URL, HTTP, HTTPS
- JSON, YAML
- ID (identifier)
- RE2 (regex syntax)

## Frontmatter Template

Every documentation page should start with this frontmatter:

```yaml
---
title: "Page Title"
description: "One sentence description for SEO and link previews."
keywords: ["keep", "relevant", "keywords"]
---
```

Optional fields:
```yaml
draft: true  # Exclude from production builds
```

The following are inferred from the file path and don't need to be specified:
- `slug` — From filename (e.g., `01-introduction.md` → `introduction`)
- `section` — From parent directory
- `order` — From numeric prefix
- `prev`/`next` — From adjacent files
