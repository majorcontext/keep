# Keep Core Interface Sketch

Working document. Sketching the core policy engine interface -- what a call looks like, how rules match, what comes back. Transport-agnostic. The integration layer (MCP relay, LLM gateway, etc.) normalizes protocol-specific traffic into this shape before Keep sees it.

---

## The Call Object

Every call Keep evaluates has this shape:

```
Call {
  operation   string            // what is being done
  params      map[string]any    // structured parameters
  context     Context           // metadata about who/when/where
}

Context {
  agent_id    string            // which agent is making the call
  user_id     string            // which human delegated access (if any)
  timestamp   time              // when the call was made
  scope       string            // which scope matched this call
  direction   string            // "request" | "response" | null
  labels      map[string]string // arbitrary key-value tags
}
```

The integration layer populates `operation` and `params` from whatever protocol it speaks. The policy engine never sees HTTP methods, URL paths, gRPC service names, or MCP framing. It just sees this object.

`direction` is context metadata, not a parameter. It tells the engine whether the call represents something going to a service (request) or something coming back from a service (response). Most integrations are request-only. The LLM gateway is bidirectional.

---

## How Integrations Populate the Call

### MCP integration

One MCP tool call = one Keep call. Clean one-to-one mapping.

**Linear MCP -- create_issue:**

```json
{
  "operation": "create_issue",
  "params": {
    "title": "Fix auth token refresh",
    "teamId": "TEAM-ENG",
    "assigneeId": "user-dan",
    "priority": 1,
    "labels": ["bug", "auth"],
    "description": "Token refresh fails silently after 24h..."
  },
  "context": {
    "agent_id": "claude-code-session-abc",
    "timestamp": "2026-03-16T14:30:00Z",
    "scope": "linear-tools",
    "direction": "request"
  }
}
```

**Linear MCP -- search_issues:**

```json
{
  "operation": "search_issues",
  "params": {
    "query": "auth bug",
    "teamId": "TEAM-ENG",
    "status": "In Progress",
    "first": 20
  },
  "context": {
    "agent_id": "claude-code-session-abc",
    "scope": "linear-tools",
    "direction": "request"
  }
}
```

### Library use case: custom email agent

Keep doesn't have to sit in a proxy. An agent application can import the Keep engine as a library and evaluate policy inline before making API calls.

Here, a custom email agent built on the Gmail API constructs the call object itself and checks Keep before sending:

```python
# Inside the agent's send_email function
call = {
    "operation": "send_email",
    "params": {
        "to": ["investor@bigfund.com"],
        "cc": [],
        "subject": "Q1 Board Deck - Draft",
        "body": "Hi, please find attached the latest draft..."
    },
    "context": {
        "agent_id": "email-assistant-v2",
        "user_id": "dan@thegp.com",
        "timestamp": "2026-03-16T02:15:00Z",
        "scope": "gmail-agent",
        "direction": "request"
    }
}

result = keep.evaluate(call)

if result.decision == "deny":
    # Return the denial to the agent loop so it can adapt
    return AgentError(result.message)

if result.decision == "redact":
    # Apply mutations to the params before sending
    call.params = keep.apply_mutations(call.params, result.mutations)

# Policy passed -- make the actual Gmail API call
gmail.users().messages().send(userId="me", body=build_mime(call.params)).execute()
```

The call object looks identical to an MCP tool call. The difference is who constructs it -- in the MCP relay, the integration layer normalizes the MCP protocol. Here, the agent code builds the call directly. The engine doesn't know the difference. Same rules, same evaluation, same result object.

This pattern works for any agent that calls APIs directly: a data pipeline agent calling Snowflake, a DevOps agent calling Terraform Cloud, a research agent calling PubMed. The agent author imports Keep's engine, constructs call objects that describe what they're about to do, and checks policy before doing it.

### LLM gateway integration

This is where normalization matters most. A single Anthropic API request contains a messages array with mixed content block types -- text, tool_use, tool_result, images. Forcing the policy engine to navigate that structure would make rules complex and fragile.

Instead, the LLM integration **decomposes** each API request into multiple Keep calls -- one per interesting content block, plus one payload-level summary. The engine evaluates them all. Any deny blocks the whole request. Redactions target specific blocks and the integration reassembles the mutated payload.

**Decomposition model:**

