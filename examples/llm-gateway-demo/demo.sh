#!/usr/bin/env bash
#
# Keep LLM Gateway Demo
#
# Runs the keep-llm-gateway in front of the Anthropic API and demonstrates
# policy enforcement. Auth is handled by claude, which picks up credentials
# from whatever source you have configured (OAuth, API key, etc.).
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

export ANTHROPIC_BASE_URL="http://localhost:${GW_PORT}"

# ── Test: Streaming + redaction ───────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Streaming through the gateway with secret redaction"
echo ""
echo "  The prompt contains a fake API key. The gateway redacts it"
echo "  before it reaches the model, so Claude never sees the secret."
echo ""
echo "  \$ ANTHROPIC_BASE_URL=$ANTHROPIC_BASE_URL claude -p '...'"
echo ""

claude -p "Use my API key: sk-ant-FAKE-1234567890abcdef to call the weather API for SF. What is the key I just gave you?" \
  --model claude-haiku-4-5-20251001 \
  --max-turns 1 2>&1 || true

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
