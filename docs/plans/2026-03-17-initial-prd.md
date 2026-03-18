# Keep -- API-Level Policy Engine for AI Agents

**Product Requirements Document -- Draft v0.6**
**Major Context -- March 2026**

---

## Problem

API tokens are designed for humans. They grant coarse-grained access -- GitHub's `repo` scope gives full read/write to every repository; Gmail's `send` scope lets you email anyone, any time. This was fine when a human with judgment sat between the token and the action.

AI agents don't have judgment. They run unsupervised, at 3am, with the same confidence whether they're doing something routine or catastrophic. The underlying APIs provide no mechanism to say "read-only on production repos" or "only email @company.com addresses during business hours." The permission boundary that humans provided implicitly now needs to be made explicit.

## Solution

Keep is an API-level policy engine for AI agents. It evaluates structured API calls -- operation name, parameters, payloads -- against declarative rules and returns allow, deny, or redact decisions.

Keep operates at the **API layer**, not the transport layer. It doesn't see hosts, ports, or TCP connections. It sees operations and their parameters. "Send a message to #general containing @here" is a Keep concern. "Can this process connect to slack.com on port 443" is not -- that's a firewall, and Moat already handles it.

This separation means Keep's policy engine is portable. The same engine and the same rules can be embedded in an MCP relay, an LLM provider gateway, or directly in agent application code as a library. Keep doesn't own the transport -- it evaluates the calls that travel over it.

Keep ships as three things:

- **`keep`** -- the core engine library. Loads rule files. Exports `evaluate()`. This is Keep.
- **`keep-mcp-relay`** -- a convenience binary that imports the engine, speaks MCP, and proxies to multiple downstream MCP servers. Thin transport shell.
- **`keep-llm-gateway`** -- a convenience binary that imports the engine, speaks HTTP, decomposes LLM message payloads into per-block calls. Thin transport shell.

Rule files are pure policy -- operation names, match expressions, actions. No transport details. The convenience binaries have their own config for transport concerns (upstreams, listen ports, provider type). Library callers don't need transport config at all.

**Moat** is the network layer -- isolation, firewalling, credential injection.
**Keep** is the API layer -- operation-level policy on structured calls.

### Design philosophy: Deny, audit, tune

Keep does not support human-in-the-loop approval queues. Agents that hit a policy boundary are denied immediately with structured feedback. The agent can work around the problem, retry with different parameters, or fail -- just like hitting any other API error.

The tuning loop is asynchronous and human-driven: audit logs capture every policy evaluation (including what would have been blocked in observation mode), and operators use that data to refine policies over time. This keeps the agent's execution loop tight and deterministic while giving humans a clear feedback mechanism that doesn't require them to be online when the agent runs.

This is **harness engineering** -- the practice of shaping agent behavior through external constraints rather than prompt-level instruction. A well-configured Keep ruleset acts like guard rails on a road: the agent is free to navigate, but the boundaries are hard. When an agent hits a boundary, one of two things happens. Either it finds another way to accomplish its task -- the intended way, within policy -- or it fails. Failure is not a bug. It's a signal. That failure feeds into the audit log, a human reviews it, and the governance criteria evolve: the rule was too strict and gets relaxed, or the agent's approach was wrong and the harness worked as designed. Over time, the rules converge on the actual boundaries of safe behavior for a given agent and environment, discovered empirically rather than specified upfront.

## Competitive Landscape

### agentsh (Canyon Road)

Execution-Layer Security. Intercepts at the syscall level -- file opens, socket connects, process spawns. Strongest where agents execute arbitrary code and spawn subprocess trees. Policy enforcement happens at the OS boundary.

**Where Keep differs:** agentsh governs what happens inside a runtime at the OS layer. Keep governs what agents do at the API layer -- the semantic content of their calls, not the system calls that carry them. An agent sandboxed by agentsh can still make an overbroad API call if its token allows it. The two are complementary: agentsh for execution security, Keep for API-level policy.

### Maybe Don't, AI

MCP gateway proxy with CEL-based policy evaluation plus optional AI-assisted evaluation. Proxies MCP tool calls, validates CLI commands, provides structured error guidance back to agents. Ships with audit-only mode for observation before enforcement.

**Where Keep differs:** Maybe Don't is an MCP gateway -- tightly coupled to one protocol and one deployment model. Keep's policy engine operates on a protocol-agnostic abstraction (operation + parameters) and can be embedded in multiple contexts: MCP relay, LLM provider gateway, or directly in agent code as a library call. The engine has no compiled-in API knowledge -- new APIs need only a YAML profile, not a code change.

### Warp / IDE-level permissions

Warp and similar tools (Cursor, Claude Code) implement allowlists and denylists for agent actions within their own environments. These are agent-specific, not infrastructure-level. They protect the developer from their own agent, but don't help an ops team enforce organization-wide policy across multiple agents and tool integrations.

## Core Abstraction

### What Keep sees

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

`direction` is context metadata, not a parameter. It tells the engine whether the call represents something going to a service (request) or coming back (response). Most integrations are request-only. The LLM gateway is bidirectional.

### Rules

A rule has a name, a match condition, and an action:

```yaml
- name: rule-name
  description: "Human-readable explanation"
  match:
    operation: "glob-or-exact"
    when: "expression"
  action: deny | log | redact
  message: "Returned to the agent on deny"
```

Rules are evaluated against every call in a scope. A call must satisfy all rules to proceed. If any rule triggers `deny`, the call is blocked. If any triggers `redact`, params are mutated before forwarding. Every evaluation produces an audit entry.

### Predicates

Rules are built from composable predicates:

- **Temporal** -- time of day, day of week, relative to a schedule
- **Operation** -- operation name matching (exact, glob, regex)
- **Parameter** -- structured field inspection via dot-path, regex matching, PII/PHI pattern detection
- **Rate** -- frequency limits over sliding windows (requires local counter store)
- **Identity** -- which agent, which user delegated access, which role
- **Direction** -- request vs. response (from `context.direction`)

### Actions

- **Deny** -- block the call, return a structured error with guidance on what would be allowed
- **Log** -- allow the call but flag it for audit review
- **Redact** -- allow the call but mask or strip specific fields before forwarding

