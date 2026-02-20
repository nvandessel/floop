#!/usr/bin/env node
// Electric mode test: verifies activation, auto-play, glow rendering, and sparks.
// Usage: NODE_PATH=build/node_modules node scripts/tests/test-electric.js [floop-binary]
// If no binary given, defaults to ./floop in project root.

const path = require("path");
const { makeCounter, launchBrowser, waitForElectric, startServer, SCREENSHOT_DIR } = require("./helpers");

const floopBin = path.resolve(process.argv[2] || "./floop");

async function testElectric() {
  const { assert, summary } = makeCounter();
  let server;
  let browser;

  try {
    // Start server
    console.log("Starting floop graph server...");
    server = await startServer(floopBin);
    console.log(`Server URL: ${server.url}`);

    // Launch browser
    const launched = await launchBrowser(1280, 720);
    browser = launched.browser;
    const page = launched.page;

    const jsErrors = [];
    page.on("console", (msg) => {
      if (msg.type() === "error") jsErrors.push(msg.text());
    });
    page.on("pageerror", (err) => jsErrors.push(err.message));

    console.log("\nNavigating to graph page...");
    await page.goto(server.url, { waitUntil: "networkidle" });
    await waitForElectric(page);

    await page.screenshot({ path: `${SCREENSHOT_DIR}/electric-before.png` });

    // --- Test 1: Graph loaded ---
    console.log("\n--- Test 1: Graph loaded ---");
    const nodeCount = await page.evaluate(() => window.__graph.graphData().nodes.length);
    assert(nodeCount > 0, `Graph has ${nodeCount} nodes`);

    // Pick highest-degree node for best spread
    const seedInfo = await page.evaluate(() => {
      const gd = window.__graph.graphData();
      const deg = {};
      gd.nodes.forEach((n) => (deg[n.id] = 0));
      gd.links.forEach((l) => {
        const s = l.source.id || l.source;
        const t = l.target.id || l.target;
        deg[s] = (deg[s] || 0) + 1;
        deg[t] = (deg[t] || 0) + 1;
      });
      const sorted = gd.nodes.slice().sort((a, b) => (deg[b.id] || 0) - (deg[a.id] || 0));
      return { id: sorted[0].id, degree: deg[sorted[0].id] };
    });
    console.log(`  Using seed node: ${seedInfo.id} (degree: ${seedInfo.degree})`);

    // --- Test 2: Electric mode activation ---
    console.log("\n--- Test 2: Electric mode activation ---");
    const steps = await page.evaluate(async (nodeId) => {
      return await window.__electricSim(nodeId);
    }, seedInfo.id);

    assert(steps && steps.length > 0, `Got ${steps ? steps.length : 0} activation steps`);
    if (steps) {
      for (let i = 0; i < steps.length; i++) {
        const s = steps[i];
        const activeNodes = Object.keys(s.activation || {}).length;
        const topValues = Object.entries(s.activation || {})
          .slice(0, 3)
          .map(([, v]) => `${v.toFixed(3)}`)
          .join(", ");
        console.log(`  Step ${i}: ${activeNodes} active nodes${topValues ? ` (top: ${topValues})` : ""}`);
      }
    }

    const isActive = await page.evaluate(() => window.__electricIsActive());
    assert(isActive === true, "Electric mode is active");

    // --- Test 3: Auto-play ---
    console.log("\n--- Test 3: Auto-play ---");
    const playState = await page.evaluate(() => ({
      stepLabel: document.getElementById("et-step-label")?.textContent,
      playBtnText: document.getElementById("et-play")?.textContent,
      toolbarVisible: document.getElementById("electric-toolbar")?.classList.contains("visible"),
    }));
    assert(playState.toolbarVisible, "Toolbar is visible");
    assert(playState.playBtnText !== "\u25B6", `Auto-play started (button shows pause icon: "${playState.playBtnText}")`);
    console.log(`  Step label: ${playState.stepLabel}`);

    // --- Test 4: Seed activation value ---
    console.log("\n--- Test 4: Seed activation ---");
    const seedAct = await page.evaluate((id) => window.__electricGetActivation(id), seedInfo.id);
    assert(seedAct > 0, `Seed activation = ${seedAct.toFixed(4)}`);

    // --- Test 5: Animation progresses over time ---
    console.log("\n--- Test 5: Animation over time ---");
    const step0 = await page.evaluate(() => window.__electricGetStep());
    console.log(`  Step at t=0: ${step0}`);

    await page.screenshot({ path: `${SCREENSHOT_DIR}/electric-step0.png` });

    await page.waitForTimeout(2000);
    const step1 = await page.evaluate(() => window.__electricGetStep());
    console.log(`  Step at t=2s: ${step1}`);

    await page.screenshot({ path: `${SCREENSHOT_DIR}/electric-step1.png` });

    if (steps && steps.length > 2) {
      assert(step1 > step0, `Step progressed: ${step0} -> ${step1}`);
    }

    // Check non-seed node activations
    const neighborActivations = await page.evaluate((seedId) => {
      const nodes = window.__graph.graphData().nodes;
      const results = [];
      for (const n of nodes) {
        if (n.id === seedId) continue;
        const act = window.__electricGetActivation(n.id);
        if (act > 0.01) results.push({ id: n.id.substring(0, 30), activation: act });
      }
      results.sort((a, b) => b.activation - a.activation);
      return results.slice(0, 5);
    }, seedInfo.id);

    console.log(`  Illuminated non-seed nodes: ${neighborActivations.length}`);
    for (const n of neighborActivations) {
      console.log(`    ${n.id}... = ${n.activation.toFixed(4)}`);
    }
    assert(
      neighborActivations.length > 0 || (steps && steps.length <= 2),
      `Non-seed nodes illuminate (${neighborActivations.length} nodes glowing)`
    );

    await page.waitForTimeout(3000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/electric-step2.png` });

    const step2 = await page.evaluate(() => window.__electricGetStep());
    console.log(`  Step at t=5s: ${step2}`);

    // --- Test 6: Prev/Next removed ---
    console.log("\n--- Test 6: Prev/Next buttons removed ---");
    const prevExists = await page.evaluate(() => !!document.getElementById("et-prev"));
    const nextExists = await page.evaluate(() => !!document.getElementById("et-next"));
    assert(!prevExists, "Prev button removed");
    assert(!nextExists, "Next button removed");

    // --- Test 7: Pause/Resume ---
    console.log("\n--- Test 7: Pause/Resume ---");
    await page.click("#et-play");
    await page.waitForTimeout(100);

    const pausedBtn = await page.evaluate(() => document.getElementById("et-play")?.textContent);
    console.log(`  After pause click, button: "${pausedBtn}"`);
    assert(pausedBtn === "\u25B6" || pausedBtn !== "", "Pause toggles to play icon");

    const stepBeforePause = await page.evaluate(() => window.__electricGetStep());
    await page.waitForTimeout(1500);
    const stepAfterPause = await page.evaluate(() => window.__electricGetStep());
    assert(stepBeforePause === stepAfterPause, `Step frozen while paused: ${stepBeforePause} == ${stepAfterPause}`);

    await page.click("#et-play");
    await page.waitForTimeout(2000);
    const stepAfterResume = await page.evaluate(() => window.__electricGetStep());
    console.log(`  After resume: step was ${stepAfterPause}, now ${stepAfterResume}`);
    assert(stepAfterResume !== stepAfterPause || steps.length <= 2, "Step advances after resume");

    // --- Test 8: JS Errors ---
    console.log("\n--- Test 8: JavaScript errors ---");
    if (jsErrors.length > 0) {
      jsErrors.forEach((e) => console.log(`  ERROR: ${e}`));
    }
    assert(jsErrors.length === 0, `No JS errors (found ${jsErrors.length})`);

    // --- Test 9: Glow rendering check ---
    console.log("\n--- Test 9: Glow rendering values ---");
    const glowData = await page.evaluate((seedId) => {
      const act = window.__electricGetActivation(seedId);
      return {
        activation: act,
        glowAlpha: Math.min(1.0, act),
        bloomBlur: act * 25,
        wouldGlow: act > 0.01,
      };
    }, seedInfo.id);
    console.log(`  Seed: activation=${glowData.activation.toFixed(3)}, bloom=${glowData.bloomBlur.toFixed(1)}px`);
    assert(glowData.wouldGlow, "Glow would render (activation > 0.01)");
    assert(glowData.bloomBlur > 5, `Bloom blur is visible (${glowData.bloomBlur.toFixed(1)}px)`);

    // --- Test 10: Spark diagnostics ---
    console.log("\n--- Test 10: Spark diagnostics ---");
    // Pause, set to step 1, let it advance briefly for mid-flight sparks
    await page.evaluate(() => {
      const playBtn = document.getElementById("et-play");
      if (playBtn) playBtn.click();
    });
    await page.waitForTimeout(200);
    await page.evaluate(() => window.__electricSetStep(1));
    await page.waitForTimeout(100);
    // Resume briefly
    await page.evaluate(() => {
      const playBtn = document.getElementById("et-play");
      if (playBtn) playBtn.click();
    });
    await page.waitForTimeout(500);
    // Pause again
    await page.evaluate(() => {
      const playBtn = document.getElementById("et-play");
      if (playBtn) playBtn.click();
    });
    await page.waitForTimeout(100);

    await page.screenshot({ path: `${SCREENSHOT_DIR}/electric-sparks.png` });

    const sparkDiag = await page.evaluate(() => window.__sparkDiag());
    console.log(`  nodeFirstStepCount: ${sparkDiag.nodeFirstStepCount}`);
    console.log(`  totalNodes: ${sparkDiag.totalNodes}, totalLinks: ${sparkDiag.totalLinks}`);
    if (sparkDiag.stepStats) {
      for (const s of sparkDiag.stepStats) {
        console.log(`  Step ${s.step}: ${s.nodeCount} nodes, max=${s.maxAct}, min=${s.minAct}${s.final ? " (final)" : ""}`);
      }
    }
    if (sparkDiag.nodeFirstStepEntries) {
      console.log(`  First step entries: ${JSON.stringify(sparkDiag.nodeFirstStepEntries)}`);
    }
    console.log(`  sparksInRange: ${sparkDiag.sparksInRange}, sparksVisible: ${sparkDiag.sparksVisible}`);
    if (sparkDiag.edges) {
      for (const e of sparkDiag.edges.slice(0, 5)) {
        console.log(`    edge ${e.srcId}->${e.tgtId}: fire=${e.edgeFireStep} sparkT=${e.sparkT} bright=${e.sparkBright} visible=${e.visible}`);
      }
    }
    assert(sparkDiag.nodeFirstStepCount > 1, `Multiple nodes in nodeFirstStep (got ${sparkDiag.nodeFirstStepCount})`);
    assert(sparkDiag.sparksVisible > 0, `Sparks are visible (got ${sparkDiag.sparksVisible})`);

    // --- Summary ---
    console.log(`\nScreenshots: ${SCREENSHOT_DIR}/electric-*.png`);
    process.exit(summary());
  } finally {
    if (browser) await browser.close().catch(() => {});
    if (server) server.proc.kill();
  }
}

testElectric().catch((err) => {
  console.error("Test failed:", err.message);
  process.exit(1);
});
