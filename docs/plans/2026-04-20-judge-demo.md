# Judge Demo (Vibe Check) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an interactive `examples/judge-demo/` that demonstrates LLM-as-judge with a "vibe check" theme — screening messages for passive-aggression, hostility, and profanity.

**Architecture:** A gateway demo (same pattern as `examples/llm-gateway-demo/`) that starts the gateway with two judge rules (`vibe-check` and `language-check`), sends 5 prompts through it, and prints colorized audit output including judge verdicts and reasoning. Requires `ANTHROPIC_API_KEY` for live judge calls.

**Tech Stack:** Bash, YAML rule files, Go (gateway binary), Python 3 (audit pretty-printer)

---

## File Structure

```
examples/judge-demo/
  demo.sh              # Main demo script (5 scenarios)
  gateway.yaml         # Gateway config with judge provider
  rules/
    vibes.yaml         # Two judge rules + audit-all log rule
```

No Go code to write — this is pure configuration and shell scripting. The gateway binary and judge providers already exist.

## Dependencies

- `examples/llm-gateway-demo/` — pattern to follow for script structure, colors, audit printing
- `cmd/keep-llm-gateway/main.go` — already wires judge provider from `gateway.yaml`
- `judge/anthropic/` — shipped provider used for judge calls
- `ANTHROPIC_API_KEY` env var — required at runtime for both the proxy and the judge

---

### Task 1: Rule File and Gateway Config

**Files:**
- Create: `examples/judge-demo/rules/vibes.yaml`
- Create: `examples/judge-demo/gateway.yaml`

- [ ] **Step 1: Create the rules file**

```yaml
# examples/judge-demo/rules/vibes.yaml
scope: vibe-check
mode: enforce

rules:
  # Screen for profanity and vulgar language
  - name: language-check
    match:
      operation: "llm.text"
      when: 'context.direction == "request" && params.role == "user"'
    action: judge
    judge:
      model: haiku
      prompt: "Does this message contain profanity, curse words, or vulgar language? Respond with deny if yes, allow if no."
      timeout: 5s
      on_error: closed

  # Screen for passive-aggression, hostility, unprofessional tone
  - name: vibe-check
    match:
      operation: "llm.text"
      when: 'context.direction == "request" && params.role == "user"'
    action: judge
    judge:
      model: haiku
      prompt: "Is this message professional and appropriate in tone? Respond with deny if the message is passive-aggressive, hostile, sarcastic, or unprofessional. Allow if it is neutral or polite."
      timeout: 5s
      on_error: closed

  # Log everything for the audit trail
  - name: audit-all
    match:
      operation: "llm.*"
    action: log
```

Note: `language-check` comes first so profanity is caught before tone analysis. Deny short-circuits, so if language-check denies, vibe-check never fires.

- [ ] **Step 2: Create the gateway config**

```yaml
# examples/judge-demo/gateway.yaml
# The demo script substitutes RULES_DIR and LOG_OUTPUT at runtime.
listen: ":18081"
rules_dir: "RULES_DIR"
provider: anthropic
upstream: "https://api.anthropic.com"
scope: vibe-check
decompose:
  text: true
  tool_result: true
  tool_use: true
judge:
  provider: anthropic
  api_key_env: ANTHROPIC_API_KEY
log:
  format: json
  output: "LOG_OUTPUT"
```

Uses port 18081 to avoid conflict with the other gateway demo on 18080.

- [ ] **Step 3: Validate the rules parse correctly**

Run: `go run ./cmd/keep validate examples/judge-demo/rules/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add examples/judge-demo/rules/vibes.yaml examples/judge-demo/gateway.yaml
git commit -m "feat(examples): add judge demo rule file and gateway config"
```

---

### Task 2: Demo Script

**Files:**
- Create: `examples/judge-demo/demo.sh`

The script follows the exact same structure as `examples/llm-gateway-demo/demo.sh`: colors, build, start gateway, run scenarios, print audit, cleanup. The key addition is rendering judge verdict details in the audit output.

- [ ] **Step 1: Create the demo script**

```bash
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
PROMPT_1="Could you help me understand how the caching layer works? I'm new to this part of the codebase."

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
            vc = '\033[32m' if verdict == 'allow' else '\033[31m'
            if len(reason) > 100:
                reason = reason[:100] + '...'
            print(f'    {magenta}\u2728 {model}{reset}: {vc}{verdict}{reset} ({ms}ms) \u2014 {dim}{reason}{reset}')
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
    --max-turns 1 2>&1 || true)

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
  "A genuine, professional question. Both judges allow it through." \
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
```

- [ ] **Step 2: Make the script executable**

Run: `chmod +x examples/judge-demo/demo.sh`

- [ ] **Step 3: Verify the gateway builds and starts**

Run: `go build -o /tmp/keep-gw-test ./cmd/keep-llm-gateway && echo "OK"`
Expected: OK (binary builds without errors)

- [ ] **Step 4: Commit**

```bash
git add examples/judge-demo/demo.sh
git commit -m "feat(examples): add vibe-check judge demo script"
```

---

## Notes

- The `language-check` rule is ordered before `vibe-check` in the YAML. Deny short-circuits, so scenario 5 demonstrates that profanity is caught by the first rule without the second ever firing.
- The demo uses `claude` CLI (Claude Code) as the client, same as the existing gateway demo. This means the user prompt goes through the gateway proxy, where it's decomposed into `llm.text` spans, and each text span hits the judge rules.
- The `print_audit` function renders judge verdicts inline under each audit entry, showing model, verdict, latency, and the LLM's reasoning — which is the entertaining part.
- Port 18081 avoids conflict with the existing gateway demo on 18080.
- `--max-turns 1` prevents follow-up turns on denied requests.
