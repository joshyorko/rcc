# Claude Code Hooks for RCC Projects

Integrate RCC with Claude Code hooks for safer, more intelligent automation.

## Design Principles

All hooks in this skill are **dependency-free** - they work on any Linux/macOS system with just:
- `bash` (or any POSIX shell)
- Standard POSIX utilities: `grep`, `sed`, `cat`, `head`
- `python3` (for Python hooks - available in RCC projects)

**No jq, no curl, no external dependencies required.**

## Quick Setup

Copy this configuration to `.claude/settings.json` in your project:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "python3 .claude/hooks/validate-rcc-commands.py",
            "timeout": 5
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/post-rcc-run.sh"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/session-setup.sh"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(rcc run *)",
      "Bash(rcc robot init *)",
      "Bash(rcc ht vars *)",
      "Bash(rcc holotree variables *)",
      "Bash(rcc task shell *)",
      "Bash(rcc task script *)",
      "Bash(rcc configure diagnostics *)",
      "Bash(rcc holotree list)",
      "Bash(rcc robot dependencies *)",
      "Bash(rcc docs *)"
    ],
    "deny": [
      "Bash(rcc holotree delete --all)",
      "Bash(rm -rf *robocorp*)"
    ]
  },
  "env": {
    "RCC_VERBOSITY": "silent"
  }
}
```

## Hook Scripts

### PreToolUse: validate-rcc-commands.py

Validates RCC commands before execution, blocking dangerous operations:

```python
#!/usr/bin/env python3
import json
import sys
import re

DANGEROUS_PATTERNS = [
    r'rcc\s+holotree\s+delete\s+--all',
    r'rm\s+-rf\s+.*\.robocorp',
    r'rcc\s+cloud\s+push.*--force',
]

def main():
    input_data = json.load(sys.stdin)
    command = input_data.get('tool_input', {}).get('command', '')

    for pattern in DANGEROUS_PATTERNS:
        if re.search(pattern, command, re.IGNORECASE):
            print(f"Blocked: {pattern}", file=sys.stderr)
            sys.exit(2)

    sys.exit(0)

if __name__ == "__main__":
    main()
```

### PostToolUse: post-rcc-run.sh

Provides helpful context after RCC commands. **Dependency-free** - uses only POSIX tools (no jq):

```bash
#!/bin/bash
# DEPENDENCY-FREE: Uses only bash builtins and POSIX tools (grep, sed)
INPUT=$(cat)

# Extract command without jq - uses sed (POSIX standard)
COMMAND=$(echo "$INPUT" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
EXIT_CODE=$(echo "$INPUT" | sed -n 's/.*"exitCode"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' | head -1)

if [[ "$COMMAND" == rcc* ]] && [[ "$EXIT_CODE" == "0" ]]; then
    if [[ "$COMMAND" == *"rcc robot init"* ]]; then
        echo '{"hookSpecificOutput": {"additionalContext": "Remember to run rcc ht vars to pre-build the environment."}}'
    fi
fi
exit 0
```

### SessionStart: session-setup.sh

Set up environment for RCC development:

```bash
#!/bin/bash
if [ -n "$CLAUDE_ENV_FILE" ]; then
    # Set default RCC verbosity
    echo 'export RCC_VERBOSITY=silent' >> "$CLAUDE_ENV_FILE"

    # Check if RCC is installed
    if command -v rcc &> /dev/null; then
        RCC_VERSION=$(rcc version 2>/dev/null | head -1)
        echo "export RCC_VERSION='$RCC_VERSION'" >> "$CLAUDE_ENV_FILE"
    fi

    # Set ROBOCORP_HOME if not already set
    if [ -z "$ROBOCORP_HOME" ]; then
        echo 'export ROBOCORP_HOME=$HOME/.robocorp' >> "$CLAUDE_ENV_FILE"
    fi
fi
exit 0
```

## Hook Events Reference

| Event | When | Use Case |
|-------|------|----------|
| `SessionStart` | Session begins | Set up env vars, check RCC installation |
| `PreToolUse` | Before tool runs | Validate/block dangerous commands |
| `PostToolUse` | After tool succeeds | Provide context, suggest next steps |
| `PostToolUseFailure` | After tool fails | Suggest fixes for common errors |

## Hook Input/Output

**Input (stdin):**
```json
{
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": {
    "command": "rcc run --task Producer"
  },
  "session_id": "abc123"
}
```

**Output (stdout, exit 0):**
```json
{
  "hookSpecificOutput": {
    "additionalContext": "Helpful message for Claude",
    "permissionDecision": "allow",
    "updatedInput": {
      "command": "modified command"
    }
  }
}
```

**Exit Codes:**
- `0` - Success, allow operation
- `2` - Block operation (stderr shown to Claude)
- Other - Non-blocking error

## Permission Patterns

```json
{
  "permissions": {
    "allow": [
      "Bash(rcc *)",                        // All rcc commands
      "Bash(rcc run --task *)",             // Specific pattern
      "Read(./**/*.yaml)",                  // Config files
      "Edit(conda.yaml)"                    // Specific file
    ],
    "deny": [
      "Bash(rcc holotree delete --all)",    // Block dangerous
      "Bash(rm -rf *)",                     // Block destructive
      "Write(.env*)"                        // Protect secrets
    ]
  }
}
```

## Prompt-Based Hooks

Use Claude to make intelligent decisions:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "prompt",
            "prompt": "Check if the RCC robot ran successfully. If there were errors, suggest fixes. If successful, mention the freeze file location.",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

## Environment Variables

Set environment variables for the session:

```json
{
  "env": {
    "TESTING": "true",
    "RCC_VERBOSITY": "silent",
    "ROBOCORP_HOME": "/opt/robocorp"
  }
}
```

Or dynamically in SessionStart hook:

```bash
#!/bin/bash
if [ -n "$CLAUDE_ENV_FILE" ]; then
    # Detect environment
    if [ -f "robot.yaml" ]; then
        echo 'export RCC_PROJECT=true' >> "$CLAUDE_ENV_FILE"
    fi

    # Set testing mode if in dev branch
    BRANCH=$(git branch --show-current 2>/dev/null)
    if [[ "$BRANCH" == "dev"* ]]; then
        echo 'export TESTING=true' >> "$CLAUDE_ENV_FILE"
    fi
fi
exit 0
```

## Project Structure

```
my-project/
├── .claude/
│   ├── settings.json      # Hooks and permissions config
│   ├── settings.local.json # Local overrides (gitignored)
│   └── hooks/
│       ├── validate-rcc-commands.py
│       ├── post-rcc-run.sh
│       └── session-setup.sh
├── robot.yaml
├── conda.yaml
└── tasks.py
```

## Best Practices

1. **Keep hooks fast**: Timeout is limited, avoid slow operations
2. **Use exit codes correctly**: 0 = allow, 2 = block with message
3. **Return JSON for context**: Use `hookSpecificOutput.additionalContext`
4. **Test hooks locally**: Run hook scripts manually first
5. **Log sparingly**: Only stderr on exit 2 is shown to Claude
6. **Handle errors gracefully**: Exit 0 if hook itself fails