All actions produce audit log entries.

### Scopes

A scope binds a set of rules to a class of traffic. Scopes define where Keep's engine is applied. They do not define how traffic gets to Keep -- that's the integration layer's job.

## Integration Points

Keep's policy engine can be embedded at multiple points. Each integration translates its protocol into Keep's call shape, evaluates rules, and acts on the result.

### MCP relay (`keep-mcp-relay`)

A single relay process that proxies to multiple downstream MCP servers. One MCP tool call = one Keep call. Tool name becomes `operation`, tool input becomes `params`. The relay presents itself as one MCP server to the agent, multiplexing across upstreams based on its routing config.

```
                          +-- MCP --> Linear MCP Server
                         /
Agent -- MCP --> keep-mcp-relay --- MCP --> Slack MCP Server
                         \
                          +-- MCP --> GitHub MCP Server
```

The agent connects to one endpoint. The relay routes each tool call to the right upstream and evaluates the corresponding scope's rules before forwarding or denying.

**Routing model:** At startup, the relay connects to all configured upstreams and performs MCP tool discovery. It builds a tool-name-to-upstream routing table from the tools each upstream advertises. If two upstreams register the same tool name, the relay fails to start with an explicit conflict error listing the colliding tool name and the two upstreams. This is the zero-config path -- no manual tool-to-scope mapping needed.

The relay maps each tool name to the scope of the upstream that registered it (from the route config). When a tool call arrives, the relay looks up the tool name in the routing table, finds the upstream and scope, evaluates the scope's rules, and forwards or denies.

**MCP protocol scope:** The relay implements MCP Streamable HTTP transport (the current MCP spec transport). It supports: tool discovery (`tools/list`), tool invocation (`tools/call`), and capability negotiation. It does not implement resources, prompts, or sampling -- these can be added later if needed. The relay presents a merged tool list to the agent (union of all upstream tools).

**Upstream health:** If an upstream is unreachable at startup, the relay logs a warning and excludes that upstream's tools from the routing table. If an upstream becomes unreachable during operation, tool calls to that upstream return an MCP error. The relay does not retry -- the agent handles retry logic.

### LLM provider gateway (`keep-llm-gateway`)

Keep sits between the agent runtime and the LLM provider API. The agent sets its base URL to Keep (e.g., `ANTHROPIC_BASE_URL=http://localhost:8080`). Bidirectional -- filters both what the model receives and what the model wants to do.

The LLM integration **decomposes** each API request/response into multiple Keep calls -- one per content block, plus one payload-level summary. This keeps rules flat: instead of navigating nested message arrays, each tool_result and each tool_use becomes its own call with flat params.

**Request decomposition** (agent -> model):

```
1. { operation: "llm.request",     params: { model, system, token_estimate, ... }}
2. { operation: "llm.tool_result", params: { tool_name, content }}
3. { operation: "llm.tool_result", params: { tool_name, content }}
```

**Response decomposition** (model -> agent):

```
1. { operation: "llm.response",    params: { stop_reason, tool_use_count }}
2. { operation: "llm.tool_use",    params: { name, input }}
```

Any deny blocks the whole request/response. Redactions target specific blocks and the integration patches the mutated content back into the original payload before forwarding.

```
Agent runtime --HTTP--> Keep (LLM gateway) --HTTPS--> api.anthropic.com
```

### Library

Keep's core is a policy engine, not a proxy. Agent applications can import the engine directly and evaluate policy inline before making API calls. The caller constructs call objects, invokes `keep.evaluate(call)`, and handles the result.

This pattern works for any agent that calls APIs directly: an email agent using the Gmail API, a data pipeline agent calling Snowflake, a DevOps agent calling Terraform Cloud. The agent author constructs call objects that describe what they're about to do and checks policy before doing it.

```python
# Inside an agent's send_email function
call = {
    "operation": "send_email",
    "params": { "to": ["investor@bigfund.com"], "body": "..." },
    "context": { "agent_id": "email-agent", "scope": "gmail-agent" }
}
result = keep.evaluate(call)
if result.decision == "deny":
    return AgentError(result.message)
if result.decision == "redact":
    # ApplyMutations returns a new map with redacted values.
    # The caller must use the returned params, not the original.
    call["params"] = keep.apply_mutations(call["params"], result.mutations)
# Policy passed (allow or redact) -- make the actual API call
gmail.send(call["params"])
```

**Mutation contract for library callers:** The engine returns a list of `Mutation` objects, each containing the field `path`, the `replaced` value, and (for audit only) the `original` value. The caller is responsible for applying mutations. The `ApplyMutations` helper does this: it takes the original params map and the mutation list, and returns a new map with the specified field paths set to their replacement values. The original map is not modified. If a mutation targets a field path that doesn't exist in params, it is silently skipped. Library callers should not expose `Mutation.Original` to end users -- it contains the pre-redaction content.

### Go API surface

The engine ships as a Go module (`github.com/majorcontext/keep`). The public API:

```go
package keep

// Load reads all rule files from a directory, compiles CEL expressions,
// resolves profiles and starter packs, and returns a ready-to-evaluate engine.
// Returns an error if any rule file fails to parse or any CEL expression
// fails to compile.
func Load(rulesDir string, opts ...Option) (*Engine, error)

// Option configures the engine at load time.
type Option func(*engineConfig)

// WithProfilesDir sets the directory to load profile YAML files from.
func WithProfilesDir(dir string) Option

// WithPacksDir sets the directory to load starter pack YAML files from.
func WithPacksDir(dir string) Option

// Engine is the loaded policy engine. Safe for concurrent use.
type Engine struct { /* unexported */ }

// Evaluate checks a call against all rules in the named scope.
// Returns EvalResult with the decision, matched rule (if any), and audit entry.
// Returns an error if the scope does not exist.
func (e *Engine) Evaluate(call Call, scope string) (EvalResult, error)

// ApplyMutations applies redaction mutations to a params map, returning
// a new map with the mutated values. The original map is not modified.
func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any

// Scopes returns the names of all loaded scopes.
func (e *Engine) Scopes() []string

// Reload re-reads rule files from disk and swaps the internal rule index
// atomically. In-flight evaluations complete against the old rules.
// Returns an error if any file fails to parse; the old rules remain active.
func (e *Engine) Reload() error

// Call is the normalized input to the policy engine.
type Call struct {
    Operation string         `json:"operation"`
    Params    map[string]any `json:"params"`
    Context   Context        `json:"context"`
}

// Context is metadata about who is making the call and when.
type Context struct {
    AgentID   string            `json:"agent_id"`
    UserID    string            `json:"user_id,omitempty"`
    Timestamp time.Time         `json:"timestamp"`
    Scope     string            `json:"scope"`
    Direction string            `json:"direction,omitempty"` // "request" | "response" | ""
    Labels    map[string]string `json:"labels,omitempty"`
}

// EvalResult is the output of a policy evaluation.
type EvalResult struct {
    Decision  Decision    `json:"decision"`            // "allow", "deny", or "redact"
    Rule      string      `json:"rule,omitempty"`       // which rule fired (empty if allow)
    Message   string      `json:"message,omitempty"`    // human/agent-readable explanation
    Mutations []Mutation  `json:"mutations,omitempty"`  // for redact: what to change
    Audit     AuditEntry  `json:"audit"`                // always populated
}

// Decision is the outcome of a policy evaluation.
type Decision string

const (
    Allow  Decision = "allow"
    Deny   Decision = "deny"
    Redact Decision = "redact"
)

// Mutation describes a single field change from a redact action.
type Mutation struct {
    Path     string `json:"path"`               // field path that was mutated
    Original string `json:"original,omitempty"`  // what was there (audit only, not returned to caller by default)
    Replaced string `json:"replaced"`            // what it became
}

// AuditEntry is the structured log record for a single evaluation.
type AuditEntry struct {
    Timestamp      time.Time    `json:"timestamp"`
    Scope          string       `json:"scope"`
    Operation      string       `json:"operation"`
    AgentID        string       `json:"agent_id"`
    UserID         string       `json:"user_id,omitempty"`
    Direction      string       `json:"direction,omitempty"`
    Decision       Decision     `json:"decision"`
    Rule           string       `json:"rule,omitempty"`
    Message        string       `json:"message,omitempty"`
    RulesEvaluated []RuleResult `json:"rules_evaluated"`
    ParamsSummary  string       `json:"params_summary"` // truncated/hashed, not full content
}

// RuleResult records what happened when a single rule was checked.
type RuleResult struct {
    Name    string `json:"name"`
    Matched bool   `json:"matched"`
    Action  string `json:"action,omitempty"`  // only set if matched
    Skipped bool   `json:"skipped,omitempty"` // true if operation glob didn't match
}
```

The `AuditEntry` is always populated regardless of decision. Integration layers can serialize it to their preferred output (JSON Lines to stdout, file, or a custom sink). The engine does not write audit logs itself -- it returns them for the caller to handle. The convenience binaries (`keep-mcp-relay`, `keep-llm-gateway`) and the `keep` CLI handle log output.

`Original` in `Mutation` is populated for audit purposes but should not be returned to the calling agent (it contains the pre-redaction value). Integration layers must strip this field before returning results to agents.

### Moat composition

Inside a Moat sandbox, Keep runs as a sidecar. Moat's network layer routes traffic to Keep transparently. Moat handles credential injection; Keep handles operation-level policy. This covers agents that don't expose configurable base URLs and provides catch-all coverage.

## User Stories

### US-1: Constrain GitHub access beyond token scopes

**As** a platform engineer,
**I want** to restrict an AI coding agent to read-only access on `main` and write access only on branches matching `agent/*`,
**so that** the agent can contribute code without the risk of force-pushing to production,
**even though** the underlying GitHub token grants full `repo` scope.

**Implementation notes:** When the agent uses GitHub via MCP tools, Keep evaluates the tool call directly. When the agent shells out to `git push` directly, this bypasses the API layer entirely -- that requires Moat's tool intercept proxy or agentsh's syscall-level enforcement. Keep's domain is API calls, not CLI commands.

### US-2: Time-based email restrictions

**As** a team lead using an AI assistant for outbound communications,
**I want** to prevent the agent from sending external emails outside of business hours (9am-6pm local),
**so that** a runaway agent doesn't blast emails at 2am,
**with** the agent receiving a clear denial so it can defer or limit to internal recipients.

### US-3: Content-level messaging constraints

**As** an admin of a company Slack workspace,
**I want** to prevent an AI agent from using `@here` or `@channel` mentions, and block messages referencing sensitive topics (e.g., M&A activity, unannounced products),
**so that** the agent can participate in channels but can't broadcast or leak sensitive context.

**Implementation notes:** Slack token scopes can already restrict which channels an agent can post to. Keep's value is parameter-level filtering that tokens don't support -- inspecting message content for mention patterns, topic keywords, or other semantic constraints.

### US-4: Rate limiting across integrations

**As** an ops engineer,
**I want** to set a rate limit of N API calls per hour per agent, with per-scope overrides,
**so that** a misbehaving agent loop doesn't burn through API quotas or trigger upstream rate limits.

### US-5: Content-aware parameter filtering

**As** a compliance officer,
**I want** to block any API call whose parameters contain patterns matching PII (SSNs, credit card numbers),
**so that** an agent can't accidentally exfiltrate sensitive data through a tool integration.

### US-6: Compose Keep inside a Moat run

**As** a developer using Moat to sandbox agent execution,
**I want** to add Keep rules to my Moat configuration so that credential injection and API-level policy enforcement are managed together.

### US-7: Audit trail for all evaluated calls

**As** a security engineer,
**I want** a structured log of every call Keep evaluated -- the operation, agent identity, rules evaluated, and decision -- so that I can investigate incidents and demonstrate compliance.

### US-8: Filter what the LLM sees

**As** a developer using Claude Code in a codebase with sensitive configuration,
**I want** Keep to redact secrets from tool results before they reach the LLM provider,
**so that** even if the agent reads a file containing API keys, that data is masked before it hits the Anthropic API.

