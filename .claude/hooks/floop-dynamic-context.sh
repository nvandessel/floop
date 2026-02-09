#!/bin/bash
# version: 0.1.0
# Dynamic context injection hook
# Triggered on PreToolUse for Read, Write, Edit, Bash tools
# Detects context changes and injects relevant behaviors via spreading activation

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
TOOL_INPUT=$(echo "$INPUT" | jq -r '.tool_input // empty')

[ -z "$TOOL_NAME" ] && exit 0

FLOOP_CMD="$(command -v floop 2>/dev/null)"
[ -z "$FLOOP_CMD" ] && exit 0

# Extract session ID for state persistence
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // "default"')

case "$TOOL_NAME" in
    Read|Edit|Write)
        # Extract file path from tool input
        FILE_PATH=$(echo "$TOOL_INPUT" | jq -r '.file_path // .path // empty')
        [ -z "$FILE_PATH" ] && exit 0
        # Trigger context-aware activation
        "$FLOOP_CMD" activate --file "$FILE_PATH" --format markdown --token-budget 500 --session-id "$SESSION_ID" 2>/dev/null
        ;;
    Bash)
        # Extract command from tool input
        COMMAND=$(echo "$TOOL_INPUT" | jq -r '.command // empty')
        [ -z "$COMMAND" ] && exit 0
        # Detect intent from command
        TASK=""
        case "$COMMAND" in
            git\ commit*|git\ push*|git\ merge*) TASK="committing" ;;
            git\ *) TASK="git-operations" ;;
            go\ test*|pytest*|npm\ test*|jest*) TASK="testing" ;;
            go\ build*|npm\ run\ build*|make*) TASK="building" ;;
            docker*|kubectl*) TASK="deployment" ;;
        esac
        [ -z "$TASK" ] && exit 0
        "$FLOOP_CMD" activate --task "$TASK" --format markdown --token-budget 500 --session-id "$SESSION_ID" 2>/dev/null
        ;;
esac

exit 0
