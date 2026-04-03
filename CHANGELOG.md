# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