**Implementation notes:** The LLM gateway decomposes the messages payload into per-block calls. A tool_result containing `.env` contents becomes a flat `{operation: "llm.tool_result", params: {tool_name: "bash", content: "..."}}` call. The redact-secrets rule matches on `params.content` -- no nested array navigation. The integration patches the redacted content back into the payload before forwarding.

### US-9: Prevent PHI exfiltration to LLM providers

**As** a healthcare startup CTO,
**I want** to block or redact Protected Health Information (PHI) before any data is sent to the agent's LLM provider,
**so that** we can use AI agents without risking HIPAA violations from PHI leaking into model provider infrastructure.

**Implementation notes:** PHI detection is substantially harder than PII pattern matching. MRNs look like any other number, diagnosis codes require medical context, and patient names are just names. May require a dedicated detection model or DLP service integration. Likely a later milestone, but the engine must support pluggable content inspection.

### US-10: Observation mode before enforcement

**As** someone rolling Keep out to an existing agent workflow,
**I want** to run Keep in audit-only mode where rules are evaluated and logged but not enforced,
**so that** I can see what would be blocked before turning enforcement on.

### US-11: Policy feedback to agents

**As** an AI agent developer,
**I want** Keep's denial responses to include structured guidance (which rule fired, what would be allowed instead),
**so that** the agent can adjust autonomously rather than failing opaquely.

### US-12: Use Keep in a custom agent application

**As** a developer building an agent that calls APIs directly (not via MCP),
**I want** to import Keep as a library and evaluate policy inline before making API calls,
**so that** I get the same policy enforcement as MCP-based agents without needing a proxy.

## Policy Language

### Principles

1. **Operates on operations and parameters, not transport.** Rules reference operation names and parameter fields. No HTTP methods, no URL paths, no headers. Integration layers normalize protocol-specific details before the engine sees them.
2. **Every rule sees flat params.** MCP tool calls have flat params naturally. The LLM integration decomposes messages into per-block calls so rules stay flat there too. No rule should need to navigate nested arrays.
3. **Profiles are sugar, not infrastructure.** A profile maps short aliases to param paths. `branch == 'main'` resolves to `params.branch == 'main'`. New APIs need a YAML file, not a code change.
4. **Starter packs are opinions, not requirements.** Curated rule sets for common APIs, like eslint shared configs. Import and override.
5. **Same expression language everywhere.** Temporal, content matching, rate counting, regex -- available in all scopes regardless of integration point.

### Configuration

Rule files and integration configs are separate. Rule files are pure policy -- the engine loads them. Integration configs are transport -- the convenience binaries load them.

**Rule files (loaded by the engine):**

```yaml
# rules/linear.yaml
scope: linear-tools
profile: linear
rules:
  - name: team-allowlist
    match:
      operation: "create_issue"
      when: "!(params.teamId in ['TEAM-ENG', 'TEAM-INFRA'])"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "params.priority == 0"
    action: deny
    message: "P0 issues must be created by a human. Use priority 1 or lower."

  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted. Archive instead."

  - name: no-sensitive-content
    match:
      operation: "create_issue"
      when: >
        containsAny(params.title, ['acquisition', 'merger', 'RIF', 'layoff'])
        || containsAny(params.description, ['acquisition', 'merger', 'RIF', 'layoff'])
    action: deny
    message: "Issue contains sensitive business terms. Create manually."
```

```yaml
# rules/slack.yaml
scope: slack-tools
rules:
  - name: no-broadcast-mentions
    match:
      operation: "send_message"
      when: "params.text.matches('<!here>|<!channel>')"
    action: deny
    message: "Broadcast mentions (@here, @channel) are not permitted."

  - name: no-sensitive-topics
    match:
      operation: "send_message"
      when: "containsAny(params.text, ['acquisition', 'merger', 'LOI', 'term sheet'])"
    action: deny
    message: "Message contains sensitive business terms."
```

```yaml
# rules/anthropic.yaml
scope: anthropic-gateway
rules:
  # Request direction: what the model is about to see
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

  # PHI detection is not handled by a built-in function. For structured PHI
  # patterns (MRN, ICD codes, labeled fields), use explicit regex patterns in
  # redact rules targeting specific fields. Unstructured PHI detection will be
  # addressed via pluggable predicates (LLM-as-judge) in the future.

  - name: context-size-limit
    match:
      operation: "llm.request"
      when: "params.token_estimate > 150000"
    action: deny
    message: "Context exceeds 150k token limit."

  # Response direction: what the model wants to do
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: "params.name == 'bash' && params.input.command.matches('rm -rf|DROP TABLE|TRUNCATE')"
    action: deny
    message: "Destructive command blocked."

  - name: tool-denylist
    match:
      operation: "llm.tool_use"
      when: "params.name in ['curl', 'wget', 'nc', 'ssh']"
    action: deny
    message: "Networking tools are blocked in this sandbox."

  - name: model-call-rate
    match:
      operation: "llm.request"
      when: "rateCount('anthropic:calls:' + context.agent_id, '1h') > 200"
    action: deny
    message: "Rate limit exceeded. Maximum 200 model calls per hour."
```

```yaml
# rules/gmail.yaml
scope: gmail-agent
rules:
  - name: business-hours-external
    match:
      operation: "send_email"
      when: >
        !params.to.all(addr, addr.endsWith('@ourcompany.com'))
        && !inTimeWindow(now, '09:00', '18:00', 'America/Los_Angeles')
    action: deny
    message: "External emails blocked outside 9am-6pm PT."

  - name: blocked-domains
    match:
      operation: "send_email"
      when: "params.to.any(addr, addr.endsWith('@competitor.com') || addr.endsWith('@press.org'))"
    action: deny
    message: "Sending to this domain is not permitted."

  # PII detection (SSN, credit card, etc.) is handled via explicit regex
  # patterns in redact rules targeting specific fields, not a built-in
  # containsPII function. See the redact action and redact block syntax.

  - name: send-rate
    match:
      operation: "send_email"
      when: "rateCount('gmail:send:' + context.agent_id, '1h') > 30"
    action: deny
    message: "Rate limit exceeded. Maximum 30 emails per hour."
```

