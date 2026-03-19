#!/usr/bin/env bash
#
# Keep LLM Gateway Demo
#
# Demonstrates the keep-llm-gateway blocking a destructive bash command
# and redacting secrets from tool results — using a mock Anthropic
# upstream so no API key is needed.
#
# Usage:
#   ./examples/llm-gateway-demo/demo.sh
#
# What happens:
#   1. Builds the gateway and a mock Anthropic server
#   2. Creates policy rules (redact secrets, block rm -rf)
#   3. Starts both servers
#   4. Sends requests through the gateway with curl
#   5. Shows policy evaluation in the audit log
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
trap 'kill $MOCK_PID $GW_PID 2>/dev/null; rm -rf "$DEMO_DIR"' EXIT

echo "=== Keep LLM Gateway Demo ==="
echo ""

# ── Step 1: Build ──────────────────────────────────────────────────
echo "Building gateway and mock upstream..."
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway
go build -o "$DEMO_DIR/mock-anthropic" "$SCRIPT_DIR/mock-upstream"
echo "  ✓ Built"
echo ""

# ── Step 2: Create rules ──────────────────────────────────────────
mkdir -p "$DEMO_DIR/rules"
cat > "$DEMO_DIR/rules/gateway.yaml" << 'YAML'
scope: demo-gateway
mode: enforce

defs:
  destructive_commands: "['rm -rf', 'DROP TABLE', 'TRUNCATE', 'mkfs', 'dd if=']"

rules:
  # ── REQUEST DIRECTION: what the model sees ──

  # Redact AWS keys from tool results before they reach the model
  - name: redact-aws-keys
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "AKIA[0-9A-Z]{16}"
          replace: "[REDACTED:AWS_KEY]"

  # Redact generic secrets
  - name: redact-secrets
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      patterns:
        - match: "(?i)(password|secret|api_key|token)\\s*[=:]\\s*\\S+"
          replace: "[REDACTED:SECRET]"

  # ── RESPONSE DIRECTION: what the model wants to do ──

  # Block destructive bash commands
  - name: block-destructive-bash
    match:
      operation: "llm.tool_use"
      when: >
        params.name == 'bash'
        && containsAny(params.input.command, destructive_commands)
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
echo "  ✓ Rules created"

# ── Step 3: Create gateway config ─────────────────────────────────
cat > "$DEMO_DIR/gateway.yaml" << YAML
listen: ":18080"
rules_dir: "$DEMO_DIR/rules"
provider: anthropic
upstream: "http://localhost:18081"
scope: demo-gateway
log:
  format: json
  output: "$DEMO_DIR/audit.jsonl"
YAML
echo "  ✓ Gateway config created"
echo ""

# ── Step 4: Start servers ─────────────────────────────────────────
echo "Starting mock Anthropic upstream on :18081..."
"$DEMO_DIR/mock-anthropic" &
MOCK_PID=$!
sleep 0.5

echo "Starting keep-llm-gateway on :18080..."
"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" &
GW_PID=$!
sleep 1
echo ""

# ── Step 5: Test — Secret redaction ───────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 1: Secret redaction in tool results"
echo ""
echo "Sending a request with a tool_result containing AWS keys..."
echo "The gateway should redact the keys before forwarding to the model."
echo ""

RESPONSE=$(curl -s -X POST http://localhost:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: test-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Read the .env file"},
      {"role": "assistant", "content": [
        {"type": "tool_use", "id": "tool_1", "name": "bash", "input": {"command": "cat .env"}}
      ]},
      {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "tool_1", "content": "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nSECRET_KEY=wJalrXUtnFEMI/K7MDENG\npassword = hunter2"}
      ]}
    ]
  }')

echo "Response: $(echo "$RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$RESPONSE")"
echo ""

# Check what the mock upstream received
echo "What the model actually saw (received by upstream):"
UPSTREAM_LOG=$(curl -s http://localhost:18081/debug/last-request 2>/dev/null || echo "unavailable")
if [ "$UPSTREAM_LOG" != "unavailable" ]; then
  # Extract the tool_result content from the upstream request
  echo "$UPSTREAM_LOG" | python3 -c "
import json, sys
try:
    req = json.load(sys.stdin)
    for msg in req.get('messages', []):
        content = msg.get('content', '')
        if isinstance(content, list):
            for block in content:
                if block.get('type') == 'tool_result':
                    print('  tool_result content:', block.get('content', ''))
except: pass
" 2>/dev/null
fi
echo ""

# ── Step 6: Test — Destructive command blocked ────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 2: Blocking a destructive bash command"
echo ""
echo "Sending a request. The mock upstream will respond with 'rm -rf /'."
echo "The gateway should block the response before it reaches the agent."
echo ""

RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST http://localhost:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: test-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Delete all the old backup files"}
    ]
  }')

HTTP_STATUS=$(echo "$RESPONSE" | grep "HTTP_STATUS:" | cut -d: -f2)
BODY=$(echo "$RESPONSE" | grep -v "HTTP_STATUS:")

echo "HTTP Status: $HTTP_STATUS"
echo "Response: $(echo "$BODY" | python3 -m json.tool 2>/dev/null || echo "$BODY")"
echo ""

if [ "$HTTP_STATUS" = "400" ]; then
  echo "  ✓ BLOCKED — The gateway denied the destructive command."
else
  echo "  ✗ NOT BLOCKED — Expected HTTP 400, got $HTTP_STATUS"
fi
echo ""

# ── Step 7: Test — Normal request allowed ─────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Test 3: Normal request (no policy violations)"
echo ""

RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST http://localhost:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: test-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "What is 2 + 2?"}
    ]
  }')

HTTP_STATUS=$(echo "$RESPONSE" | grep "HTTP_STATUS:" | cut -d: -f2)
BODY=$(echo "$RESPONSE" | grep -v "HTTP_STATUS:")

echo "HTTP Status: $HTTP_STATUS"
if [ "$HTTP_STATUS" = "200" ]; then
  echo "  ✓ ALLOWED — Normal request passed through."
else
  echo "  ✗ UNEXPECTED — Expected HTTP 200, got $HTTP_STATUS"
fi
echo ""

# ── Step 8: Show audit log ────────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Audit log ($DEMO_DIR/audit.jsonl):"
echo ""
if [ -f "$DEMO_DIR/audit.jsonl" ]; then
  cat "$DEMO_DIR/audit.jsonl" | python3 -c "
import json, sys
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try:
        entry = json.loads(line)
        op = entry.get('Operation', '?')
        decision = entry.get('Decision', '?')
        rule = entry.get('Rule', '')
        msg = entry.get('Message', '')
        enforced = entry.get('Enforced', True)
        icon = {'allow': '✓', 'deny': '✗', 'redact': '~'}.get(decision, '?')
        line_out = f'  {icon} {decision:6s} {op}'
        if rule:
            line_out += f'  (rule: {rule})'
        if msg:
            line_out += f'  — {msg}'
        print(line_out)
    except json.JSONDecodeError:
        pass
" 2>/dev/null
else
  echo "  (no audit log found)"
fi
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Demo complete."
echo ""
echo "To use with a real Anthropic API:"
echo "  ANTHROPIC_API_KEY=sk-... keep-llm-gateway --config gateway.yaml"
echo "  ANTHROPIC_BASE_URL=http://localhost:8080 claude -p 'your prompt'"
