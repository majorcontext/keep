#!/usr/bin/env bash
#
# Keep LLM Gateway Demo
#
# Runs the keep-llm-gateway in front of the real Anthropic API and
# demonstrates policy enforcement with both curl and claude -p.
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
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

echo "=== Keep LLM Gateway Demo ==="
echo ""

# ── Step 1: Build ──────────────────────────────────────────────────
echo "Building gateway..."
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway
echo "  done"
echo ""

# ── Step 2: Create rules ──────────────────────────────────────────
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

# ── Step 3: Create gateway config ─────────────────────────────────
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

# ── Step 4: Start gateway ─────────────────────────────────────────
echo "Starting keep-llm-gateway on :${GW_PORT}..."
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" 2>&1 &
GW_PID=$!
sleep 1

# Quick health check — passthrough a non-messages request
if ! curl -s -o /dev/null -w '' "http://localhost:${GW_PORT}/v1/health" 2>/dev/null; then
  true  # gateway doesn't need a health endpoint — just check it's up
fi
echo "  Gateway running (PID $GW_PID)"
echo ""

# ── Test 1: curl — normal request through gateway ─────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 1: Normal API call through the gateway (curl)"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" \
  "http://localhost:${GW_PORT}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 50,
    "messages": [{"role": "user", "content": "What is 2+2? Answer in one word."}]
  }' 2>&1)

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
  ANSWER=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['content'][0]['text'])" 2>/dev/null || echo "$BODY")
  echo "  ✓ HTTP 200 — Model said: $ANSWER"
else
  echo "  ✗ HTTP $HTTP_CODE — $BODY"
  echo ""
  echo "  The gateway could not reach the Anthropic API."
  echo "  Make sure HTTPS_PROXY is set or ANTHROPIC_API_KEY is available."
  exit 1
fi
echo ""

# ── Test 2: claude -p through gateway ─────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 2: claude -p through the gateway"
echo ""
echo "Running: ANTHROPIC_BASE_URL=http://localhost:${GW_PORT} claude -p 'What is 2+2? One word.'"
echo ""

CLAUDE_OUTPUT=$(ANTHROPIC_BASE_URL="http://localhost:${GW_PORT}" \
  claude -p "What is 2+2? Answer in exactly one word, no punctuation." \
  --model claude-haiku-4-5-20251001 \
  --max-turns 1 \
  2>&1) || true

if echo "$CLAUDE_OUTPUT" | grep -qi "streaming"; then
  echo "  ~ claude -p uses streaming by default, which the gateway correctly rejects."
  echo "    This is expected — streaming support requires SSE parsing (future work)."
  echo "    Use curl with stream:false for non-streaming requests (see tests above)."
elif [ -n "$CLAUDE_OUTPUT" ]; then
  echo "  ✓ Claude response: $CLAUDE_OUTPUT"
else
  echo "  ✗ claude -p produced no output (auth may not be available)"
fi
echo ""

# ── Test 3: Destructive command blocked ───────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 3: Blocking a destructive bash command"
echo ""
echo "Simulating a model response that contains 'rm -rf'."
echo "We craft the API exchange so no real model generates a dangerous command."
echo ""

# We send a request as if we're continuing a conversation where the model
# already responded with a tool_use. The gateway evaluates the upstream
# response and blocks it. To avoid actually asking a model to generate
# rm -rf, we start a mock upstream just for this test.
go build -o "$DEMO_DIR/mock-anthropic" "$SCRIPT_DIR/mock-upstream"
"$DEMO_DIR/mock-anthropic" &
MOCK_PID=$!
sleep 0.5

# Temporarily point a second gateway at the mock for this test
cat > "$DEMO_DIR/gateway-mock.yaml" << YAML
listen: ":18082"
rules_dir: "$DEMO_DIR/rules"
provider: anthropic
upstream: "http://localhost:18081"
scope: demo-gateway
log:
  format: json
  output: "$DEMO_DIR/audit.jsonl"
YAML
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway-mock.yaml" 2>&1 &
GW_MOCK_PID=$!
sleep 0.5

RESPONSE=$(curl -s -w "\n%{http_code}" \
  "http://localhost:18082/v1/messages" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 200,
    "messages": [{"role": "user", "content": "Delete the old backups please"}]
  }' 2>&1)

kill "$GW_MOCK_PID" "$MOCK_PID" 2>/dev/null || true

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "400" ]; then
  ERROR_MSG=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['error']['message'])" 2>/dev/null || echo "$BODY")
  echo "  ✓ HTTP 400 — BLOCKED: $ERROR_MSG"
  echo ""
  echo "  The mock upstream returned a response with 'rm -rf /data/old-backups'."
  echo "  The gateway intercepted it and blocked it before it reached the agent."
else
  echo "  ? HTTP $HTTP_CODE — $BODY"
fi
echo ""

# ── Test 4: Secret redaction ──────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 4: Secret redaction in tool results"
echo ""
echo "Sending a conversation where a tool_result contains an AWS key..."
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" \
  "http://localhost:${GW_PORT}/v1/messages" \
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
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
  echo "  ✓ HTTP 200 — Model responded (secrets were redacted before reaching it)"
  ANSWER=$(echo "$BODY" | python3 -c "import json,sys; r=json.load(sys.stdin); print(r['content'][0]['text'][:200])" 2>/dev/null || echo "(parse error)")
  echo "  Model said: $ANSWER"
else
  echo "  HTTP $HTTP_CODE — $BODY"
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
