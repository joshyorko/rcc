#!/bin/bash
# RCC Skill - Environment Setup Hook
# Ensures skill's holotree is available before first use
#
# This runs on SessionStart to auto-import hololib.zip if needed

SKILL_DIR="$CLAUDE_PROJECT_DIR/.claude/skills/rcc"
HOLOLIB_ZIP="$SKILL_DIR/hololib.zip"
ROBOT_YAML="$SKILL_DIR/robot.yaml"

# Check if RCC is available
if ! command -v rcc &> /dev/null; then
    echo '{"hookSpecificOutput": {"additionalContext": "RCC not found. Install with: brew install --cask joshyorko/tools/rcc"}}'
    exit 0
fi

# Check if skill environment exists by testing rcc task script
if rcc task script -r "$ROBOT_YAML" --silent -- python -c "print('ok')" &> /dev/null; then
    # Environment already exists
    exit 0
fi

# Environment doesn't exist - try to import hololib
if [ -f "$HOLOLIB_ZIP" ]; then
    echo '{"hookSpecificOutput": {"additionalContext": "Importing RCC skill environment from hololib.zip..."}}'
    rcc ht import "$HOLOLIB_ZIP" --silent
    if [ $? -eq 0 ]; then
        echo '{"hookSpecificOutput": {"additionalContext": "RCC skill environment imported successfully."}}'
    else
        echo '{"hookSpecificOutput": {"additionalContext": "Failed to import hololib.zip. Building from scratch..."}}'
        rcc ht vars -r "$ROBOT_YAML" --silent
    fi
else
    # No hololib.zip - build from scratch
    echo '{"hookSpecificOutput": {"additionalContext": "Building RCC skill environment from conda.yaml..."}}'
    rcc ht vars -r "$ROBOT_YAML" --silent
fi

exit 0