For a request to `POST /v1/messages` that contains a system prompt, a user text message, an assistant response with a tool_use, and a user message with two tool_results, the LLM integration emits:

```
1. { operation: "llm.request",     params: { system, model, token_estimate, tool_result_count, ... }}
2. { operation: "llm.tool_result", params: { tool_name, tool_use_id, content }}
3. { operation: "llm.tool_result", params: { tool_name, tool_use_id, content }}
```

For a response from the model that contains a tool_use block:

```
1. { operation: "llm.response",    params: { stop_reason, tool_use_count }}
2. { operation: "llm.tool_use",    params: { name, input }}
```

Each is a flat call. Rules match on flat fields. No reaching into nested arrays.

**Concrete example -- request with tool results containing secrets:**

The agent sent `cat .env` via bash, got back secrets in the tool result, and now the LLM integration is about to forward the context to Anthropic.

Call 1 (payload summary):

```json
{
  "operation": "llm.request",
  "params": {
    "model": "claude-sonnet-4-20250514",
    "system": "You are a helpful coding assistant...",
    "token_estimate": 4200,
    "message_count": 3,
    "tool_result_count": 1
  },
  "context": {
    "agent_id": "claude-code-session-xyz",
    "user_id": "dan",
    "scope": "anthropic-gateway",
    "direction": "request"
  }
}
```

Call 2 (tool_result block):

```json
{
  "operation": "llm.tool_result",
  "params": {
    "tool_name": "bash",
    "tool_use_id": "abc123",
    "content": "DATABASE_URL=postgres://prod:s3cret@db.internal:5432/app\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nAWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI..."
  },
  "context": {
    "agent_id": "claude-code-session-xyz",
    "scope": "anthropic-gateway",
    "direction": "request"
  }
}
```

**Concrete example -- response with a dangerous tool call:**

The model wants to call bash with `rm -rf /data/production`.

Call 1 (payload summary):

```json
{
  "operation": "llm.response",
  "params": {
    "stop_reason": "tool_use",
    "tool_use_count": 1
  },
  "context": {
    "agent_id": "claude-code-session-xyz",
    "scope": "anthropic-gateway",
    "direction": "response"
  }
}
```

Call 2 (tool_use block):

```json
{
  "operation": "llm.tool_use",
  "params": {
    "name": "bash",
    "input": {
      "command": "rm -rf /data/production/old-backups"
    }
  },
  "context": {
    "agent_id": "claude-code-session-xyz",
    "scope": "anthropic-gateway",
    "direction": "response"
  }
}
```

---

## Rules

A rule has a name, a match condition, and an action. Match conditions use the expression language to test the call object. If a rule matches (or has no `when` clause), the action fires.

```yaml
- name: rule-name
  description: "Human-readable explanation"
  match:
    operation: "glob-or-exact"      # filter by operation name
    when: "expression"              # expression evaluated against params/context
  action: deny | log | redact
  message: "Returned to the agent on deny"
```

### Evaluation model

1. Find the scope that matches this call (by integration config)
2. Collect all rules in that scope
3. Filter to rules whose `operation` pattern matches the call's operation
4. For matching rules, evaluate the `when` expression against `{params, context}`
5. If any rule triggers `deny`, the call is blocked
6. If any rule triggers `redact`, params are mutated before forwarding
7. If all rules pass (or only `log`), the call proceeds
8. Every evaluation produces an audit entry

**On state:** evaluating a single rule against a single call is stateless -- no network calls, no database queries. The exception is `rateCount()`, which requires an in-memory or embedded counter store. The engine needs a pluggable rate backend, but it's local, not remote. This is a deliberate tradeoff: rate limiting is too useful to omit, and a local counter is fast enough to stay within the latency budget.

---

## Concrete Examples

### Linear MCP

Scope: `linear-tools`. Agent is using Linear MCP to manage issues.

