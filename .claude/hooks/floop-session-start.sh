#!/bin/bash
# version: 0.1.0
# Inject behaviors at session start
# This hook runs when a Claude Code session begins

FLOOP_CMD="$(command -v floop 2>/dev/null)"
[ -z "$FLOOP_CMD" ] && exit 0

# Generate prompt with behaviors
"$FLOOP_CMD" prompt --format markdown --token-budget 2000 2>/dev/null

exit 0
