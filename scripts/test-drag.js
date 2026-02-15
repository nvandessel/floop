#!/usr/bin/env node
// Diagnostic test: captures detailed state before/during/after mouse interaction.

const { chromium } = require("playwright");
const path = require("path");

const input = process.argv[2];
if (!input) {
  console.error("Usage: node scripts/test-drag.js <input.html>");
  process.exit(1);
}
const inputPath = path.resolve(input);

async function testDrag() {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.setViewportSize({ width: 1920, height: 1080 });

    // Capture all console output
    const logs = [];
    page.on("console", (msg) => logs.push(`[${msg.type()}] ${msg.text()}`));
    page.on("pageerror", (err) => logs.push(`[ERROR] ${err.message}`));

    await page.goto(`file://${inputPath}`, { waitUntil: "networkidle" });
    await new Promise((r) => setTimeout(r, 4000));

    // Diagnostic: check graph state
    const state = await page.evaluate(() => {
      const g = window.__graph;
      if (!g) return { error: "no __graph" };

      const data = g.graphData();
      const nodes = data.nodes || [];

      // Sample first 5 nodes' positions
      const samples = nodes.slice(0, 5).map((n) => ({
        id: n.id.substring(0, 30),
        x: n.x, y: n.y,
        fx: n.fx, fy: n.fy,
        vx: n.vx, vy: n.vy,
      }));

      // Check for NaN/Infinity
      const badNodes = nodes.filter(
        (n) => !isFinite(n.x) || !isFinite(n.y)
      ).length;

      return {
        nodeCount: nodes.length,
        linkCount: (data.links || []).length,
        badNodes,
        samples,
        zoom: g.zoom(),
        centerAt: g.centerAt(),
        engineRunning: typeof g.d3ReheatSimulation === "function",
      };
    });

    console.log("=== GRAPH STATE (before interaction) ===");
    console.log(`Nodes: ${state.nodeCount}, Links: ${state.linkCount}`);
    console.log(`Bad nodes (NaN/Infinity): ${state.badNodes}`);
    console.log(`Zoom: ${state.zoom}`);
    console.log("Sample nodes:");
    for (const s of state.samples) {
      console.log(
        `  ${s.id}: pos=(${s.x?.toFixed(1)}, ${s.y?.toFixed(1)}) vel=(${s.vx?.toFixed(4)}, ${s.vy?.toFixed(4)}) fx=${s.fx} fy=${s.fy}`
      );
    }

    // Get a node near center
    const target = await page.evaluate(() => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
      let best = nodes[0], bestDist = Infinity;
      for (const n of nodes) {
        const d = n.x * n.x + n.y * n.y;
        if (d < bestDist) { bestDist = d; best = n; }
      }
      const coords = g.graph2ScreenCoords(best.x, best.y);
      return { id: best.id, sx: coords.x, sy: coords.y, gx: best.x, gy: best.y };
    });

    console.log(`\nTarget: screen=(${target.sx.toFixed(0)}, ${target.sy.toFixed(0)}) graph=(${target.gx.toFixed(1)}, ${target.gy.toFixed(1)})`);

    // Inject drag event listener to check if drag fires
    await page.evaluate(() => {
      window.__dragEvents = [];
      const canvas = document.querySelector("canvas");
      for (const evt of ["pointerdown", "pointermove", "pointerup", "mousedown", "mousemove", "mouseup"]) {
        canvas.addEventListener(evt, (e) => {
          window.__dragEvents.push({
            type: e.type,
            x: e.clientX,
            y: e.clientY,
            ts: Date.now(),
          });
        });
      }

      // Hook into force-graph's drag handler (onNodeDrag only — avoid
      // overriding onNodeDragEnd which is configured in the production template)
      const g = window.__graph;
      g.onNodeDrag((node) => {
        window.__dragEvents.push({ type: "onNodeDrag", nodeId: node.id, ts: Date.now() });
      });
    });

    await page.screenshot({ path: "/tmp/drag-test-before.png" });

    // Perform the drag
    const sx = Math.round(target.sx);
    const sy = Math.round(target.sy);
    console.log(`\n=== DRAGGING (${sx},${sy}) -> (${sx + 150},${sy}) ===`);

    await page.mouse.move(sx, sy);
    await page.mouse.down();

    // Move in small steps
    for (let i = 1; i <= 15; i++) {
      await page.mouse.move(sx + i * 10, sy);
      await new Promise((r) => setTimeout(r, 30));
    }

    await new Promise((r) => setTimeout(r, 200));

    // Check state during drag
    const duringState = await page.evaluate((nodeId) => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
      const target = nodes.find((n) => n.id === nodeId);
      const badNodes = nodes.filter(
        (n) => !isFinite(n.x) || !isFinite(n.y)
      ).length;

      const events = window.__dragEvents;
      const dragFired = events.some((e) => e.type === "onNodeDrag");

      return {
        badNodes,
        targetPos: target
          ? { x: target.x, y: target.y, fx: target.fx, fy: target.fy }
          : null,
        eventTypes: [...new Set(events.map((e) => e.type))],
        eventCount: events.length,
        dragFired,
        zoom: g.zoom(),
      };
    }, target.id);

    console.log("\n=== STATE DURING DRAG ===");
    console.log(`Bad nodes: ${duringState.badNodes}`);
    console.log(`Zoom: ${duringState.zoom}`);
    console.log(`Events captured: ${duringState.eventCount} (types: ${duringState.eventTypes.join(", ")})`);
    console.log(`onNodeDrag fired: ${duringState.dragFired}`);
    if (duringState.targetPos) {
      console.log(
        `Target pos: (${duringState.targetPos.x?.toFixed(1)}, ${duringState.targetPos.y?.toFixed(1)}) fx=${duringState.targetPos.fx} fy=${duringState.targetPos.fy}`
      );
    }

    await page.mouse.up();
    await new Promise((r) => setTimeout(r, 500));

    // Check state after release
    const afterState = await page.evaluate(() => {
      const g = window.__graph;
      const nodes = g.graphData().nodes;
      const badNodes = nodes.filter(
        (n) => !isFinite(n.x) || !isFinite(n.y)
      ).length;
      const samples = nodes.slice(0, 3).map((n) => ({
        id: n.id.substring(0, 30),
        x: n.x, y: n.y,
      }));
      return { badNodes, samples, zoom: g.zoom() };
    });

    console.log("\n=== STATE AFTER RELEASE ===");
    console.log(`Bad nodes: ${afterState.badNodes}`);
    console.log(`Zoom: ${afterState.zoom}`);
    for (const s of afterState.samples) {
      console.log(`  ${s.id}: (${s.x?.toFixed(1)}, ${s.y?.toFixed(1)})`);
    }

    await page.screenshot({ path: "/tmp/drag-test-after.png" });

    // Console logs
    if (logs.length > 0) {
      console.log("\n=== CONSOLE LOGS ===");
      logs.forEach((l) => console.log("  " + l));
    }

    console.log("\nScreenshots: /tmp/drag-test-before.png, /tmp/drag-test-after.png");

    const passed = duringState.dragFired && afterState.badNodes === 0;
    console.log(passed ? "\nPASS" : "\nFAIL");
    process.exit(passed ? 0 : 1);
  } finally {
    await browser.close();
  }
}

testDrag().catch((err) => {
  console.error("Test failed:", err.message);
  process.exit(1);
});
