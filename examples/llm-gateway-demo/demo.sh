#!/usr/bin/env bash
#
# Keep LLM Gateway Demo
#
# Runs the keep-llm-gateway in front of the Anthropic API and demonstrates
# policy enforcement. Uses `claude -p` for real API calls (picks up your
# local credentials) and a mock upstream for deny/redact tests.
#
# Usage:
#   ./examples/llm-gateway-demo/demo.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
GW_PORT=18080
MOCK_PORT=18081
GW_MOCK_PORT=18082
GW_PID=""
GW_MOCK_PID=""
MOCK_PID=""

cleanup() {
  for pid in "$GW_PID" "$GW_MOCK_PID" "$MOCK_PID"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

echo "=== Keep LLM Gateway Demo ==="
echo ""

# ── Build ─────────────────────────────────────────────────────────
echo "Building gateway..."
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway
go build -o "$DEMO_DIR/mock-anthropic" "$SCRIPT_DIR/mock-upstream"
echo "  done"
echo ""

# ── Create rules ──────────────────────────────────────────────────
mkdir -p "$DEMO_DIR/rules"
cat > "$DEMO_DIR/rules/gateway.yaml" << 'YAML'
scope: demo-gateway
mode: enforce

defs:
  destructive_patterns: "['rm -rf', 'DROP TABLE', 'TRUNCATE', 'mkfs']"

rules:
  # Redact secrets from tool results before they reach the model
  - name: redact-aws-keys
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

  # Block destructive bash commands from the model
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: >
        params.name == 'bash'
        && containsAny(lower(params.input.command), destructive_patterns)
    action: deny
    message: "Destructive command blocked by policy."

  # Block networking tools
  - name: block-networking
    match:
      operation: "llm.tool_use"
      when: "params.name in ['curl', 'wget', 'nc', 'ssh']"
    action: deny
    message: "Networking tools are blocked by policy."

  # Log everything
  - name: audit-all
    match:
      operation: "llm.*"
    action: log
YAML

# ── Start real gateway (upstream: api.anthropic.com) ──────────────
cat > "$DEMO_DIR/gateway.yaml" << YAML
listen: ":${GW_PORT}"
rules_dir: "$DEMO_DIR/rules"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: demo-gateway
log:
  format: json
  output: "$DEMO_DIR/audit.jsonl"
YAML

echo "Starting gateway on :${GW_PORT} → api.anthropic.com..."
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" 2>&1 &
GW_PID=$!
sleep 1
echo "  Gateway running (PID $GW_PID)"
echo ""

# ── Start mock gateway (upstream: mock server) ────────────────────
"$DEMO_DIR/mock-anthropic" &
MOCK_PID=$!
sleep 0.5

cat > "$DEMO_DIR/gateway-mock.yaml" << YAML
listen: ":${GW_MOCK_PORT}"
rules_dir: "$DEMO_DIR/rules"
provider: anthropic
upstream: "http://localhost:${MOCK_PORT}"
scope: demo-gateway
log:
  format: json
  output: "$DEMO_DIR/audit.jsonl"
YAML

echo "Starting gateway on :${GW_MOCK_PORT} → mock upstream..."
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway-mock.yaml" 2>&1 &
GW_MOCK_PID=$!
sleep 0.5
echo "  Mock gateway running (PID $GW_MOCK_PID)"
echo ""

# ── Test 1: claude -p through gateway ─────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 1: claude -p through the gateway (streaming)"
echo ""
echo "  ANTHROPIC_BASE_URL=http://localhost:${GW_PORT} claude -p 'What is 2+2?'"
echo ""

CLAUDE_OUTPUT=$(ANTHROPIC_BASE_URL="http://localhost:${GW_PORT}" \
  claude -p "What is 2+2? Answer in exactly one word, no punctuation." \
  --model claude-haiku-4-5-20251001 \
  --max-turns 1 \
  2>&1) || true

if [ -n "$CLAUDE_OUTPUT" ]; then
  echo "  ✓ Claude response: $CLAUDE_OUTPUT"
else
  echo "  ✗ claude -p produced no output"
  echo "    Make sure claude is installed and has valid credentials."
fi
echo ""

# ── Test 2: Destructive command blocked ───────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 2: Blocking a destructive bash command"
echo ""
echo "  The mock upstream returns a response with 'rm -rf /data/old-backups'."
echo "  The gateway evaluates the response and blocks it."
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" \
  "http://localhost:${GW_MOCK_PORT}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 200,
    "messages": [{"role": "user", "content": "Delete the old backups please"}]
  }' 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "400" ]; then
  ERROR_MSG=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['error']['message'])" 2>/dev/null || echo "$BODY")
  echo "  ✓ HTTP 400 — BLOCKED: $ERROR_MSG"
else
  echo "  ? HTTP $HTTP_CODE — $BODY"
fi
echo ""

# ── Test 3: Secret redaction ──────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 3: Secret redaction in tool results"
echo ""
echo "  Sending a tool_result with an AWS key and password through the"
echo "  gateway. The mock upstream records what it received so we can"
echo "  verify secrets were redacted before leaving the gateway."
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" \
  "http://localhost:${GW_MOCK_PORT}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 100,
    "messages": [
      {"role": "user", "content": "Read the .env file"},
      {"role": "assistant", "content": [
        {"type": "tool_use", "id": "tool_1", "name": "bash", "input": {"command": "cat .env"}}
      ]},
      {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "tool_1", "content": "DB_HOST=localhost\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\npassword = hunter2\nAPP_NAME=myapp"}
      ]}
    ]
  }' 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)

if [ "$HTTP_CODE" = "200" ]; then
  # Check what the mock upstream actually received
  FORWARDED=$(curl -s "http://localhost:${MOCK_PORT}/debug/last-request" 2>&1)
  if echo "$FORWARDED" | grep -q "REDACTED"; then
    echo "  ✓ HTTP 200 — Secrets were redacted before reaching upstream"
    echo ""
    echo "  What the upstream saw:"
    echo "$FORWARDED" | python3 -c "
import json, sys
req = json.load(sys.stdin)
for msg in req.get('messages', []):
    content = msg.get('content', '')
    if isinstance(content, list):
        for block in content:
            if block.get('type') == 'tool_result':
                print('    ' + block.get('content', ''))
" 2>/dev/null
  else
    echo "  ✗ Secrets were NOT redacted: $FORWARDED"
  fi
else
  BODY=$(echo "$RESPONSE" | sed '$d')
  echo "  ✗ HTTP $HTTP_CODE — $BODY"
fi
echo ""

# ── Show audit log ────────────────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Audit log:"
echo ""
if [ -f "$DEMO_DIR/audit.jsonl" ]; then
  python3 -c "
import json, sys
for line in open('$DEMO_DIR/audit.jsonl'):
    line = line.strip()
    if not line: continue
    try:
        e = json.loads(line)
        op = e.get('Operation', '?')
        d = e.get('Decision', '?')
        r = e.get('Rule', '')
        m = e.get('Message', '')
        icon = {'allow': '✓', 'deny': '✗', 'redact': '~'}.get(d, '?')
        out = f'  {icon} {d:6s} {op}'
        if r: out += f'  (rule: {r})'
        if m: out += f'  — {m}'
        print(out)
    except: pass
" 2>/dev/null
else
  echo "  (no audit log)"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Demo complete."