```yaml
scope: linear-tools
rules:

  # Only allow issue creation in specific teams
  - name: team-allowlist
    match:
      operation: "create_issue"
      when: "!(params.teamId in ['TEAM-ENG', 'TEAM-INFRA'])"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure teams."

  # Don't let the agent assign P0 issues
  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 (urgent) issues must be created by a human. Use priority 1 or lower."

  # Rate limit issue creation
  - name: issue-creation-rate
    match:
      operation: "create_issue"
      when: "rateCount('linear:create:' + context.agent_id, '1h') > 20"
    action: deny
    message: "Rate limit exceeded. Maximum 20 issues per hour."

  # Block issue deletion entirely
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted. Archive instead."

  # Log all project modifications for audit
  - name: audit-project-changes
    match:
      operation: "update_project"
    action: log

  # Block sensitive terms in issue titles/descriptions
  - name: no-sensitive-content
    match:
      operation: "create_issue"
      when: >
        containsAny(params.title, ['acquisition', 'merger', 'RIF', 'layoff'])
        || containsAny(params.description, ['acquisition', 'merger', 'RIF', 'layoff'])
    action: deny
    message: "Issue contains sensitive business terms. Create manually."

  # Prevent changing issue status to "Done" (only humans close issues)
  - name: no-close-issues
    match:
      operation: "update_issue"
      when: "params.stateId == 'done' || params.stateId == 'cancelled'"
    action: deny
    message: "Agents cannot close or cancel issues. A human must verify completion."
```

**Evaluation trace:**

```
Call:
  operation: "create_issue"
  params: { title: "Fix auth bug", teamId: "TEAM-ENG", priority: 1 }
  context: { agent_id: "cc-session-abc", timestamp: "2026-03-16T14:30:00Z" }

Rule evaluations:
  team-allowlist:       SKIP    ("TEAM-ENG" is in allowlist)
  no-auto-p0:           SKIP    (priority is 1, not 0)
  issue-creation-rate:  SKIP    (count is 3, under 20)
  no-sensitive-content: SKIP    (no sensitive terms)

Result: ALLOW
```

```
Call:
  operation: "create_issue"
  params: { title: "RIF planning - eng headcount", teamId: "TEAM-HR", priority: 2 }
  context: { agent_id: "cc-session-abc" }

Rule evaluations:
  team-allowlist:       DENY    ("TEAM-HR" not in allowlist)

Result: DENY
  policy: "team-allowlist"
  message: "This agent may only create issues in Engineering and Infrastructure teams."
```

---

### Gmail agent (library use case)

Scope: `gmail-agent`. A custom email agent evaluates Keep policy as a library call before hitting the Gmail API. The rules are identical to what you'd write for an MCP scope -- the engine doesn't know the difference.

```yaml
scope: gmail-agent
rules:

  # External emails only during business hours
  - name: business-hours-external
    match:
      operation: "send_email"
      when: >
        !params.to.all(addr, addr.endsWith('@ourcompany.com'))
        && !inTimeWindow('09:00', '18:00', 'America/Los_Angeles')
    action: deny
    message: "External emails blocked outside 9am-6pm PT. Internal addresses are always allowed."

  # Never email certain domains
  - name: blocked-domains
    match:
      operation: "send_email"
      when: "params.to.any(addr, addr.endsWith('@competitor.com') || addr.endsWith('@press.org'))"
    action: deny
    message: "Sending to this domain is not permitted."

  # Don't let agent send to more than 10 recipients
  - name: max-recipients
    match:
      operation: "send_email"
      when: "size(params.to) + size(params.cc) > 10"
    action: deny
    message: "Maximum 10 recipients per message."

  # Block PII in email body
  - name: no-pii-in-body
    match:
      operation: "send_email"
      when: "containsPII(params.body)"
    action: deny
    message: "Email body contains patterns matching PII (SSN, credit card). Review and redact."

  # Rate limit outbound email
  - name: send-rate
    match:
      operation: "send_email"
      when: "rateCount('gmail:send:' + context.agent_id, '1h') > 30"
    action: deny
    message: "Rate limit exceeded. Maximum 30 emails per hour."

  # Read operations are always allowed, just logged
  - name: audit-reads
    match:
      operation: "search_emails"
    action: log

  - name: audit-reads-detail
    match:
      operation: "get_email"
    action: log
```

**Evaluation trace:**

```
Call:
  operation: "send_email"
  params: {
    to: ["investor@bigfund.com"],
    subject: "Q1 Board Deck",
    body: "Hi, please find the draft attached..."
  }
  context: {
    agent_id: "email-assistant-v2",
    user_id: "dan@thegp.com",
    timestamp: "2026-03-16T02:15:00Z",  # 2:15 AM Pacific
    scope: "gmail-agent",
    direction: "request"
  }

Rule evaluations:
  business-hours-external:  DENY  (external recipient + outside 9am-6pm)

Result: DENY
  policy: "business-hours-external"
  message: "External emails blocked outside 9am-6pm PT. Internal addresses are always allowed."
```

