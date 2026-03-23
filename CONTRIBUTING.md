# Contributing to Keep

## Development Setup

```bash
git clone https://github.com/majorcontext/keep.git
cd keep
go build ./...
```

## Running Tests

```bash
# Unit tests (includes race detector)
make test-unit

# Single test
make test-unit ARGS='-run TestName'

# With coverage (includes race detector)
make coverage
```

## Linting

```bash
make lint
```

## Architecture

```
cmd/
  keep/                CLI entry point (Cobra commands)
  keep-mcp-relay/      MCP relay binary
  keep-llm-gateway/    LLM gateway binary
internal/
  engine/              Core policy engine (rule loading, evaluation, expression environment)
  cel/                 CEL expression compilation and evaluation
  rule/                Rule file parsing, validation, scope indexing
  profile/             Profile loading and alias resolution
  pack/                Starter pack loading and override merging
  rate/                Local counter store for rateCount()
  audit/               Structured audit logging
  config/              Integration config parsing (relay, gateway)
  relay/               MCP relay transport (upstream routing, MCP protocol)
  gateway/             LLM gateway transport (block decomposition, payload reassembly)
  redact/              Redaction pattern matching and field mutation
```

### Key Flows

**Rule Evaluation:**
1. Integration layer normalizes protocol-specific call into `Call{operation, params, context}`
2. Engine looks up scope, collects rules (pack rules first, then inline)
3. Filters by operation glob, evaluates `when` expressions via CEL
4. Short-circuits on deny, accumulates redactions, records logs
5. Returns `EvalResult{decision, rule, message, mutations, audit}`

**MCP Relay:**
1. Agent connects to relay's listen port via MCP
2. Relay connects to upstream MCP servers, builds tool-name-to-upstream routing table
3. Tool call arrives -> relay maps tool name to scope, constructs Keep call
4. Engine evaluates -> relay forwards or returns MCP error

**LLM Gateway:**
1. Agent sets base URL to gateway (e.g., `ANTHROPIC_BASE_URL=http://localhost:8080`)
2. Request arrives -> gateway decomposes into N+1 calls (summary + per-block)
3. Engine evaluates each call -> gateway patches mutations or blocks
4. Gateway forwards (potentially mutated) payload to upstream provider

**Library Use:**
1. Agent code imports Keep engine, loads rules from directory
2. Before making an API call, agent constructs a `Call` object and invokes `engine.Evaluate()`
3. Agent handles deny/redact inline

### Expression Environment

Keep uses CEL with custom functions:

- `inTimeWindow(start, end, tz)` -- temporal predicate against `context.timestamp`
- `dayOfWeek()` / `dayOfWeek(tz)` -- day name from `context.timestamp`
- `containsAny(field, terms)` -- case-insensitive keyword match
- `lower(field)` -- lowercase string
- `upper(field)` -- uppercase string
- `matchesDomain(field, domain)` -- email domain match
- `hasSecrets(field)` -- detect secrets via gitleaks patterns
- `rateCount(key, window)` -- sliding window counter (local store)
- `estimateTokens(field)` -- rough token count (chars / 4)

## Manual Testing

### MCP relay with Linear

See `demo/` for a working relay example with SQLite.

### LLM gateway with Anthropic

See `demo/` for gateway configuration examples.

### Library integration

See `demo/` for library integration examples.

## Code Style & Guidelines

For code style, error messages, documentation standards, and commit conventions, see [CLAUDE.md](CLAUDE.md).

Key points:
- Follow standard Go conventions and `go fmt`
- Use [Conventional Commits](https://www.conventionalcommits.org/) format: `type(scope): description`
- Error messages should be actionable -- tell users exactly what to do
- Documentation must match actual behavior

## Data Directory Structure

Rules are loaded from a directory specified at startup. Audit logs are written to stdout in structured JSON format. Rate counter state is held in-memory for the lifetime of the process.
