#!/usr/bin/env node
// Focus mode test: verifies node fade on click, BFS distances, and clear on background click.

const { chromium } = require("playwright");
const path = require("path");

const input = process.argv[2];
if (!input) {
  console.error("Usage: node scripts/test-focus.js <input.html>");
  process.exit(1);
}
const inputPath = path.resolve(input);

async function testFocus() {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.setViewportSize({ width: 1920, height: 1080 });

    const logs = [];
    page.on("console", (msg) => logs.push(`[${msg.type()}] ${msg.text()}`));
    page.on("pageerror", (err) => logs.push(`[ERROR] ${err.message}`));

    await page.goto(`file://${inputPath}`, { waitUntil: "networkidle" });
    await new Promise((r) => setTimeout(r, 4000));

    let passed = true;
    let testCount = 0;
    let failCount = 0;

    function assert(condition, msg) {
      testCount++;
      if (!condition) {
        failCount++;
        passed = false;
        console.log(`  FAIL: ${msg}`);
      } else {
        console.log(`  ok: ${msg}`);
      }
    }

    // --- Test 1: Initial state has no focus ---
    console.log("\n=== Test 1: Initial focus state ===");
    const initialState = await page.evaluate(() => window.__getFocusState());
    assert(initialState.focusedNodeId === null, "focusedNodeId is null initially");
    assert(initialState.distances === null, "distances is null initially");

    // --- Test 2: Programmatic focus via __setFocusNode ---
    console.log("\n=== Test 2: Programmatic focus ===");
    const progResult = await page.evaluate(() => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
      if (nodes.length === 0) return { error: "no nodes" };

      // Pick a node that has connections (find one with neighbors)
      const nodeId = nodes[0].id;
      const ok = window.__setFocusNode(nodeId);
      const state = window.__getFocusState();
      return { nodeId, ok, state, nodeCount: nodes.length };
    });

    assert(!progResult.error, "graph has nodes");
    assert(progResult.ok === true, "__setFocusNode returns true");
    assert(
      progResult.state.focusedNodeId === progResult.nodeId,
      "focusedNodeId matches set node"
    );
    assert(progResult.state.distances !== null, "distances computed after setFocusNode");
    assert(
      progResult.state.distances[progResult.nodeId] === 0,
      "focused node has distance 0"
    );

    // Check BFS correctness: neighbors should have distance 1
    console.log("\n=== Test 3: BFS distance correctness ===");
    const bfsCheck = await page.evaluate(() => {
      const state = window.__getFocusState();
      const g = window.__graph;
      const links = g.graphData().links;
      const focusId = state.focusedNodeId;

      // Find direct neighbors
      const neighbors = [];
      links.forEach((l) => {
        const src = l.source.id || l.source;
        const tgt = l.target.id || l.target;
        if (src === focusId) neighbors.push(tgt);
        if (tgt === focusId) neighbors.push(src);
      });

      const neighborDistances = neighbors.map((id) => ({
        id: id.substring(0, 30),
        distance: state.distances[id],
      }));

      // Count nodes at each distance
      const distCounts = {};
      for (const id in state.distances) {
        const d = state.distances[id];
        distCounts[d] = (distCounts[d] || 0) + 1;
      }

      return { neighbors: neighborDistances, distCounts, totalReachable: Object.keys(state.distances).length };
    });

    console.log(`  Reachable nodes: ${bfsCheck.totalReachable}`);
    console.log(`  Distance distribution: ${JSON.stringify(bfsCheck.distCounts)}`);

    const allNeighborsDist1 = bfsCheck.neighbors.every((n) => n.distance === 1);
    assert(
      bfsCheck.neighbors.length === 0 || allNeighborsDist1,
      `all direct neighbors have distance 1 (${bfsCheck.neighbors.length} neighbors)`
    );

    await page.screenshot({ path: "/tmp/focus-test-programmatic.png" });

    // --- Test 4: Clear focus via __clearFocus ---
    console.log("\n=== Test 4: Clear focus ===");
    const clearResult = await page.evaluate(() => {
      window.__clearFocus();
      return window.__getFocusState();
    });
    assert(clearResult.focusedNodeId === null, "focusedNodeId is null after clear");
    assert(clearResult.distances === null, "distances is null after clear");

    // --- Test 5: Click node via screen coords ---
    console.log("\n=== Test 5: Click node via screen coords ===");
    await page.screenshot({ path: "/tmp/focus-test-before-click.png" });

    const clickTarget = await page.evaluate(() => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
      // Find node closest to center
      let best = nodes[0], bestDist = Infinity;
      for (const n of nodes) {
        const d = n.x * n.x + n.y * n.y;
        if (d < bestDist) { bestDist = d; best = n; }
      }
      const coords = g.graph2ScreenCoords(best.x, best.y);
      return { id: best.id, sx: coords.x, sy: coords.y };
    });

    console.log(`  Clicking node at screen (${clickTarget.sx.toFixed(0)}, ${clickTarget.sy.toFixed(0)})`);
    await page.mouse.click(Math.round(clickTarget.sx), Math.round(clickTarget.sy));
    await new Promise((r) => setTimeout(r, 500));

    const afterClick = await page.evaluate(() => window.__getFocusState());
    assert(afterClick.focusedNodeId !== null, "focusedNodeId is set after node click");
    assert(afterClick.distances !== null, "distances computed after node click");

    if (afterClick.focusedNodeId) {
      assert(
        afterClick.distances[afterClick.focusedNodeId] === 0,
        "clicked node has distance 0"
      );
    }

    await page.screenshot({ path: "/tmp/focus-test-focused.png" });

    // --- Test 6: Click background to clear focus ---
    console.log("\n=== Test 6: Click background to clear ===");
    // Click far corner — guaranteed to be background
    await page.mouse.click(10, 10);
    await new Promise((r) => setTimeout(r, 500));

    const afterBgClick = await page.evaluate(() => window.__getFocusState());
    assert(afterBgClick.focusedNodeId === null, "focusedNodeId is null after background click");
    assert(afterBgClick.distances === null, "distances is null after background click");

    await page.screenshot({ path: "/tmp/focus-test-cleared.png" });

    // Console logs
    if (logs.length > 0) {
      console.log("\n=== CONSOLE LOGS ===");
      logs.forEach((l) => console.log("  " + l));
    }

    console.log(`\nScreenshots: /tmp/focus-test-{before-click,programmatic,focused,cleared}.png`);
    console.log(`\n${testCount} assertions, ${failCount} failures`);
    console.log(passed ? "\nPASS" : "\nFAIL");
    process.exit(passed ? 0 : 1);
  } finally {
    await browser.close();
  }
}

testFocus().catch((err) => {
  console.error("Test failed:", err.message);
  process.exit(1);
});
