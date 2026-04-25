#!/usr/bin/env bash
#
# Keep Judge Demo — Vibe Check
#
# Runs the keep-llm-gateway with LLM-as-judge rules that screen messages
# for passive-aggression, hostility, and profanity.
#
# Requires: ANTHROPIC_API_KEY environment variable
#
# Usage:
#   ./examples/judge-demo/demo.sh
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
MAGENTA='\033[0;35m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR=$(mktemp -d)
GW_PORT=18081
GW_PID=""

# ── Prompts ──────────────────────────────────────────────────────

# Scenario 1: Polite request — should be allowed
PROMPT_1="Could you explain the difference between a mutex and a semaphore?"

# Scenario 2: Classic passive-aggressive — should be denied by vibe-check
PROMPT_2="Per my last email, I already explained this. Please advise."

# Scenario 3: Denial anger — should be denied by vibe-check
PROMPT_3="I'm not mad, I just think it's funny how nobody tested this before deploying to prod."

# Scenario 4: Weaponized smiley — should be denied by vibe-check
PROMPT_4="Friendly reminder that this was due last Thursday :)"

# Scenario 5: Profanity — should be denied by language-check (before vibe-check fires)
PROMPT_5="This damn API is such absolute crap, what idiot designed this?"

cleanup() {
  [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null || true
}
trap cleanup EXIT

# ── print_audit: display audit log entries with judge verdicts ───
print_audit() {
  local logfile="$1"
  if [ ! -f "$logfile" ]; then
    echo "  (no audit log)"
    return
  fi
  python3 -c "
import json, sys
dim = '\033[2m'
magenta = '\033[35m'
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
        # Print judge verdicts from rules evaluated
        for rule in e.get('RulesEvaluated', []):
            j = rule.get('judge')
            if not j: continue
            model = j.get('model', '?')
            verdict = j.get('verdict', '?')
            reason = j.get('reason', '')
            ms = j.get('latency_ms', 0)
            cached = j.get('cached', False)
            cache_tag = ' (cached)' if cached else ''
            vc = '\033[32m' if verdict == 'allow' else '\033[31m'
            if len(reason) > 100:
                reason = reason[:100] + '...'
            print(f'    {magenta}\u2728 {model}{reset}: {vc}{verdict}{reset} ({ms}ms){cache_tag} \u2014 {dim}{reason}{reset}')
    except: pass
" 2>/dev/null
}

echo ""
echo -e "${BOLD}${MAGENTA}Keep Judge Demo — Vibe Check${RESET}"
echo -e "${DIM}LLM-as-judge screening for tone and language${RESET}"
echo ""

# ── Preflight ────────────────────────────────────────────────────
if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo -e "${RED}Error:${RESET} ANTHROPIC_API_KEY environment variable is required."
  echo -e "  The judge needs an API key to evaluate message vibes."
  echo ""
  echo -e "  ${DIM}export ANTHROPIC_API_KEY=sk-ant-...${RESET}"
  exit 1
fi

# ── Build ────────────────────────────────────────────────────────
echo -e "${DIM}Building gateway...${RESET}"
go build -o "$DEMO_DIR/keep-llm-gateway" ./cmd/keep-llm-gateway

# ── Start gateway ────────────────────────────────────────────────
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

# ── run_scenario: helper to reduce repetition ────────────────────
run_scenario() {
  local num="$1"
  local title="$2"
  local description="$3"
  local prompt="$4"
  local expect_color="$5"  # GREEN or RED

  > "$DEMO_DIR/audit.jsonl"

  echo -e "${BOLD}Scenario ${num}:${RESET} ${title}"
  echo ""
  echo -e "  ${description}"
  echo ""
  echo -e "${BOLD}${BLUE}User prompt:${RESET}"
  echo ""
  echo -e "  ${DIM}>${RESET} ${prompt}"
  echo ""
  echo -e "${DIM}Sending via gateway...${RESET}"
  echo ""

  RESPONSE=$(claude -p "$prompt" \
    --model claude-haiku-4-5-20251001 \
    --max-turns 3 2>&1 || true)

  echo -e "${BOLD}${expect_color}Agent response:${RESET}"
  echo ""
  echo "$RESPONSE" | sed 's/^/  /'
  echo ""

  echo -e "${BOLD}Audit trail:${RESET}"
  echo ""
  print_audit "$DEMO_DIR/audit.jsonl"
  echo ""
}

# ══════════════════════════════════════════════════════════════════
# Scenario 1: Polite Request (should be allowed)
# ══════════════════════════════════════════════════════════════════

run_scenario 1 \
  "Polite request ${DIM}(good vibes)${RESET}" \
  "A polite, professional question. Both judges allow it through." \
  "$PROMPT_1" \
  "$GREEN"

# ══════════════════════════════════════════════════════════════════
# Scenario 2: Classic Passive-Aggressive
# ══════════════════════════════════════════════════════════════════

run_scenario 2 \
  "Classic passive-aggressive ${DIM}(bad vibes)${RESET}" \
  "The triple combo: \"per my last email\" + \"already explained\" +\n  \"please advise.\" The ${CYAN}vibe-check${RESET} judge catches the tone." \
  "$PROMPT_2" \
  "$RED"

# ══════════════════════════════════════════════════════════════════
# Scenario 3: Denial Anger
# ══════════════════════════════════════════════════════════════════

run_scenario 3 \
  "\"I'm not mad\" ${DIM}(definitely mad)${RESET}" \
  "Starting with \"I'm not mad\" is a reliable signal that someone\n  is, in fact, quite mad. The ${CYAN}vibe-check${RESET} judge sees through it." \
  "$PROMPT_3" \
  "$RED"

# ══════════════════════════════════════════════════════════════════
# Scenario 4: Weaponized Smiley
# ══════════════════════════════════════════════════════════════════

run_scenario 4 \
  "Weaponized smiley ${DIM}(passive-aggressive)${RESET}" \
  "\"Friendly reminder\" + deadline + smiley face = the most\n  passive-aggressive message format known to Slack." \
  "$PROMPT_4" \
  "$RED"

# ══════════════════════════════════════════════════════════════════
# Scenario 5: Profanity
# ══════════════════════════════════════════════════════════════════

run_scenario 5 \
  "Profanity ${DIM}(language-check short-circuits)${RESET}" \
  "This one doesn't even make it to the vibe check. The\n  ${CYAN}language-check${RESET} judge catches the profanity first\n  and deny short-circuits." \
  "$PROMPT_5" \
  "$RED"

# ── Done ─────────────────────────────────────────────────────────
echo -e "${DIM}Debug log: $KEEP_DEBUG${RESET}"
echo ""
