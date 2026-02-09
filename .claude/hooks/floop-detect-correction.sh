#!/bin/bash
# version: 0.1.0
# Detect corrections in user prompts and auto-capture
# This hook runs on UserPromptSubmit events

INPUT=$(cat)
PROMPT=$(echo "$INPUT" | jq -r '.prompt // empty')

# Skip if no prompt
[ -z "$PROMPT" ] && exit 0

FLOOP_CMD="$(command -v floop 2>/dev/null)"
[ -z "$FLOOP_CMD" ] && exit 0

# Use floop's detection (calls MightBeCorrection + LLM extraction)
# Run in background with timeout to avoid blocking the prompt
timeout 5s "$FLOOP_CMD" detect-correction --prompt "$PROMPT" --json 2>/dev/null &

exit 0
