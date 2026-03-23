# MCP Relay Demo — Design Spec

## Goal

Create a self-contained demo showing the Keep MCP relay as a drop-in policy proxy between Claude and a sqlite MCP server. No auth required — just `npx` and an Anthropic API key.

## Architecture

```
Claude CLI → Keep MCP Relay (:19090) → sqlite MCP server (stdio via npx)
```

The relay enforces two policies:
1. **Read-only enforcement** — `write_query` calls are denied
2. **Password redaction** — plaintext passwords in `read_query` results are replaced with `********`

## Changes Required

### 1. Response-side evaluation in relay handler

**File:** `internal/relay/handler.go`

Currently the relay only evaluates policy on the request (tool call arguments). Add a second evaluation after the upstream returns:

```go
// After route.Client.CallTool returns successfully:
// 1. Build response call with result content
// 2. Evaluate with Direction: "response"
// 3. If deny: return error
// 4. If redact: apply mutations to ContentBlock text
// 5. Log audit
```

The response `keep.Call` uses:
- `Operation`: same tool name
- `Params`: `{"content": <joined text from ContentBlocks>}`
- `Direction`: `"response"`

If redacted, replace the `Text` field in each `ContentBlock` with the mutated text.

### 2. Demo files

#### `examples/mcp-relay-demo/seed.sql`

```sql
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  name TEXT,
  email TEXT,
  password TEXT,
  role TEXT
);
INSERT INTO users VALUES (1, 'Alice Chen', 'alice@company.com', 'hunter2', 'admin');
INSERT INTO users VALUES (2, 'Bob Park', 'bob@company.com', 'p@ssw0rd!', 'editor');
INSERT INTO users VALUES (3, 'Carol White', 'carol@company.com', 'letmein123', 'viewer');
INSERT INTO users VALUES (4, 'Dan Rivera', 'dan@company.com', 'qwerty456', 'editor');
INSERT INTO users VALUES (5, 'Eve Foster', 'eve@company.com', 'trustno1!', 'admin');
INSERT INTO users VALUES (6, 'Frank Zhao', 'frank@company.com', 'baseball9', 'viewer');
INSERT INTO users VALUES (7, 'Grace Kim', 'grace@company.com', 'shadow99!', 'editor');
INSERT INTO users VALUES (8, 'Hank Patel', 'hank@company.com', 'dragon123', 'viewer');
INSERT INTO users VALUES (9, 'Iris Novak', 'iris@company.com', 'master!42', 'admin');
INSERT INTO users VALUES (10, 'Jack Torres', 'jack@company.com', 'abc123xyz', 'editor');
INSERT INTO users VALUES (11, 'Karen Liu', 'karen@company.com', 'welcome1!', 'viewer');
INSERT INTO users VALUES (12, 'Leo Santos', 'leo@company.com', 'passw0rd!', 'editor');
```

#### `examples/mcp-relay-demo/rules/demo.yaml`

```yaml
scope: demo-sqlite
mode: enforce

rules:
  - name: block-writes
    match:
      operation: "write_query"
    action: deny
    message: "Database is read-only. Write operations are not permitted."

  - name: redact-passwords
    match:
      operation: "read_query"
      when: "context.direction == 'response'"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "hunter2|p@ssw0rd!|letmein123|qwerty456|trustno1!|baseball9|shadow99!|dragon123|master!42|abc123xyz|welcome1!|passw0rd!"
          replace: "********"

  - name: audit-all
    match:
      operation: "*"
    action: log
```

#### `examples/mcp-relay-demo/relay.yaml`

```yaml
listen: ":19090"
rules_dir: RULES_DIR

routes:
  - scope: demo-sqlite
    command: COMMAND
    args: ARGS

log:
  format: json
  output: LOG_OUTPUT
```

Placeholders (`RULES_DIR`, `LOG_OUTPUT`, `COMMAND`, `ARGS`) are substituted by the demo script. Routes use either `upstream` (HTTP URL) or `command`/`args` (stdio subprocess) — mutually exclusive.

#### `examples/mcp-relay-demo/demo.sh`

The script:
1. Builds `keep-mcp-relay`
2. Creates a temp directory
3. Creates and seeds the sqlite database using `sqlite3`
4. Substitutes paths into `relay.yaml`
5. Starts the relay
6. Prints instructions for connecting Claude

The user then configures Claude's MCP settings to point at `http://localhost:19090` and interacts naturally.

## Testing

- Add engine-level tests in `internal/engine/mcp_*_test.go` for the new rule patterns
- Add handler-level test for response-side evaluation in `internal/relay/handler_test.go`

## Dependencies

- `sqlite3` CLI (for seeding the database)
- `uvx` (for running the official `mcp-server-sqlite` — install via https://docs.astral.sh/uv/)
- Anthropic API access (for Claude CLI)
