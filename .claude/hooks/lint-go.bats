#!/usr/bin/env bats

setup() {
  HOOK="$BATS_TEST_DIRNAME/lint-go.py"
}

# Helper: pipe JSON to hook, capture exit code
run_hook() {
  local output
  output="$(echo "$1" | "$HOOK" 2>&1)" && status=0 || status=$?
  echo "$output"
  return "$status"
}

# --- Should skip ---

@test "skips non-commit commands" {
  result="$(run_hook '{"tool_name":"Bash","tool_input":{"command":"echo hello"}}')" || status=$?
  [ "${status:-0}" -eq 0 ]
}

@test "skips non-Bash tools" {
  result="$(run_hook '{"tool_name":"Write","tool_input":{"command":"git commit -m test"}}')" || status=$?
  [ "${status:-0}" -eq 0 ]
}

@test "skips git commands that are not commit" {
  result="$(run_hook '{"tool_name":"Bash","tool_input":{"command":"git status"}}')" || status=$?
  [ "${status:-0}" -eq 0 ]
}

# --- Should trigger ---

@test "runs lint on git commit" {
  # This test depends on the current repo state being lint-clean.
  # It verifies the hook runs and exits 0 (no blocking output).
  result="$(run_hook '{"tool_name":"Bash","tool_input":{"command":"git commit -m \"test\""}}')" || status=$?
  [ "${status:-0}" -eq 0 ]
}