Nothing in these files mentions upstreams, ports, or protocols. The engine loads a directory of rule files and indexes them by scope name. Any caller -- relay, gateway, library -- can evaluate against any scope.

**MCP relay config (loaded by `keep-mcp-relay`):**

```yaml
# keep-mcp-relay.yaml
listen: ":8090"
rules_dir: "./rules"
routes:
  - scope: linear-tools
    upstream: "https://mcp.linear.app/mcp"

  - scope: slack-tools
    upstream: "https://slack-mcp-server.example.com"

  - scope: github-tools
    upstream: "https://api.githubcopilot.com/mcp/"
```

One process, one listen port, multiple upstreams. The agent connects to `:8090` and sees tools from all three MCP servers. The relay routes each tool call to the right upstream based on which server registered that tool, evaluates the matching scope's rules, and forwards or denies.

**LLM gateway config (loaded by `keep-llm-gateway`):**

```yaml
# keep-llm-gateway.yaml
listen: ":8080"
rules_dir: "./rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: anthropic-gateway
```

The agent sets `ANTHROPIC_BASE_URL=http://localhost:8080`. The gateway decomposes each request/response into per-block calls, evaluates against the `anthropic-gateway` scope, and forwards the (potentially mutated) payload.

**Library callers (no transport config needed):**

```python
engine = keep.load("./rules")
result = engine.evaluate(call, scope="gmail-agent")
```

The rule files are identical in all three cases. The Linear rules work whether they're loaded by the MCP relay, by a library caller, or by a future integration that doesn't exist yet.

Note how the LLM rules look just like the MCP rules -- flat field checks against `params`. The LLM gateway's block-level decomposition makes this possible. `llm.tool_result` is as flat as `create_issue` or `send_email`.

### Integration-specific normalization

Each integration translates protocol details into Keep's call shape. This is the only place transport concerns appear -- and it's the integration's job, not the engine's:

| Caller | How `operation` is derived | How `params` is derived | Cardinality |
|---|---|---|---|
| `keep-mcp-relay` | Tool name | Tool input object | 1:1 (one tool call = one Keep call) |
| `keep-llm-gateway` | Block type prefixed with `llm.` (`llm.tool_result`, `llm.tool_use`, `llm.request`, `llm.response`) | Flat fields from each content block | N+1 per API request (one summary + one per block) |
| Library caller | Caller provides directly | Caller provides directly | Caller controls |

### Profiles

A profile maps semantic aliases to parameter paths:

```yaml
# profiles/linear.yaml
name: linear
aliases:
  team:         "params.teamId"
  assignee:     "params.assigneeId"
  priority:     "params.priority"
  title:        "params.title"
  description:  "params.description"
```

With a profile, `priority == 0` resolves to `params.priority == 0`. Without it, you write `params.priority` directly. Profiles add readability, not capability.

**Alias resolution scope:** Aliases resolve exclusively to `params.*` paths. They cannot target `context.*` fields -- context fields are stable across all APIs and don't benefit from aliasing. An alias like `agent: "context.agent_id"` is rejected at load time. This keeps profiles focused on their purpose: making API-specific parameter names more readable.

### Starter Packs

Pre-built rule sets. Import and override:

```yaml
# starter-packs/linear-safe-defaults.yaml
name: linear-safe-defaults
profile: linear
rules:
  - name: no-delete
    match:
      operation: "delete_issue"
    action: deny
    message: "Issue deletion is not permitted."

  - name: no-auto-p0
    match:
      operation: "create_issue"
      when: "priority == 0"
    action: deny
    message: "P0 issues must be created by a human."

  - name: no-close-issues
    match:
      operation: "update_issue"
      when: "params.stateId in ['done', 'cancelled']"
    action: deny
    message: "Agents cannot close or cancel issues."
```

Usage with overrides:

```yaml
# rules/linear.yaml
scope: linear-tools
profile: linear
packs:
  - name: linear-safe-defaults
    overrides:
      no-auto-p0: disabled
      no-close-issues:
        when: "params.stateId == 'cancelled'"
rules:
  - name: team-allowlist
    match:
      operation: "create_issue"
      when: "!(team in ['TEAM-ENG', 'TEAM-INFRA'])"
    action: deny
    message: "This agent may only create issues in Engineering and Infrastructure."
```

**Pack interaction with inline rules:** Pack rules evaluate before inline rules, and deny short-circuits. There is no "allow" action that overrides a deny. If a pack rule denies a call, inline rules never see it. To relax a pack rule, use overrides: `disabled` removes it, or replace its `when` clause with a narrower condition. This is intentional -- deny is a hard boundary, not a suggestion.

### Expression language

Shared across all scopes and integration types:

**Field access:** `params.field`, `params.nested.field`, `context.agent_id`, `context.direction`

**Comparison:** `==`, `!=`, `>`, `<`, `>=`, `<=`

**Logic:** `&&`, `||`, `!`, `in`, parentheses

**Collections:** `collection.any(item, expr)`, `collection.all(item, expr)`, `size(collection)`

**String:** `matches(regex)`, `startsWith(prefix)`, `endsWith(suffix)`, `contains(substr)`

**Content patterns:** `containsAny(field, [terms...])` -- keyword search. PII/PHI detection uses explicit regex patterns in redact rules.

**Temporal:** `inTimeWindow(now, start, end, tz)`, `dayOfWeek(now)`, `dayOfWeek(now, tz)` -- `now` is a top-level CEL variable of type `timestamp`, automatically bound to `context.timestamp` at eval time.

**Rate:** `rateCount(key, window)` -- sliding window counters. Requires a local counter store (in-memory or embedded KV), not a network call. This is the one predicate that is not purely stateless.

### Evaluation result

What the engine returns:

```
EvalResult {
  decision         "allow" | "deny" | "redact"
  rule             string | null
  message          string | null
  mutations        []Mutation | null
  audit            AuditEntry
}
```

The integration layer decides what to do with this. The MCP relay returns an MCP error. The LLM gateway patches mutated content back into the payload or strips a denied block. A library caller handles it inline. The engine doesn't know or care.

