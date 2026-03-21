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

# ── Colors ───────────────────────────────────────────────────────
BOLD='\033[1m'
DIM='\033[2m'
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
GW_PORT=18080
GW_PID=""

PROMPT="Write me a curl command to call the OpenWeather API for San Francisco using api_key=sk-ant-1234567890abcdef"

cleanup() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null || true
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

echo ""
echo -e "${BOLD}Keep LLM Gateway Demo${RESET}"
echo -e "${DIM}Policy enforcement for AI agent traffic${RESET}"
echo ""

# ── Build ─────────────────────────────────────────────────────────
echo -e "${DIM}Building gateway...${RESET}"
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway

# ── Start gateway ─────────────────────────────────────────────────
sed \
  -e "s|RULES_DIR|$SCRIPT_DIR/rules|" \
  -e "s|LOG_OUTPUT|$DEMO_DIR/audit.jsonl|" \
  "$SCRIPT_DIR/gateway.yaml" > "$DEMO_DIR/gateway.yaml"

export KEEP_DEBUG="$DEMO_DIR/debug.log"

"$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" >/dev/null 2>&1 &
GW_PID=$!
sleep 1

echo -e "${GREEN}Gateway running${RESET} on :${GW_PORT} ${DIM}(PID $GW_PID)${RESET}"
echo ""

export ANTHROPIC_BASE_URL="http://localhost:${GW_PORT}"

# ── Scenario ─────────────────────────────────────────────────────
echo -e "${BOLD}Scenario:${RESET} Secret redaction in streaming mode"
echo ""
echo -e "  The user prompt contains a fake API key. The gateway's"
echo -e "  ${CYAN}redact-secrets-in-text${RESET} rule strips it before it reaches"
echo -e "  the model, so Claude never sees the secret."
echo ""

# Show the prompt
echo -e "${BOLD}${BLUE}User prompt:${RESET}"
echo ""
echo -e "  ${DIM}>${RESET} $PROMPT"
echo ""

# Run claude
echo -e "${DIM}Sending via gateway (streaming)...${RESET}"
echo ""
RESPONSE=$(claude -p "$PROMPT" \
  --model claude-haiku-4-5-20251001 \
  --max-turns 1 2>&1 || true)

echo -e "${BOLD}${GREEN}Agent response:${RESET}"
echo ""
echo "$RESPONSE" | sed 's/^/  /'
echo ""

# ── Audit log ────────────────────────────────────────────────────
echo -e "${BOLD}Audit trail:${RESET}"
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
        colors = {'allow': '\033[32m', 'deny': '\033[31m', 'redact': '\033[33m'}
        icon = {'allow': '\u2713', 'deny': '\u2717', 'redact': '\u2192'}.get(d, '?')
        c = colors.get(d, '')
        reset = '\033[0m'
        out = f'  {c}{icon} {d:6s}{reset} {op}'
        if r: out += f'  \033[2m({r})\033[0m'
        if m: out += f'  \033[2m\u2014 {m}\033[0m'
        print(out)
    except: pass
" 2>/dev/null
else
  echo "  (no audit log)"
fi

echo ""
echo -e "${DIM}Debug log: $KEEP_DEBUG${RESET}"
echo ""
