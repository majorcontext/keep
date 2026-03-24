---
title: "Limitations"
navTitle: "Limitations"
description: "What Keep does not protect against and where other controls are needed."
keywords: ["keep", "limitations", "threat model", "defense in depth"]
---

# Limitations

Keep enforces policy on structured API calls. It evaluates the operation name and parameters that the integration layer presents to it. This is a useful boundary, but it is not the only boundary that matters. Understanding what Keep does not cover is as important as understanding what it does.

## Keep sees names, not behavior

Keep matches rules against the operation name and parameters of a call. It has no model of what the operation actually does when it reaches the upstream service.

A rule that blocks `rm -rf` in a bash command:

```yaml
- name: block-rm-rf
  match:
    operation: "llm.tool_use"
    when: "params.name == 'bash' && params.input.command.contains('rm -rf')"
  action: deny
  message: "Destructive command blocked."
```

This denies calls where the command string contains `rm -rf`. It does not deny:

- A script called `cleanup.sh` that runs `rm -rf /` internally
- A Python one-liner: `import shutil; shutil.rmtree('/')`
- An alias: `alias purge='rm -rf'` followed by `purge /`
- A base64-encoded command: `echo cm0gLXJmIC8= | base64 -d | sh`

Keep evaluates the parameters it receives. It does not execute, decompile, or trace the code those parameters reference. If the dangerous behavior is behind an indirection that isn't visible in the call's params, Keep cannot see it.

This applies equally to MCP tool calls. A rule blocking `delete_issue` does not block a tool called `bulk_cleanup` that deletes issues as a side effect.

## Keep does not see what happens after forwarding

When Keep allows or redacts a call and forwards it to an upstream service, it has no visibility into what that service does with the request. If an MCP server has a tool called `run_query` and the agent passes a `DROP TABLE` statement as a parameter, Keep can match on that parameter and deny it. But if the MCP server internally executes additional queries beyond what the agent requested, Keep does not see those.

Similarly, the LLM gateway filters what goes to and from the model, but it does not control what the model does with the information it receives. If a secret is redacted before reaching the model, the model never sees it. But if a secret reaches the model through a path that doesn't go through Keep, the redaction provides no protection.

## Keep does not enforce at the process level

Keep operates at the API layer. It does not control what processes an agent spawns, what files it reads, what network connections it makes, or what system calls it issues. These are enforcement points for other tools:

- **Network-level controls** (firewalls, Moat) restrict which hosts and ports a process can reach
- **Process-level controls** (seccomp, AppArmor, agentsh) restrict what syscalls a process can make
- **File-level controls** (permissions, chroot, containers) restrict what a process can read and write

An agent that calls `curl` directly from a shell bypasses the MCP relay entirely. An agent that reads `/etc/shadow` and stuffs it into an API call's parameters can be caught by Keep, but an agent that reads the file and writes it to a local socket cannot.

## String matching has inherent limits

Rules that match on string content -- bash commands, message text, code snippets -- are limited by what pattern matching can express:

- **Encoding** -- content can be base64-encoded, URL-encoded, hex-encoded, or compressed
- **Obfuscation** -- variable interpolation, string concatenation, or unicode substitution can defeat keyword matching
- **Synonyms** -- `containsAny(text, ['layoff', 'RIF'])` does not catch "workforce reduction" or "headcount adjustment"
- **Context** -- "I'm writing a blog post about the word 'acquisition'" is not a sensitive disclosure, but a keyword rule cannot tell the difference

Keep's expression language (CEL) is deliberately bounded -- no loops, no recursion, no external calls. This makes evaluation fast and deterministic, but it also means Keep cannot perform deep content analysis. For cases where string matching is insufficient, the audit log provides the data for human review.

## Rate limits are per-instance

`rateCount()` uses an in-memory counter store local to each engine instance. If you run multiple relay or gateway instances behind a load balancer, each maintains independent counters. An agent that distributes calls across instances sees a higher effective rate limit than any single instance enforces.

For deployments where distributed rate limiting matters, use an external rate limiter (API gateway, reverse proxy) in addition to Keep's per-instance counters.

## Audit-only mode still increments counters

In `audit_only` mode, rules are evaluated and logged but not enforced. However, `rateCount()` counters still increment during evaluation. When you switch a scope from `audit_only` to `enforce`, the counters reflect all calls -- not just the ones that would have been allowed. This means rate limits may trigger immediately after switching to enforce mode if traffic was high during the observation period.

## What to pair with Keep

Keep is one layer in a defense-in-depth strategy. It is most effective when combined with controls at other layers:

| Layer | Tool | What it controls |
|-------|------|-----------------|
| Network | Firewall, Moat | Which hosts and ports a process can reach |
| Process | seccomp, AppArmor, agentsh | Which syscalls a process can make |
| API | Keep | Which operations and parameters are permitted |
| Token | OAuth scopes, API key permissions | Which APIs a credential can access |

Keep narrows what an agent can do within the access its credentials already grant. It does not replace token scoping, network isolation, or process sandboxing.
