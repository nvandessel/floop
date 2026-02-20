#!/usr/bin/env node
// Focus mode test: verifies node fade on click, BFS distances, and clear on background click.
// Usage: NODE_PATH=build/node_modules node scripts/tests/test-focus.js <input.html>

const path = require("path");
const { makeCounter, launchBrowser, waitForGraph, SCREENSHOT_DIR } = require("./helpers");

const input = process.argv[2];
if (!input) {
  console.error("Usage: node scripts/tests/test-focus.js <input.html>");
  process.exit(1);
}
const inputPath = path.resolve(input);

async function testFocus() {
  const { browser, page } = await launchBrowser();
  const { assert, summary } = makeCounter();

  try {
    const logs = [];
    page.on("console", (msg) => logs.push(`[${msg.type()}] ${msg.text()}`));
    page.on("pageerror", (err) => logs.push(`[ERROR] ${err.message}`));

    await page.goto(`file://${inputPath}`, { waitUntil: "networkidle" });
    await waitForGraph(page);

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
      const links = g.graphData().links;
      if (nodes.length === 0) return { error: "no nodes" };

      const connectedIds = new Set();
      links.forEach((l) => {
        connectedIds.add(l.source.id || l.source);
        connectedIds.add(l.target.id || l.target);
      });
      const connectedNode = nodes.find((n) => connectedIds.has(n.id));
      const nodeId = connectedNode ? connectedNode.id : nodes[0].id;

      const ok = window.__setFocusNode(nodeId);
      const state = window.__getFocusState();
      return {
        nodeId,
        ok,
        state,
        nodeCount: nodes.length,
        hasConnections: !!connectedNode,
      };
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

    // --- Test 3: BFS distance correctness ---
    console.log("\n=== Test 3: BFS distance correctness ===");
    const bfsCheck = await page.evaluate(() => {
      const state = window.__getFocusState();
      const g = window.__graph;
      const links = g.graphData().links;
      const nodes = g.graphData().nodes;
      const focusId = state.focusedNodeId;

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

      const distCounts = {};
      for (const id in state.distances) {
        const d = state.distances[id];
        distCounts[d] = (distCounts[d] || 0) + 1;
      }

      return {
        neighbors: neighborDistances,
        distCounts,
        totalReachable: Object.keys(state.distances).length,
        totalNodes: nodes.length,
      };
    });

    console.log(`  Reachable nodes: ${bfsCheck.totalReachable}/${bfsCheck.totalNodes}`);
    console.log(`  Distance distribution: ${JSON.stringify(bfsCheck.distCounts)}`);

    const allNeighborsDist1 = bfsCheck.neighbors.every((n) => n.distance === 1);
    assert(
      bfsCheck.neighbors.length === 0 || allNeighborsDist1,
      `all direct neighbors have distance 1 (${bfsCheck.neighbors.length} neighbors)`
    );
    assert(
      bfsCheck.totalReachable <= bfsCheck.totalNodes,
      `reachable (${bfsCheck.totalReachable}) <= total (${bfsCheck.totalNodes})`
    );

    // --- Test 3b: Opacity values for different distances ---
    console.log("\n=== Test 3b: Opacity values ===");
    const opacityCheck = await page.evaluate(() => {
      const state = window.__getFocusState();
      const g = window.__graph;
      const nodes = g.graphData().nodes;

      const focusedOpacity = window.__getNodeOpacity(state.focusedNodeId);

      let d1Node = null, d2Node = null, unreachableNode = null;
      for (const n of nodes) {
        const d = state.distances[n.id];
        if (d === 1 && !d1Node) d1Node = n.id;
        if (d === 2 && !d2Node) d2Node = n.id;
        if (d === undefined && !unreachableNode) unreachableNode = n.id;
      }

      return {
        focusedOpacity,
        d1Opacity: d1Node ? window.__getNodeOpacity(d1Node) : null,
        d2Opacity: d2Node ? window.__getNodeOpacity(d2Node) : null,
        unreachableOpacity: unreachableNode ? window.__getNodeOpacity(unreachableNode) : null,
      };
    });

    assert(opacityCheck.focusedOpacity === 1.0, "focused node opacity is 1.0");
    if (opacityCheck.d1Opacity !== null) {
      assert(opacityCheck.d1Opacity === 1.0, "distance-1 node opacity is 1.0");
    }
    if (opacityCheck.d2Opacity !== null) {
      assert(opacityCheck.d2Opacity === 0.4, "distance-2 node opacity is 0.4");
    }
    if (opacityCheck.unreachableOpacity !== null) {
      assert(opacityCheck.unreachableOpacity === 0.12, "unreachable node opacity is 0.12");
    }

    await page.screenshot({ path: `${SCREENSHOT_DIR}/focus-programmatic.png` });

    // --- Test 4: Clear focus via __clearFocus ---
    console.log("\n=== Test 4: Clear focus ===");
    const clearResult = await page.evaluate(() => {
      window.__clearFocus();
      return window.__getFocusState();
    });
    assert(clearResult.focusedNodeId === null, "focusedNodeId is null after clear");
    assert(clearResult.distances === null, "distances is null after clear");

    const noFocusOpacity = await page.evaluate(() => {
      const nodes = window.__graph.graphData().nodes;
      return window.__getNodeOpacity(nodes[0].id);
    });
    assert(noFocusOpacity === 1.0, "all nodes opacity 1.0 when no focus");

    // --- Test 5: Click node via screen coords ---
    console.log("\n=== Test 5: Click node via screen coords ===");
    await page.screenshot({ path: `${SCREENSHOT_DIR}/focus-before-click.png` });

    const clickTarget = await page.evaluate(() => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
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

    await page.screenshot({ path: `${SCREENSHOT_DIR}/focus-focused.png` });

    // --- Test 6: Click background to clear focus ---
    console.log("\n=== Test 6: Click background to clear ===");
    await page.mouse.click(10, 10);
    await new Promise((r) => setTimeout(r, 500));

    const afterBgClick = await page.evaluate(() => window.__getFocusState());
    assert(afterBgClick.focusedNodeId === null, "focusedNodeId is null after background click");
    assert(afterBgClick.distances === null, "distances is null after background click");

    await page.screenshot({ path: `${SCREENSHOT_DIR}/focus-cleared.png` });

    // --- Test 7: closePanel clears focus state ---
    console.log("\n=== Test 7: Panel close clears focus ===");
    const panelTest = await page.evaluate(() => {
      const nodes = window.__graph.graphData().nodes;
      window.__setFocusNode(nodes[0].id);
      const before = window.__getFocusState();
      window.closePanel();
      const after = window.__getFocusState();
      return {
        beforeFocused: before.focusedNodeId !== null,
        afterFocused: after.focusedNodeId,
        afterDistances: after.distances,
      };
    });
    assert(panelTest.beforeFocused, "focus was active before closePanel");
    assert(panelTest.afterFocused === null, "closePanel clears focusedNodeId");
    assert(panelTest.afterDistances === null, "closePanel clears distances");

    // --- Test 7b: Re-focus switches to different node ---
    console.log("\n=== Test 7b: Re-focus switches node ===");
    const refocusTest = await page.evaluate(() => {
      const nodes = window.__graph.graphData().nodes;
      if (nodes.length < 2) return { skip: true };
      const idA = nodes[0].id;
      const idB = nodes[1].id;
      window.__setFocusNode(idA);
      const stateA = window.__getFocusState();
      window.__setFocusNode(idB);
      const stateB = window.__getFocusState();
      return {
        skip: false,
        idA,
        idB,
        focusedA: stateA.focusedNodeId,
        distA_A: stateA.distances[idA],
        focusedB: stateB.focusedNodeId,
        distB_B: stateB.distances[idB],
        distB_A: stateB.distances[idA],
      };
    });
    if (!refocusTest.skip) {
      assert(refocusTest.focusedA === refocusTest.idA, "first focus is on node A");
      assert(refocusTest.distA_A === 0, "node A has distance 0 when focused");
      assert(refocusTest.focusedB === refocusTest.idB, "re-focus switches to node B");
      assert(refocusTest.distB_B === 0, "node B has distance 0 when focused");
      assert(refocusTest.focusedB !== refocusTest.focusedA, "focus actually changed nodes");
    }

    await page.evaluate(() => window.__clearFocus());

    // --- Test 8: __setFocusNode rejects invalid node IDs ---
    console.log("\n=== Test 8: Invalid node ID rejection ===");
    const invalidResult = await page.evaluate(() => {
      window.__clearFocus();
      const ok = window.__setFocusNode("nonexistent-node-id");
      const state = window.__getFocusState();
      return { ok, state };
    });
    assert(invalidResult.ok === false, "__setFocusNode returns false for invalid ID");
    assert(invalidResult.state.focusedNodeId === null, "focus not set for invalid node");
    assert(invalidResult.state.distances === null, "distances not set for invalid node");

    // --- Test 9: Scope filter clears focus ---
    console.log("\n=== Test 9: Scope filter clears focus ===");
    const scopeTest = await page.evaluate(() => {
      const nodes = window.__graph.graphData().nodes;
      window.__setFocusNode(nodes[0].id);
      const beforeScope = window.__getFocusState();

      const btn = document.querySelector('#scope-filter button[data-scope="local"]');
      if (btn) btn.click();

      const afterScope = window.__getFocusState();
      return {
        beforeFocused: beforeScope.focusedNodeId !== null,
        afterFocused: afterScope.focusedNodeId,
        afterDistances: afterScope.distances,
      };
    });
    assert(scopeTest.beforeFocused, "focus was active before scope change");
    assert(scopeTest.afterFocused === null, "scope change clears focusedNodeId");
    assert(scopeTest.afterDistances === null, "scope change clears distances");

    await page.evaluate(() => {
      const btn = document.querySelector('#scope-filter button[data-scope="all"]');
      if (btn) btn.click();
    });

    // --- Final: Check for console errors ---
    console.log("\n=== Console error check ===");
    const errors = logs.filter((l) => l.startsWith("[ERROR]") || l.startsWith("[error]"));
    assert(errors.length === 0, `no console errors (got ${errors.length})`);

    if (logs.length > 0) {
      console.log("\n=== CONSOLE LOGS ===");
      logs.forEach((l) => console.log("  " + l));
    }

    console.log(`\nScreenshots: ${SCREENSHOT_DIR}/focus-*.png`);
    process.exit(summary());
  } finally {
    await browser.close();
  }
}

testFocus().catch((err) => {
  console.error("Test failed:", err.message);
  process.exit(1);
});
