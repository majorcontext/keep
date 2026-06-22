# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-06-22

### Added

- **Whole-body `hasSecrets`** — `hasSecrets` now accepts a map or list in addition to a string, scanning every string leaf recursively (map values only — keys are field names). `hasSecrets(params.body)` now works over a parsed JSON request body instead of returning `false`; previously only a scalar field (`hasSecrets(params.body.token)`) was detected. Recursion uses the original-case value via the existing `_originalParams` rewrite, so case normalization does not weaken detection.

## [0.5.0] - 2026-06-18

### Added

- **Request body inspection helpers** for HTTP policy evaluation
  - `NewHTTPCallWithBody(method, host, path, body)` constructs a `Call` with the decoded request body exposed under `params.body`, so rules can match on body contents (e.g. `params.body.model == 'gpt-4'` or `hasSecrets(params.body.prompt)`). `body` is typed `any`, accepting a JSON object, array, or scalar. The existing `NewHTTPCall` is unchanged.
  - `Engine.RequiresBody(scope)` reports, from compile-time analysis of the scope's rules, whether any rule references `params.body`. Gatekeepers can use this as a trigger to decide whether to buffer and parse the request body before evaluating. It fails safe: every idiomatic body reference is detected, and an unrecognized use of the `params` map (or an unknown scope) returns `true` so the body is buffered rather than silently skipped.

## [0.4.0] - 2026-05-11

### Added

- **LLM-as-judge** — new `action: judge` rule type that sends matched content to an LLM for allow/deny evaluation
  - Anthropic provider with forced tool use for structured output (`haiku`, `sonnet`, `opus` shortcuts)
  - OpenAI provider with `json_schema` response format (`gpt-4o`, `gpt-4o-mini`, `o3` shortcuts)
  - `judge` block in rules: `model`, `prompt`, `timeout`, `on_error` fields
  - `judge` config block in gateway and relay for provider wiring (`provider`, `api_key_env`, `base_url`)
- **Verdict cache** — in-memory cache for judge verdicts eliminates redundant LLM calls in multi-turn conversations
  - Cache key: `sha256(model + prompt + content)` with null-byte separators
  - Oldest-first eviction, default 10,000 entries, configurable via `judge.WithMaxSize(n)`
  - `Cached` field propagated through `Verdict` → `JudgeResult` → `JudgeAudit` for audit trail visibility
- `keep eval` command for measuring judge quality against labeled datasets
- Judge verdict support in `keep test` fixtures
- Vibe-check demo (`examples/judge-demo/`) — screens messages for passive-aggression, hostility, and profanity
- `WithJudge(fn)` engine option for Go library users
- Documentation for judge across all sections: writing-rules guide, rule-file reference, gateway/relay config, audit logging, Go library, README

## [0.3.0] - 2026-04-03

### Added

- `SafeEvaluate()` — wraps `Engine.Evaluate` with panic recovery, fails closed on `Deny`
- `NewHTTPCall()` — constructs a `Call` for HTTP request policy evaluation (`METHOD host/path` format)
- `NewMCPCall()` — constructs a `Call` for MCP tool-use policy evaluation
- `version` field in rule file schema — defaults to `v1` if absent, rejects unknown versions
- Benchmark suite for engine evaluation (12 sub-benchmarks covering simple match, CEL, globs, redaction, large params)
- Fuzz tests for rule parsing, CEL compilation, and `ValidateRuleBytes`
- LLM evaluation library guide
- v1.0.0 design spec

### Changed

- Centralized version defaulting into `setDefaults` helper in config package
- `RuleSet.Compile` now sets `Version` on constructed rule files

## [0.2.0] - 2026-03-26

### Added

- Case-insensitive matching mode for policy evaluation — normalize operation names and parameters so rules match regardless of casing
- Uppercase literal linter that warns when case-insensitive mode is enabled but rules contain uppercase literals
- `hasSecrets()` now uses original-case parameters for accurate secret detection even under case normalization
- Limitations page in documentation

### Fixed

- **rate**: capture `clock.Now()` before acquiring lock in `Increment` to avoid timing skew
- **rate**: protect `stopCh` with mutex in `StartGC`/`StopGC` to prevent data race
- **gateway**: add bounds check for response block map access
- **cel**: scope `hasSecrets` detection to the named field instead of scanning all params
- **engine**: preserve original-case params in audit `ParamsSummary` for deny paths
- **relay**: use `atomic.Bool` for MCP server initialized flag
- Actionable context added to audit logger and pack resolver error messages

### Changed

- Improved case-insensitive normalization maintainability in engine
- Documentation fixes: redaction `audit_only` behavior, capture group examples, `inTimeWindow` signature
- Updated installation instructions for Homebrew

## [0.1.0] - 2026-03-23

Initial public release.

### Added

- Policy engine with CEL expression evaluation
- YAML rule files with scope, mode, and rule definitions
- Actions: deny, redact, log
- Redaction with regex patterns and gitleaks-based secret detection (~160 patterns)
- Rate limiting via `rateCount()` with sliding window counters
- Temporal predicates: `inTimeWindow()`, `dayOfWeek()`
- Content functions: `containsAny()`, `estimateTokens()`, `matchesDomain()`, `hasSecrets()`
- String functions: `lower()`, `upper()`
- Profiles for field alias mapping
- Starter packs for reusable rule sets
- Definitions (`defs`) for named constants in expressions
- `audit_only` and `enforce` modes
- `on_error: closed | open` for CEL evaluation error handling
- Structured JSON audit logging
- `keep validate` CLI for rule file validation
- `keep test` CLI for fixture-based rule testing
- `keep-mcp-relay` — MCP proxy with per-tool-call policy evaluation
- `keep-llm-gateway` — LLM provider proxy with per-content-block decomposition
- Support for Anthropic Messages API (streaming and non-streaming)
- Bidirectional policy: filter requests and responses
- Documentation site with getting started, concepts, guides, and reference