### Error handling

Errors fall into two categories: **load-time** (when rules are read) and **eval-time** (when a call is evaluated).

**Load-time errors** are fatal. If a rule file fails to parse, a CEL expression fails to compile, a profile alias shadows a built-in, or a starter pack reference doesn't resolve, `Load()` returns an error and the engine is not created. The caller (CLI, relay, gateway) must handle this -- typically by logging the error and exiting. `keep validate` exercises the same code path. Hot-reload (`Engine.Reload()`) follows the same rule: if any file fails, the reload is rejected and the old rules remain active.

**Eval-time errors** follow the scope's fail mode (R-13):

| Error | Fail-closed (default) | Fail-open |
|---|---|---|
| CEL expression returns non-bool | Deny (with rule name and error message) | Allow (log the error in audit entry) |
| CEL expression panics (nil access, type mismatch) | Deny | Allow (log) |
| CEL expression exceeds 5ms timeout | Deny | Allow (log) |
| `rateCount()` counter store unavailable | Deny | Allow (log) |
| Unknown field path in `has()` / field access | Expression evaluates to `false` / `null` (CEL null-safety) | Same |
| Scope not found | `Evaluate()` returns an error (not a deny -- this is a caller bug) | Same |

The fail mode is per-scope, set via a new `on_error` field:

```yaml
scope: linear-tools
mode: enforce
on_error: closed   # "closed" (default) or "open"
```

In `audit_only` mode, eval-time errors are always logged but never enforced -- consistent with the mode's purpose. The `on_error` setting only affects behavior in `enforce` mode.

When a CEL expression error triggers a deny (in fail-closed mode), the `EvalResult.Message` includes the error detail: `"Rule 'team-allowlist' evaluation error: type mismatch: got string, expected int. Call denied (fail-closed)."` This gives the agent (and the operator reviewing logs) enough information to diagnose the problem.

## Requirements

### Functional

**R-1: Protocol-agnostic policy engine.** Keep's core evaluates rules against a normalized call shape (operation + params + context). The engine has no knowledge of transport protocols. It loads rule files from a directory, indexes by scope name, and exports `evaluate()`. Ships as a Go library.

**R-2: `keep-mcp-relay`.** A convenience binary that imports the engine, speaks MCP Streamable HTTP, and proxies to multiple downstream MCP servers from a single listen port. Routes tool calls to the correct upstream, evaluates the corresponding scope's rules, and returns MCP-formatted results. This is the launch integration.

**R-3: `keep-llm-gateway`.** A convenience binary that imports the engine, accepts LLM API requests (via base URL config), decomposes each request/response into per-content-block calls, evaluates rules on each, and reassembles the (potentially mutated) payload before forwarding. Must support Anthropic messages API at launch. OpenAI-compatible as a fast follow.

**R-4: Library API.** The engine is importable directly. Callers construct call objects and invoke `evaluate()`. No transport, no proxy. The library, the relay, and the gateway share the same engine, rule format, and expression language. Rule files are the same regardless of which caller loads them.

**R-5: Config separation.** Rule files (pure policy) and integration configs (transport details) are distinct files. Rule files contain scope name, profile, packs, and rules. Integration configs contain listen ports, upstreams, and provider type. Library callers only need rule files.

**R-6: Moat composition.** Keep must be deployable as a sidecar within a Moat sandbox. Moat routes traffic to the relay or gateway transparently. Moat config references Keep rule files. Keep respects Moat's credential injection.

**R-7: Audit logging.** Every evaluated call produces a structured log entry. The engine returns an `AuditEntry` struct (see Go API surface); the caller is responsible for serialization and output. The convenience binaries (`keep-mcp-relay`, `keep-llm-gateway`) and the `keep` CLI write JSON Lines to their configured output (stdout by default, configurable to stderr or a file path via the `log.output` field in integration configs). Each line is a single JSON object -- one `AuditEntry` per evaluation. The `params_summary` field contains a truncated representation of the params (first 256 characters of the JSON-serialized params, or a SHA-256 hash if the params contain fields targeted by redaction rules). Full params are never written to the audit log.

**R-8: Observation mode.** `audit_only` mode per-scope: rules are evaluated and logged but calls always proceed. Default for new scopes.

**R-9: Denial responses.** Denied calls return a structured error: rule name, human-readable message, optional suggestion for an allowed alternative.

**R-10: Redaction.** The engine can indicate field mutations (patterns matched, replacement values). For proxy integrations (MCP, LLM), the integration applies mutations before forwarding. For library callers, the result includes mutations for the caller to apply.

**R-11: Profiles and starter packs.** Field alias YAML files (profiles) and curated rule sets (starter packs) loadable from filesystem. No code changes to support new APIs.

### Non-Functional

**R-12: Latency.** Rule evaluation < 10ms p99. Redaction adds < 20ms p99 for regex patterns.

**R-13: Availability.** Fail-open or fail-closed per scope. Default: fail-closed.

**R-14: Evaluation model.** Evaluating a single rule against a single call is stateless -- no network calls, no database queries. The exception is `rateCount()`, which maintains local counters (in-memory or embedded KV). This is a deliberate tradeoff: rate limiting is too useful to omit, and local counters stay within the latency budget.

**Rate counter lifecycle:** The counter store is created per `Engine` instance. At launch, counters start at zero. Counters are in-memory for M0 -- no persistence across restarts. A restart resets all counters. Counters are keyed by the string passed to `rateCount()` (typically `"scope:action:agent_id"`). Expired entries (outside any active window) are garbage-collected periodically (every 60 seconds). The maximum window is 24 hours, so counter memory is bounded: each unique key holds at most 24 hours of timestamps. The counter store is safe for concurrent use. For M0, this is sufficient. Persistent counters (surviving restarts) are a future consideration if demand warrants it -- likely an embedded KV (bbolt or similar) behind the same interface.

**R-15: Configuration hot-reload.** Rule file changes take effect without restarting the engine, relay, or gateway.

## Architecture