The agent's code receives this result and can decide: wait until morning, rewrite to send to an internal address, or surface the denial to the human who initiated the task. Keep doesn't care which -- it returned its decision, the agent handles the rest.

---

### Anthropic Messages API

Scope: `anthropic-gateway`. Bidirectional. The LLM integration decomposes each API request/response into per-block calls. Rules are flat.

```yaml
scope: anthropic-gateway
rules:

  # ---- REQUEST: what the model is about to see ----

  # Redact secrets in tool results before they reach the model
  - name: redact-secrets
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "AKIA[0-9A-Z]{16}"
          replace: "[REDACTED:AWS_KEY]"
        - match: "(?i)(password|secret|api_key|token)\\s*[=:]\\s*\\S+"
          replace: "[REDACTED:SECRET]"
        - match: "-----BEGIN (RSA |EC )?PRIVATE KEY-----[\\s\\S]*?-----END \\1PRIVATE KEY-----"
          replace: "[REDACTED:PRIVATE_KEY]"

  # Block if a tool result contains PHI
  - name: block-phi
    match:
      operation: "llm.tool_result"
      when: "containsPHI(params.content)"
    action: deny
    message: "Tool result contains potential PHI. Sanitize the data source."

  # Block tool results from specific tools entirely (e.g., don't let
  # the model see output from a database query tool)
  - name: block-db-results
    match:
      operation: "llm.tool_result"
      when: "params.tool_name == 'sql_query'"
    action: deny
    message: "Database query results are not permitted in model context."

  # Payload-level: block requests with excessive context
  - name: context-size-limit
    match:
      operation: "llm.request"
      when: "params.token_estimate > 150000"
    action: deny
    message: "Context exceeds 150k token limit."

  # ---- RESPONSE: what the model wants to do ----

  # Block destructive bash commands
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: "params.name == 'bash' && params.input.command.matches('rm -rf|DROP TABLE|TRUNCATE|mkfs|dd if=')"
    action: deny
    message: "Destructive command blocked."

  # Block certain tools entirely
  - name: tool-denylist
    match:
      operation: "llm.tool_use"
      when: "params.name in ['curl', 'wget', 'nc', 'ssh']"
    action: deny
    message: "Networking tools are blocked in this sandbox."

  # Payload-level: rate limit model calls (cost control)
  - name: model-call-rate
    match:
      operation: "llm.request"
      when: "rateCount('anthropic:calls:' + context.agent_id, '1h') > 200"
    action: deny
    message: "Rate limit exceeded. Maximum 200 model calls per hour."

  # Log everything
  - name: audit-all
    match:
      operation: "llm.*"
    action: log
```

**Evaluation trace -- secret redaction:**

The agent ran `cat .env` and the tool result contains secrets. The LLM integration decomposes the request into two calls:

```
Call 1:
  operation: "llm.request"
  params: { model: "claude-sonnet-4-20250514", token_estimate: 4200, tool_result_count: 1 }
  context: { agent_id: "cc-xyz", scope: "anthropic-gateway", direction: "request" }

Rule evaluations:
  context-size-limit:  SKIP  (4200 < 150000)
  model-call-rate:     SKIP  (under limit)
  audit-all:           LOG

Result: ALLOW
```

```
Call 2:
  operation: "llm.tool_result"
  params: {
    tool_name: "bash",
    tool_use_id: "abc123",
    content: "DATABASE_URL=postgres://prod:s3cret@db.internal:5432/app\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"
  }
  context: { agent_id: "cc-xyz", scope: "anthropic-gateway", direction: "request" }

Rule evaluations:
  redact-secrets:  REDACT  (AWS key matched, password matched)
  block-phi:       SKIP    (no PHI detected)
  block-db-results: SKIP   (tool_name is "bash", not "sql_query")
  audit-all:       LOG

Result: ALLOW (with mutations)
  Mutated params.content:
    "DATABASE_URL=[REDACTED:SECRET]\nAWS_ACCESS_KEY_ID=[REDACTED:AWS_KEY]"
```

The LLM integration takes the mutated content and patches it back into the original tool_result block before forwarding to Anthropic. The model sees that a .env file exists and what keys are configured, but not the actual values.

