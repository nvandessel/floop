#!/bin/bash
# version: {{VERSION}}
# Fallback: inject behaviors on first prompt if SessionStart didn't fire
# This ensures new conversations also get behavior injection

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // "unknown"')
RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"
MARKER="${RUNTIME_DIR}/floop-session-${SESSION_ID}"

# Only run once per session (atomic mkdir fails if dir already exists, avoiding TOCTOU race)
if ! mkdir "$MARKER" 2>/dev/null; then
    exit 0
fi

FLOOP_CMD="$(command -v floop 2>/dev/null)"
[ -z "$FLOOP_CMD" ] && exit 0

# Generate prompt with behaviors
"$FLOOP_CMD" prompt --format markdown --token-budget {{TOKEN_BUDGET}} 2>/dev/null

exit 0
