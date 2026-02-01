#!/bin/bash
# Claude Code PostToolUse hook for RCC commands
# DEPENDENCY-FREE: Uses only bash builtins and standard POSIX tools
#
# Install: Copy to your project and configure in .claude/settings.json:
# {
#   "hooks": {
#     "PostToolUse": [
#       {
#         "matcher": "Bash",
#         "hooks": [{"type": "command", "command": "bash .claude/hooks/post-rcc-run.sh"}]
#       }
#     ]
#   }
# }

# Read stdin into variable (pure bash, no jq needed)
INPUT=$(cat)

# Extract command using bash string manipulation
# The input JSON has format: {"tool_input":{"command":"..."}, ...}
# We use grep/sed which are POSIX standard
COMMAND=""
EXIT_CODE="0"

# Try to extract command - works without jq
if echo "$INPUT" | grep -q '"command"'; then
    # Extract the command value using sed (POSIX)
    COMMAND=$(echo "$INPUT" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
fi

# Try to extract exit code
if echo "$INPUT" | grep -q '"exitCode"'; then
    EXIT_CODE=$(echo "$INPUT" | sed -n 's/.*"exitCode"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' | head -1)
fi

# Only process RCC commands
if [[ "$COMMAND" == rcc* ]]; then

    # After environment build, remind about verification
    if [[ "$COMMAND" == *"rcc run"* ]] || [[ "$COMMAND" == *"rcc ht vars"* ]]; then
        if [[ "$EXIT_CODE" == "0" ]]; then
            echo '{"hookSpecificOutput": {"additionalContext": "Environment built successfully. Freeze file may be available in output/ directory for production deployment."}}'
        fi
    fi

    # After robot init, remind to pre-build
    if [[ "$COMMAND" == *"rcc robot init"* ]] && [[ "$EXIT_CODE" == "0" ]]; then
        echo '{"hookSpecificOutput": {"additionalContext": "Robot created. Remember to run: rcc ht vars -r <path>/robot.yaml --silent to pre-build the environment."}}'
    fi

    # After diagnostics, suggest next steps
    if [[ "$COMMAND" == *"rcc configure diagnostics"* ]] && [[ "$EXIT_CODE" != "0" ]]; then
        echo '{"hookSpecificOutput": {"additionalContext": "Diagnostics found issues. Check network connectivity and disk space. Run with --robot robot.yaml for project-specific diagnostics."}}'
    fi

fi

exit 0
