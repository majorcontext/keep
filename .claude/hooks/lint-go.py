#!/usr/bin/env python3
"""Claude Code PreToolUse hook: runs golangci-lint before git commit."""

import json
import re
import sys
import subprocess


def main():
    data = json.load(sys.stdin)

    if data.get("tool_name") != "Bash":
        sys.exit(0)

    command = data.get("tool_input", {}).get("command", "")
    if not command:
        sys.exit(0)

    # Only trigger on git commit commands
    if not re.search(r"\bgit\s+commit\b", command):
        sys.exit(0)

    result = subprocess.run(
        ["golangci-lint", "run", "./..."],
        capture_output=True,
        text=True,
        timeout=120,
    )

    if result.returncode != 0:
        output = json.dumps({
            "decision": "block",
            "reason": f"golangci-lint failed. Fix lint errors before committing:\n{result.stdout}{result.stderr}",
        })
        print(output)
        sys.exit(0)


if __name__ == "__main__":
    main()
