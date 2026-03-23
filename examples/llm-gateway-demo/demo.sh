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

# ── Prompts ──────────────────────────────────────────────────────

# Scenario 1: Secret in prompt — should be redacted, model still answers
PROMPT_1="Write me a curl command to call the OpenWeather API for San Francisco using api_key=AKIAIOSFODNN7REALKEY"

# Scenario 2: PII in prompt — should be hard denied (operator mistake)
PROMPT_2="Summarize this customer complaint: From: jane.doe@acmecorp.com, Subject: Billing error on account #4821. Dear support, my card ending in 4242 was charged twice for order #8891."

# Scenario 3: Model generates a network tool call — should be blocked (model mistake)
PROMPT_3="Use curl to check if api.github.com is responding."

cleanup() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null || true
}
trap cleanup EXIT

# ── print_audit: display audit log entries ────────────────────────
print_audit() {
  local logfile="$1"
  if [ ! -f "$logfile" ]; then
    echo "  (no audit log)"
    return
  fi
  python3 -c "
import json, sys
dim = '\033[2m'
reset = '\033[0m'
for line in open('$1'):
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
        out = f'  {c}{icon} {d:6s}{reset} {op}'
        if r: out += f'  {dim}({r}){reset}'
        if m: out += f'  {dim}\u2014 {m}{reset}'
        print(out)
        for rf in e.get('RedactSummary', []):
            replaced = rf.get('Replaced', '')
            path = rf.get('Path', '')
            if len(replaced) > 120:
                replaced = replaced[:120] + '...'
            print(f'    {dim}{path}: {replaced}{reset}')
    except: pass
" 2>/dev/null
}

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

if [ -n "${KEEP_VERBOSE:-}" ]; then
  "$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" >/dev/null &
else
  "$DEMO_DIR/keep-llm-gateway" --config "$DEMO_DIR/gateway.yaml" >/dev/null 2>&1 &
fi
GW_PID=$!
sleep 1

echo -e "${GREEN}Gateway running${RESET} on :${GW_PORT} ${DIM}(PID $GW_PID)${RESET}"
echo ""

export ANTHROPIC_BASE_URL="http://localhost:${GW_PORT}"

# ══════════════════════════════════════════════════════════════════
# Scenario 1: Secret Redaction
# ══════════════════════════════════════════════════════════════════

echo -e "${BOLD}Scenario 1:${RESET} Secret redaction"
echo ""
echo -e "  A developer accidentally pastes an AWS access key into their"
echo -e "  prompt. The ${CYAN}redact-secrets-in-text${RESET} rule detects it with"
echo -e "  gitleaks and replaces it with a placeholder before the prompt"
echo -e "  reaches the model. The agent still answers — it just never"
echo -e "  sees the real key."
echo ""

echo -e "${BOLD}${BLUE}User prompt:${RESET}"
echo ""
echo -e "  ${DIM}>${RESET} $PROMPT_1"
echo ""

echo -e "${DIM}Sending via gateway...${RESET}"
echo ""
RESPONSE_1=$(claude -p "$PROMPT_1" \
  --model claude-haiku-4-5-20251001 \
  --max-turns 3 2>&1 || true)

echo -e "${BOLD}${GREEN}Agent response:${RESET}"
echo ""
echo "$RESPONSE_1" | sed 's/^/  /'
echo ""

cp "$DEMO_DIR/audit.jsonl" "$DEMO_DIR/audit-scenario1.jsonl" 2>/dev/null || true

echo -e "${BOLD}Audit trail:${RESET}"
echo ""
print_audit "$DEMO_DIR/audit-scenario1.jsonl"
echo ""

# ══════════════════════════════════════════════════════════════════
# Scenario 2: PII Blocked (operator mistake)
# ══════════════════════════════════════════════════════════════════

> "$DEMO_DIR/audit.jsonl"

echo -e "${BOLD}Scenario 2:${RESET} PII blocked ${DIM}(operator mistake)${RESET}"
echo ""
echo -e "  An operator copy-pastes a customer support ticket directly"
echo -e "  into the agent prompt — including the customer's email address."
echo -e "  The ${CYAN}block-pii-in-prompts${RESET} rule detects the PII and denies"
echo -e "  the request before it reaches the model."
echo ""

echo -e "${BOLD}${BLUE}User prompt:${RESET}"
echo ""
echo -e "  ${DIM}>${RESET} $PROMPT_2"
echo ""

echo -e "${DIM}Sending via gateway...${RESET}"
echo ""
RESPONSE_2=$(claude -p "$PROMPT_2" \
  --model claude-haiku-4-5-20251001 \
  --max-turns 3 2>&1 || true)

echo -e "${BOLD}${RED}Agent response:${RESET}"
echo ""
echo "$RESPONSE_2" | sed 's/^/  /'
echo ""

echo -e "${BOLD}Audit trail:${RESET}"
echo ""
print_audit "$DEMO_DIR/audit.jsonl"
echo ""

# ══════════════════════════════════════════════════════════════════
# Scenario 3: Tool Use Blocked (model mistake)
# ══════════════════════════════════════════════════════════════════

> "$DEMO_DIR/audit.jsonl"

echo -e "${BOLD}Scenario 3:${RESET} Tool use blocked ${DIM}(model mistake)${RESET}"
echo ""
echo -e "  The user asks the agent to check an API. The model generates"
echo -e "  a ${CYAN}curl${RESET} command as a tool call. The gateway's"
echo -e "  ${CYAN}block-networking${RESET} rule catches the tool call in the model's"
echo -e "  response and blocks it before execution."
echo ""

echo -e "${BOLD}${BLUE}User prompt:${RESET}"
echo ""
echo -e "  ${DIM}>${RESET} $PROMPT_3"
echo ""

echo -e "${DIM}Sending via gateway...${RESET}"
echo ""
RESPONSE_3=$(claude -p "$PROMPT_3" \
  --model claude-haiku-4-5-20251001 \
  --max-turns 3 \
  --dangerously-skip-permissions 2>&1 || true)

echo -e "${BOLD}${RED}Agent response:${RESET}"
echo ""
echo "$RESPONSE_3" | sed 's/^/  /'
echo ""

echo -e "${BOLD}Audit trail:${RESET}"
echo ""
print_audit "$DEMO_DIR/audit.jsonl"
echo ""

echo -e "${DIM}Debug log: $KEEP_DEBUG${RESET}"
echo ""