**Evaluation trace -- blocking a dangerous tool call:**

The model responds wanting to run `rm -rf /data/production/old-backups`.

```
Call 1:
  operation: "llm.response"
  params: { stop_reason: "tool_use", tool_use_count: 1 }
  context: { agent_id: "cc-xyz", scope: "anthropic-gateway", direction: "response" }

Rule evaluations:
  audit-all: LOG

Result: ALLOW
```

```
Call 2:
  operation: "llm.tool_use"
  params: { name: "bash", input: { command: "rm -rf /data/production/old-backups" } }
  context: { agent_id: "cc-xyz", scope: "anthropic-gateway", direction: "response" }

Rule evaluations:
  block-destructive-bash:  DENY  (matches "rm -rf")

Result: DENY
  policy: "block-destructive-bash"
  message: "Destructive command blocked."
```

The LLM integration handles the deny by stripping the tool_use block from the response and (optionally) injecting a text block with the denial message, so the agent harness knows the model tried something that was blocked.

---

## Evaluation Result

What the engine returns to the integration layer:

```
EvalResult {
  decision    "allow" | "deny" | "redact"
  rule        string | null       // which rule fired (null if allow)
  message     string | null       // human/agent-readable explanation
  mutations   []Mutation | null   // for redact: what was changed
  audit       AuditEntry          // always populated
}

Mutation {
  path        string              // field path that was mutated
  original    string              // what was there (for audit only, not returned to caller)
  replaced    string              // what it became
}

AuditEntry {
  timestamp        time
  scope            string
  operation        string
  agent_id         string
  user_id          string | null
  direction        string | null
  decision         string
  rule             string | null
  rules_evaluated  []RuleResult   // every rule that was checked
  params_summary   string         // truncated/hashed params (not full content)
}
```

The integration layer decides what to do with this result. The MCP relay returns an MCP error. The LLM gateway patches the mutated content back into the payload or strips a blocked tool_use. A future library caller handles it inline. The engine doesn't know or care.

---

## Profiles (field aliases)

Profiles are optional. They map short names to param paths so rules read more naturally. The engine resolves aliases before evaluating expressions.

```yaml
# profiles/linear.yaml
name: linear
aliases:
  team:         "params.teamId"
  assignee:     "params.assigneeId"
  priority:     "params.priority"
  title:        "params.title"
  description:  "params.description"
  labels:       "params.labels"
  state:        "params.stateId"
  project:      "params.projectId"
```

With the profile:

```yaml
# without profile
- name: no-auto-p0
  match:
    operation: "create_issue"
    when: "params.priority == 0"

# with profile
- name: no-auto-p0
  match:
    operation: "create_issue"
    when: "priority == 0"
```

Both evaluate identically. The profile saves typing and makes rules scannable when you have 30 of them.

---

## Starter Packs

Pre-built rule sets for common APIs. Import and override.

```yaml
# starter-packs/linear-safe-defaults.yaml
name: linear-safe-defaults
profile: linear
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted. Archive instead."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "priority == 0"
    action: deny
    message: "P0 issues must be created by a human."

  - name: no-close-issues
    match:
      operation: "update_issue"
      when: "state in ['done', 'cancelled']"
    action: deny
    message: "Agents cannot close or cancel issues."
```

Usage with overrides:

```yaml
scope: linear-tools
packs:
  - name: linear-safe-defaults
    overrides:
      no-auto-p0: disabled               # this agent is trusted for P0
      no-close-issues:
        when: "state == 'cancelled'"      # allow closing, block cancelling
rules:
  # additional rules beyond the pack
  - name: team-allowlist
    match:
      operation: "create_issue"
      when: "!(team in ['TEAM-ENG', 'TEAM-INFRA'])"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure."
```

---

## Expression Language

Available in all `when` clauses across all scopes.

**Field access:**
`params.field`, `params.nested.field`, `context.agent_id`, `context.direction`

**Comparison:**
`==`, `!=`, `>`, `<`, `>=`, `<=`

**Logic:**
`&&`, `||`, `!`, parentheses

**Collections:**
`x in [list]`, `collection.any(item, expr)`, `collection.all(item, expr)`, `size(collection)`

**String:**
`matches(regex)`, `startsWith(prefix)`, `endsWith(suffix)`, `contains(substr)`