```
  rules/                          Config files (transport)
  +-- linear.yaml                 +-- keep-mcp-relay.yaml
  +-- slack.yaml                  +-- keep-llm-gateway.yaml
  +-- anthropic.yaml
  +-- gmail.yaml
      |                               |               |
      v                               v               v
  +---------------------------------------------------+
  |           keep (policy engine library)             |
  |                                                    |
  |   { operation, params, context }                   |
  |         |                                          |
  |   evaluate(rules) -> allow / deny / redact         |
  |                                                    |
  |   shared: expressions, profiles, packs, counters   |
  +-------------------+-------------------------------+
                       |
         +-------------+-------------+
         v             v             v
  keep-mcp-relay  keep-llm-gw    Library callers
  (1 process,     (base URL       (any language,
   N upstreams)    gateway)        no transport)
     |    |           |                |
     v    v           v                v
  Linear Slack    Anthropic       Agent code
  MCP    MCP      API             (Gmail, etc.)

                      ^
                      | (optional)
                +-----+------+
                |    Moat    |  transparent routing,
                |  Sandbox   |  credential injection,
                |            |  network-level firewall
                +------------+
```

The policy engine is the core. It ships as a library. `keep-mcp-relay` and `keep-llm-gateway` are thin convenience binaries -- transport shells that import the library, load their own config for routing, and call `evaluate()` on every call. Adding a new integration means writing a new shell. The engine and rule language don't change.

## Milestones

### M0: Policy engine + MCP relay (3-4 weeks)

- `keep` engine library: rule loading, call evaluation, expression language, audit logging
- Library API: `load(rules_dir)`, `evaluate(call, scope)`, `applyMutations(params, mutations)`
- Rule file format: scope, profile, rules with match/when/action
- `keep-mcp-relay`: single process, multiple upstreams, single listen port
- Relay routing: tool calls dispatched to correct upstream by tool registration
- Deny action with structured MCP error responses
- JSON audit log to stdout
- CLI: `keep validate` (check rule files), `keep test` (simulate calls against rules) -- see CLI specification below
- Linear MCP profile + starter pack as first examples

### M1: LLM gateway + observation mode (3-4 weeks)

- `keep-llm-gateway`: Anthropic messages API
- Block-level decomposition (per tool_result, tool_use, request/response summary)
- Bidirectional filtering (request + response)
- Redact action (secret stripping, payload reassembly)
- Observation (audit_only) mode
- Temporal predicates, rate limiting with local counters
- Slack MCP profile + starter pack

### M2: Moat integration + library packaging (3-4 weeks)

- Sidecar deployment within Moat sandbox
- Moat config references Keep rule files and relay/gateway configs
- Credential passthrough from Moat
- Library packaging: Go module, Python bindings (or sidecar with local API for other languages)
- Starter pack import/override syntax
- GitHub MCP profile + starter pack

### M3: Content inspection + production hardening

- PII/PHI detection via user-supplied regex patterns in redact rules (already supported)
- Pluggable predicate interface research (LLM-as-judge for unstructured PHI detection)
- OpenAI-compatible LLM gateway
- Configuration hot-reload
- Fail-open/fail-closed modes
- Dashboard or structured log viewer

## CLI Specification

### `keep validate`

Validates rule files, profiles, and starter packs without evaluating any calls. Catches errors before deployment.

```bash
keep validate ./rules
keep validate ./rules --profiles ./profiles --packs ./starter-packs
```

**What it checks:**

