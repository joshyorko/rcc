#!/bin/bash
# Claude Code SessionStart hook for RCC projects
#
# Install: Copy to your project and configure in .claude/settings.json:
# {
#   "hooks": {
#     "SessionStart": [
#       {
#         "hooks": [{"type": "command", "command": "bash .claude/hooks/session-setup.sh"}]
#       }
#     ]
#   }
# }
#
# This hook sets up environment variables and checks RCC installation.

if [ -n "$CLAUDE_ENV_FILE" ]; then
    # Set default RCC verbosity
    echo 'export RCC_VERBOSITY=silent' >> "$CLAUDE_ENV_FILE"

    # Check if RCC is installed
    if command -v rcc &> /dev/null; then
        RCC_VERSION=$(rcc version 2>/dev/null | head -1)
        echo "export RCC_INSTALLED=true" >> "$CLAUDE_ENV_FILE"
        echo "export RCC_VERSION='$RCC_VERSION'" >> "$CLAUDE_ENV_FILE"
    else
        echo "export RCC_INSTALLED=false" >> "$CLAUDE_ENV_FILE"
        # Check if Homebrew is available for installation hint
        if command -v brew &> /dev/null; then
            echo "export RCC_INSTALL_HINT='brew install --cask joshyorko/tools/rcc'" >> "$CLAUDE_ENV_FILE"
        else
            echo "export RCC_INSTALL_HINT='curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64 && chmod +x rcc && sudo mv rcc /usr/local/bin/'" >> "$CLAUDE_ENV_FILE"
        fi
    fi

    # Check if Action Server is installed
    if command -v action-server &> /dev/null; then
        AS_VERSION=$(action-server version 2>/dev/null | head -1)
        echo "export ACTION_SERVER_INSTALLED=true" >> "$CLAUDE_ENV_FILE"
        echo "export ACTION_SERVER_VERSION='$AS_VERSION'" >> "$CLAUDE_ENV_FILE"
    else
        echo "export ACTION_SERVER_INSTALLED=false" >> "$CLAUDE_ENV_FILE"
    fi

    # Set ROBOCORP_HOME if not already set
    if [ -z "$ROBOCORP_HOME" ]; then
        echo 'export ROBOCORP_HOME=$HOME/.robocorp' >> "$CLAUDE_ENV_FILE"
    fi

    # Detect if we're in an RCC project
    if [ -f "robot.yaml" ]; then
        echo 'export RCC_PROJECT=true' >> "$CLAUDE_ENV_FILE"
    fi

    # Set testing mode if in dev branch
    BRANCH=$(git branch --show-current 2>/dev/null)
    if [[ "$BRANCH" == "dev"* ]] || [[ "$BRANCH" == "feature/"* ]]; then
        echo 'export TESTING=true' >> "$CLAUDE_ENV_FILE"
    fi
fi

exit 0
