#!/usr/bin/env bash
# Run the full Playwright visual test suite.
# Usage: bash scripts/tests/run-all.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

FLOOP_BIN="$REPO_ROOT/floop"
GRAPH_HTML="$REPO_ROOT/build/graph/graph.html"
SCREENSHOT_DIR="$REPO_ROOT/build/playwright"

# --- Step 1: Ensure playwright is installed ---
if [ ! -d build/node_modules/playwright ]; then
  echo "==> Installing playwright..."
  npm install --prefix build playwright
  npx --prefix build playwright install chromium
fi

# --- Step 2: Build floop binary ---
echo "==> Building floop..."
go build -o "$FLOOP_BIN" ./cmd/floop

# --- Step 3: Generate static HTML ---
echo "==> Generating graph HTML..."
mkdir -p build/graph
"$FLOOP_BIN" graph --format html -o "$GRAPH_HTML" --no-open

# --- Step 4: Create screenshot directory ---
mkdir -p "$SCREENSHOT_DIR"

# --- Step 5: Run tests ---
total=0
pass=0
fail=0
failed_tests=""

run_test() {
  local name="$1"
  shift
  total=$((total + 1))
  echo ""
  echo "========================================"
  echo "  Running: $name"
  echo "========================================"
  if NODE_PATH=build/node_modules "$@"; then
    pass=$((pass + 1))
    echo "  >> $name: PASS"
  else
    fail=$((fail + 1))
    failed_tests="$failed_tests $name"
    echo "  >> $name: FAIL"
  fi
}

run_test "test-focus" node scripts/tests/test-focus.js "$GRAPH_HTML"
run_test "test-drag" node scripts/tests/test-drag.js "$GRAPH_HTML"
run_test "test-electric" node scripts/tests/test-electric.js "$FLOOP_BIN"

# --- Step 6: Summary ---
echo ""
echo "========================================"
echo "  Suite Summary: $pass/$total passed, $fail failed"
if [ -n "$failed_tests" ]; then
  echo "  Failed:$failed_tests"
fi
echo "  Screenshots: $SCREENSHOT_DIR/"
echo "========================================"

exit "$fail"