1. YAML syntax (parse all `.yaml` and `.yml` files)
2. Required fields present (`scope` and `rules` in rule files, `name` and `aliases` in profiles, `name` and `rules` in packs)
3. Scope name uniqueness across all loaded files
4. Scope name format (`[a-z][a-z0-9-]*`, max 64 chars)
5. Rule name uniqueness within each scope
6. Rule name format (`[a-z][a-z0-9-]*`, max 64 chars)
7. CEL expression compilation (all `when` clauses parse and type-check against the Keep environment)
8. Redact pattern compilation (all `match` fields are valid RE2)
9. Profile alias resolution (aliases don't shadow built-ins or `params`/`context`)
10. Starter pack references resolve (named packs exist in the packs directory)
11. Pack override targets exist (overridden rule names exist in the referenced pack)
12. Field path syntax (dot-separated identifiers in `redact.target` and profile aliases)
13. Expression size limit (max 2048 characters)

**Output:**

```
$ keep validate ./rules
rules/linear.yaml: OK (6 rules, scope: linear-tools, profile: linear)
rules/anthropic.yaml: OK (8 rules, scope: anthropic-gateway)
rules/slack.yaml: OK (2 rules, scope: slack-tools)

3 files, 16 rules, 0 errors
```

On error:

```
$ keep validate ./rules
rules/linear.yaml:14: CEL compilation error in rule "team-allowlist": undeclared reference to 'teamId' (did you mean 'params.teamId'?)
rules/slack.yaml:8: invalid RE2 pattern in rule "no-broadcast": missing closing paren

2 errors
```

Exit code 0 on success, 1 on validation errors, 2 on file system errors (directory not found, permission denied).

### `keep test`

Evaluates calls from fixture files against loaded rules. Used for policy testing -- verify that rules allow, deny, and redact the right things before deploying.

```bash
keep test ./rules --fixtures ./fixtures
keep test ./rules --fixtures ./fixtures/linear-tests.yaml
keep test ./rules --fixtures ./fixtures --profiles ./profiles --packs ./starter-packs
```

**Fixture file format:**

Fixtures are YAML files. Each file contains a list of test cases. A test case is a call plus the expected result.

```yaml
# fixtures/linear-tests.yaml
scope: linear-tools
tests:
  - name: "allow normal issue creation"
    call:
      operation: "create_issue"
      params:
        title: "Fix auth bug"
        teamId: "TEAM-ENG"
        priority: 1
      context:
        agent_id: "test-agent"
    expect:
      decision: allow

  - name: "deny P0 creation"
    call:
      operation: "create_issue"
      params:
        title: "Outage"
        teamId: "TEAM-ENG"
        priority: 0
      context:
        agent_id: "test-agent"
    expect:
      decision: deny
      rule: no-auto-p0

  - name: "deny wrong team"
    call:
      operation: "create_issue"
      params:
        title: "HR task"
        teamId: "TEAM-HR"
        priority: 2
      context:
        agent_id: "test-agent"
    expect:
      decision: deny
      rule: team-allowlist

  - name: "redact secrets in tool result"
    call:
      operation: "llm.tool_result"
      params:
        tool_name: "bash"
        content: "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"
      context:
        agent_id: "test-agent"
        scope: "anthropic-gateway"
    expect:
      decision: redact
      rule: redact-secrets
      mutations:
        - path: "params.content"
          replaced: "AWS_ACCESS_KEY_ID=[REDACTED:AWS_KEY]"
```

Test case fields:

| Field | Required | Description |
|---|---|---|
| `name` | yes | Human-readable test name |
| `call.operation` | yes | Operation name |
| `call.params` | yes | Parameters map |
| `call.context` | no | Context fields (defaults: `agent_id: "test"`, `timestamp: now`, `scope: <file-level scope>`) |
| `expect.decision` | yes | Expected decision: `allow`, `deny`, or `redact` |
| `expect.rule` | no | Expected rule name that fired (if deny or redact) |
| `expect.message` | no | Expected message substring (partial match) |
| `expect.mutations` | no | Expected mutations (for redact; `path` and `replaced` fields checked) |

The file-level `scope` field sets the default scope for all tests in that file. Individual tests can override by setting `call.context.scope`.

**Output:**

```
$ keep test ./rules --fixtures ./fixtures
fixtures/linear-tests.yaml:
  PASS  allow normal issue creation
  PASS  deny P0 creation
  PASS  deny wrong team
fixtures/anthropic-tests.yaml:
  PASS  redact secrets in tool result
  FAIL  block destructive bash
        expected: deny (rule: block-destructive-bash)
        got:      allow

5 tests, 4 passed, 1 failed
```

Exit code 0 if all tests pass, 1 if any test fails, 2 on load errors (bad fixtures, bad rules).

**Interaction with `audit_only` mode:** `keep test` always evaluates rules in enforce mode, regardless of the scope's `mode` setting. Tests verify policy logic, not deployment mode.

## Open Questions

### Resolved

1. **Policy testing.** `keep test` uses hand-written YAML fixture files. Each fixture specifies a call and the expected decision, rule, and (optionally) mutations. See the CLI Specification section above for the full format. Recorded traffic replay is a future consideration -- not needed for M0.

2. **LLM decomposition edge cases.** Resolved: block the entire response. When the engine denies any decomposed call within a request or response, the integration blocks the whole thing. Partial responses create confusing agent behavior and are harder to reason about in rules. This is documented in the LLM gateway section.

3. **MCP relay routing.** Resolved: auto-discovery with conflict detection. The relay connects to all upstreams at startup, performs MCP tool discovery, and builds a tool-name-to-upstream routing table. If two upstreams register the same tool name, the relay fails to start with an explicit error. See the updated MCP relay section above.

### Open (not blocking M0)

4. **Policy versioning.** Do rules need versions, rollback, and diffing? For teams, probably yes. For individual developers, overhead. **M0 position:** rule files are plain files on disk. Versioning is git. No built-in versioning system.

5. **Multi-agent policy.** When multiple agents share a scope, they share rules. Per-agent behavior is expressed via `context.agent_id` in `when` clauses. **Remaining question:** should there be syntactic sugar for per-agent rule overrides, or is the CEL predicate sufficient? Deferring -- CEL predicates are sufficient for launch.

6. **PHI detection feasibility.** Regex catches obvious patterns but PHI is contextual. **Updated position:** `containsPHI()` (and `containsPII()`) have been removed -- they were shallow regex wrappers that gave a false sense of security. Users write explicit regex patterns in redact rules targeting specific fields, which is more transparent and already works. **Future direction:** PHI remains the strongest candidate for the LLM-as-judge predicate (see #8). For structured formats (MRN, ICD codes, labeled fields), explicit regex patterns in redact rules are sufficient. For unstructured clinical text where pattern matching fails, a pluggable predicate interface (`llmJudge`) is the right path. A DLP service integration (Google DLP, AWS Macie) is the middle ground -- better than regex, cheaper than an LLM call, but adds a network dependency.

7. **Defense-in-depth narrative.** Keep (API-level), Moat (network-level), agentsh (syscall-level) form a stack. **M0 position:** document the layering in Keep's docs but don't build cross-product integration until M2 (Moat composition).

8. **AI-assisted policy evaluation.** **M0 position: not in scope.** Adding an LLM call to the evaluation path breaks Keep's default properties (determinism, bounded time, no network calls), so the core engine won't support it. But there are real cases -- nuanced content moderation, PHI detection in unstructured text, intent classification -- where regex and keyword matching aren't enough and operators will accept the latency/cost tradeoff. **Future goal:** a pluggable predicate interface (e.g., `llmJudge(field, prompt)`) that integrations can opt into per-scope. The predicate would be async, have its own timeout, and be explicitly marked as non-deterministic in audit logs. The engine's default path stays fast and stateless; LLM-as-judge is an opt-in extension for scopes that need it.

9. **Profile and starter pack ecosystem.** **M0 position:** profiles and starter packs ship in the Keep repo under `profiles/` and `starter-packs/`. A separate curated repo can come later if the ecosystem grows. For M0, bundle the Linear profile + starter pack as the first example.

10. **Library distribution.** **M0 position:** Go module only. The sidecar binary (`keep-eval-server` or similar, exposing a local HTTP API) is a fast follow if Python/TypeScript demand materializes. Native language bindings are deferred. The Go library is the canonical implementation; everything else calls through it.

11. **LLM provider abstraction depth.** **M1 decision (when the gateway is built):** thin abstraction. The gateway needs to find content blocks (tool_use, tool_result, text) and decompose them into Keep calls. It does not need a normalized schema across providers. Each provider gets its own decomposer (a function that takes a provider-specific request/response and emits Keep calls). Anthropic first, OpenAI second. The decomposer interface is internal -- not part of the public API.