**Content patterns:**
`containsAny(field, [terms...])` -- keyword search
`containsPII(field)` -- named regex library (SSN, CC, etc.)
`containsPHI(field)` -- named regex library (MRN, DOB patterns, etc.) + future DLP hook

**Temporal:**
`inTimeWindow(start, end, timezone)` -- is `context.timestamp` within window?
`dayOfWeek()` -- day name from `context.timestamp`

**Rate:**
`rateCount(key, window)` -- call count matching key in sliding window ("1h", "24h"). Requires a local counter store (in-memory or embedded KV). Not a network call.

**Utility:**
`estimateTokens(field)` -- rough token count for a string or message array
`size(field)` -- length of string, array, or map

---

## Integration Layer Responsibilities

The engine is the core. Each integration is a normalizer + transport handler. Here's what each does:

**MCP relay:**
- Accepts MCP connections from agents, proxies to downstream MCP servers
- Maps: tool name -> operation, tool input -> params
- On deny: returns MCP error with policy name and message
- On redact: mutates tool input or result before forwarding
- One tool call = one Keep call (one-to-one)

**LLM gateway:**
- Accepts HTTP from agent (via base URL config), forwards to LLM provider
- Decomposes request into N+1 calls: one `llm.request` summary + one per content block (`llm.tool_result`, `llm.tool_use`, `llm.text`)
- Decomposes response similarly: one `llm.response` summary + one per content block
- On deny of any call: blocks the entire request or strips the specific block from the response
- On redact: patches mutated content back into the original payload before forwarding
- Direction comes from context: request = agent-to-model, response = model-to-agent

**Library (agent applications, custom proxies, middleware):**
- The caller constructs call objects directly and invokes `keep.evaluate(call)`
- No protocol normalization needed -- the caller already knows its own domain
- Agent applications (email agent, data pipeline agent, DevOps agent) build calls that describe what they're about to do and check policy before doing it
- API gateways and middleware can normalize their protocol (REST, GraphQL, gRPC) into call objects and use Keep as an inline policy check
- Caller handles deny/redact in whatever way makes sense for its context
- Keep ships the engine as a library; the caller owns transport, auth, and error handling

**Moat composition:**
- Moat routes traffic to the MCP relay or LLM gateway transparently
- Moat populates `context.labels` with sandbox metadata (sandbox ID, credential set, etc.)
- Keep respects Moat's credential injection -- never asks for credentials itself

---

## Open Interface Questions

**LLM decomposition granularity.** We're decomposing to the content block level. Should `llm.text` blocks (the model's natural language responses) be evaluable too? Use case: blocking the model from outputting certain content. Risk: this starts to look like output moderation, which is a different problem. Leaning toward including `llm.text` in the decomposition but not shipping rules for it in starter packs.

**Redaction target syntax.** For MCP and LLM tool_result calls, the redaction target is usually `params.content` (a flat string). But some tool results are structured JSON. Do we need deep path targeting for redaction, or is "scan this string field for patterns" sufficient for launch?

**Deny propagation in LLM responses.** When the engine denies an `llm.tool_use` call, the LLM integration needs to decide: strip just that block, or block the entire response? If the model wanted to call two tools and one is denied, do we forward the other? Leaning toward blocking the entire response -- partial responses create confusing agent behavior.

**Context enrichment.** `context.labels` is the extensibility point. Moat can set `sandbox_id`, a CI integration can set `repo` and `branch`, a custom harness can set anything. But who is responsible for populating common fields like `user_id`? The integration layer (from auth headers)? The engine config (static mapping)? Both?

**Expression language implementation.** CEL is the pragmatic choice (Go/Rust libs, Kubernetes precedent, proven at scale). Its collection operators (`exists`, `all`, `filter`) have unfamiliar syntax for JS/Python developers. Ship CEL directly, or wrap it in a thin DSL?

**Library API surface.** The Gmail agent example shows Keep as a library with `keep.evaluate(call)`. What's the minimum API? Probably: `loadRules(path) -> Engine`, `engine.evaluate(call) -> EvalResult`, `engine.applyMutations(params, mutations) -> params`. Language bindings: Go first (matches Moat), then Python and TypeScript for agent developers. Alternatively, a single-binary sidecar with a local HTTP/gRPC API avoids the language binding question entirely -- any agent in any language can call it. Tradeoff is latency vs. integration ergonomics.