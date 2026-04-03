# Keep

API-level policy engine for AI agents.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
type(scope): description
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`, `build`, `perf`

Scope is optional but encouraged. Use the package or component name (e.g., `engine`, `relay`, `gateway`, `cli`, `cel`).

Examples:
- `feat(engine): add CEL expression compilation`
- `fix(relay): handle upstream disconnect during tool call`
- `docs: update PRD with Go API surface`
- `test(engine): add fixtures for redact evaluation`

## Build & Test

```bash
make build          # Build all packages
make test-unit      # Unit tests with race detector
make lint           # golangci-lint
make fix            # Auto-fix lint/format issues
```

Run a single test:
```bash
make test-unit ARGS='-run TestName'
make test-unit ARGS='-run TestName ./internal/engine'
```

## Code Style

- Follow standard Go conventions and `go fmt`
- Error messages should be actionable -- tell users exactly what to do
- Documentation must match actual behavior

See [CONTRIBUTING.md](CONTRIBUTING.md) for architecture, testing, and development details.

## Releases

Update CHANGELOG.md **before** tagging and pushing. Order: CHANGELOG commit → tag → push both together. This avoids the tag pointing to a commit that doesn't include its own changelog entry.

## Superpowers Overrides

- Save specs and plans to `docs/plans/` (not `docs/superpowers/`)
