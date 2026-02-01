#!/usr/bin/env python3
"""
Claude Code PreToolUse hook for validating RCC commands.

Install: Copy to your project and configure in .claude/settings.json:
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{"type": "command", "command": "python3 .claude/hooks/validate-rcc-commands.py"}]
      }
    ]
  }
}

Exit codes:
  0 - Command allowed
  2 - Command blocked (stderr shown to Claude)
"""

import json
import sys
import re

# Dangerous command patterns to block
DANGEROUS_PATTERNS = [
    r'rcc\s+holotree\s+delete\s+--all',  # Deleting all environments
    r'rm\s+-rf\s+.*\.robocorp',           # Deleting robocorp home
    r'rcc\s+cloud\s+push.*--force',       # Force push to cloud
]

# Safe RCC commands (informational)
SAFE_COMMANDS = [
    r'^rcc\s+run',
    r'^rcc\s+robot\s+init',
    r'^rcc\s+ht\s+vars',
    r'^rcc\s+holotree\s+variables',
    r'^rcc\s+task\s+shell',
    r'^rcc\s+task\s+script',
    r'^rcc\s+configure\s+diagnostics',
    r'^rcc\s+holotree\s+list',
    r'^rcc\s+robot\s+dependencies',
    r'^rcc\s+docs',
]


def main():
    try:
        input_data = json.load(sys.stdin)
        command = input_data.get('tool_input', {}).get('command', '')

        # Check for dangerous patterns
        for pattern in DANGEROUS_PATTERNS:
            if re.search(pattern, command, re.IGNORECASE):
                print(f"Blocked: Potentially destructive command matches '{pattern}'", file=sys.stderr)
                sys.exit(2)

        # All checks passed
        sys.exit(0)

    except Exception as e:
        # Don't block on hook errors, just warn
        print(f"Hook warning: {e}", file=sys.stderr)
        sys.exit(0)


if __name__ == "__main__":
    main()
