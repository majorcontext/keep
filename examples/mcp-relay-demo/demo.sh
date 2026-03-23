#!/usr/bin/env bash
#
# Keep MCP Relay Demo
#
# Runs the keep-mcp-relay in front of a sqlite MCP server and demonstrates
# policy enforcement: read-only mode and password redaction.
#
# Usage:
#   ./examples/mcp-relay-demo/demo.sh
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
RELAY_PORT=19090
RELAY_PID=""

cleanup() {
  echo ""
  echo -e "${DIM}Cleaning up...${RESET}"
  [ -n "$RELAY_PID" ] && kill "$RELAY_PID" 2>/dev/null || true
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

echo ""
echo -e "${BOLD}Keep MCP Relay Demo${RESET}"
echo -e "${DIM}Policy enforcement for MCP tool calls${RESET}"
echo ""

# ── Prerequisites ────────────────────────────────────────────────
if ! command -v sqlite3 &>/dev/null; then
  echo -e "${RED}Error:${RESET} sqlite3 is required but not found."
  echo "  Install it with: brew install sqlite3  (macOS) or apt install sqlite3  (Linux)"
  exit 1
fi

MCP_RUNNER=""
if command -v npx &>/dev/null; then
  MCP_RUNNER="npx"
elif command -v bunx &>/dev/null; then
  MCP_RUNNER="bunx"
else
  echo -e "${RED}Error:${RESET} npx or bunx is required but neither was found."
  echo "  Install Node.js (https://nodejs.org) or Bun (https://bun.sh)"
  exit 1
fi

echo -e "${DIM}Using ${MCP_RUNNER} to run MCP sqlite server${RESET}"

# ── Build ────────────────────────────────────────────────────────
echo -e "${DIM}Building relay...${RESET}"
go build -o "$DEMO_DIR/keep-mcp-relay" ./cmd/keep-mcp-relay

# ── Seed database ───────────────────────────────────────────────
DB_PATH="$DEMO_DIR/demo.db"
sqlite3 "$DB_PATH" < "$SCRIPT_DIR/seed.sql"
echo -e "${DIM}Seeded database with 12 users${RESET}"

# ── Generate relay config ───────────────────────────────────────
sed \
  -e "s|RULES_DIR|$SCRIPT_DIR/rules|" \
  -e "s|LOG_OUTPUT|$DEMO_DIR/audit.jsonl|" \
  -e "s|COMMAND|$MCP_RUNNER|" \
  -e "s|ARGS|[\"-y\", \"@modelcontextprotocol/server-sqlite\", \"$DB_PATH\"]|" \
  "$SCRIPT_DIR/relay.yaml" > "$DEMO_DIR/relay.yaml"

# ── Start relay ─────────────────────────────────────────────────
export KEEP_DEBUG="$DEMO_DIR/debug.log"

if [ -n "${KEEP_VERBOSE:-}" ]; then
  "$DEMO_DIR/keep-mcp-relay" --config "$DEMO_DIR/relay.yaml" >/dev/null &
else
  "$DEMO_DIR/keep-mcp-relay" --config "$DEMO_DIR/relay.yaml" >/dev/null 2>&1 &
fi
RELAY_PID=$!
sleep 2

if ! kill -0 "$RELAY_PID" 2>/dev/null; then
  echo -e "${RED}Error:${RESET} Relay failed to start. Check debug log: $KEEP_DEBUG"
  exit 1
fi

echo -e "${GREEN}Relay running${RESET} on :${RELAY_PORT} ${DIM}(PID $RELAY_PID)${RESET}"
echo ""

# ── Instructions ─────────────────────────────────────────────────
echo -e "${BOLD}Connect Claude to the relay:${RESET}"
echo ""
echo -e "  ${CYAN}claude mcp add keep-demo --transport http http://localhost:${RELAY_PORT}${RESET}"
echo ""
echo -e "${BOLD}Try these prompts:${RESET}"
echo ""
echo -e "  ${BLUE}1.${RESET} \"List all users in the database\""
echo -e "     ${DIM}→ passwords will be redacted from the response${RESET}"
echo ""
echo -e "  ${BLUE}2.${RESET} \"Add a new user named Test User\""
echo -e "     ${DIM}→ write operation will be denied${RESET}"
echo ""
echo -e "${BOLD}Audit log:${RESET} ${DIM}$DEMO_DIR/audit.jsonl${RESET}"
echo -e "${BOLD}Debug log:${RESET} ${DIM}$KEEP_DEBUG${RESET}"
echo ""
echo -e "${DIM}Press Ctrl+C to stop the relay${RESET}"

# ── Wait ─────────────────────────────────────────────────────────
wait "$RELAY_PID"
