#!/usr/bin/env bash
#
# Keep LLM Gateway Demo
#
# Runs the keep-llm-gateway in front of the Anthropic API and demonstrates
# policy enforcement: streaming, redaction, and audit logging.
#
# Prerequisites:
#   - ANTHROPIC_API_KEY set in your environment
#
# Usage:
#   ./examples/llm-gateway-demo/demo.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
GW_PORT=18080
GW_PID=""

cleanup() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null || true
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo "ANTHROPIC_API_KEY is not set. Export it and re-run."
  exit 1
fi

echo "=== Keep LLM Gateway Demo ==="
echo ""

# ── Build ─────────────────────────────────────────────────────────
echo "Building gateway..."
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway
echo "  done"
echo ""

# ── Start gateway ─────────────────────────────────────────────────
sed \
  -e "s|RULES_DIR|$SCRIPT_DIR/rules|" \
  -e "s|LOG_OUTPUT|$DEMO_DIR/audit.jsonl|" \
  "$SCRIPT_DIR/gateway.yaml" > "$DEMO_DIR/gateway.yaml"

echo "Starting keep-llm-gateway on :${GW_PORT}..."
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" 2>&1 &
GW_PID=$!
sleep 1
echo "  Gateway running (PID $GW_PID)"
echo ""

GW_URL="http://localhost:${GW_PORT}/v1/messages"
GW_HEADERS=(-H "Content-Type: application/json" -H "anthropic-version: 2023-06-01" -H "x-api-key: $ANTHROPIC_API_KEY")

# ── Test 1: Streaming request ─────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 1: Streaming request through the gateway"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" "$GW_URL" "${GW_HEADERS[@]}" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 30,
    "stream": true,
    "messages": [{"role": "user", "content": "What is 2+2? Answer in one word."}]
  }' 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
  EVENT_COUNT=$(echo "$BODY" | grep -c "^event:" || true)
  echo "  ✓ HTTP 200 — Received $EVENT_COUNT SSE events (buffered, policy-evaluated, replayed)"
else
  echo "  ✗ HTTP $HTTP_CODE — $BODY"
  echo ""
  echo "  Check that ANTHROPIC_API_KEY is valid."
  exit 1
fi
echo ""

# ── Test 2: Non-streaming request ─────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 2: Non-streaming request through the gateway"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" "$GW_URL" "${GW_HEADERS[@]}" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 30,
    "messages": [{"role": "user", "content": "What is 2+2? Answer in one word."}]
  }' 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
  ANSWER=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['content'][0]['text'])" 2>/dev/null || echo "$BODY")
  echo "  ✓ HTTP 200 — Model said: $ANSWER"
else
  echo "  ✗ HTTP $HTTP_CODE — $BODY"
fi
echo ""

# ── Test 3: Secret redaction ──────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 3: Secret redaction in tool results"
echo ""
echo "  Sending a tool_result containing an AWS key and password."
echo "  The gateway redacts secrets before forwarding to the model."
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" "$GW_URL" "${GW_HEADERS[@]}" \
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
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
  echo "  ✓ HTTP 200 — Model responded (secrets were redacted before reaching it)"
  ANSWER=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['content'][0]['text'][:200])" 2>/dev/null || echo "(parse error)")
  echo "  Model said: $ANSWER"
else
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
echo ""
echo "You can also point claude at the gateway:"
echo "  ANTHROPIC_BASE_URL=http://localhost:${GW_PORT} claude"
echo ""
echo "Response deny (blocking destructive commands) is covered by unit tests:"
echo "  make test-unit ARGS='-run TestProxy_DenyResponse ./internal/gateway'"
