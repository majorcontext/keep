# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
