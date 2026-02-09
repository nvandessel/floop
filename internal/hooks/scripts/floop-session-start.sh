#!/bin/bash
# version: {{VERSION}}
# Inject behaviors at session start
# This hook runs when a Claude Code session begins

FLOOP_CMD="$(command -v floop 2>/dev/null)"
[ -z "$FLOOP_CMD" ] && exit 0

# Generate prompt with behaviors
"$FLOOP_CMD" prompt --format markdown --token-budget {{TOKEN_BUDGET}} 2>/dev/null

exit 0
