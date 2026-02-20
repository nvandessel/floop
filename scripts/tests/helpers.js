// Shared helpers for Playwright visual tests.
// Usage: const { makeCounter, launchBrowser, waitForGraph, waitForElectric, startServer, SCREENSHOT_DIR } = require("./helpers");

const { chromium } = require("playwright");
const { spawn } = require("child_process");
const path = require("path");

const SCREENSHOT_DIR = path.resolve(__dirname, "../../build/playwright");

/** Returns { assert(cond, msg), summary() } with pass/fail tracking. */
function makeCounter() {
  let passed = 0;
  let failed = 0;

  function assert(condition, msg) {
    if (condition) {
      console.log(`  ok: ${msg}`);
      passed++;
    } else {
      console.log(`  FAIL: ${msg}`);
      failed++;
    }
  }

  function summary() {
    console.log(`\n${passed + failed} assertions, ${failed} failures`);
    console.log(failed === 0 ? "\nPASS" : "\nFAIL");
    return failed === 0 ? 0 : 1;
  }

  return { assert, summary };
}

/** Launch headless Chromium, return { browser, page } with viewport set. */
async function launchBrowser(width = 1920, height = 1080) {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  await page.setViewportSize({ width, height });
  return { browser, page };
}

/** Wait for window.__graph to be defined, then wait for physics simulation to settle. */
async function waitForGraph(page, timeout = 15000) {
  await page.waitForFunction(() => window.__graph, { timeout });
  // Wait for force simulation to cool (alpha < 0.05) or 4s max
  await page.waitForFunction(
    () => {
      const sim = window.__graph.d3Force && window.__graph.d3Force("link");
      // If we can read alpha, wait for it to drop; otherwise just wait
      if (window.__graph.d3Force) {
        try {
          // Access the internal simulation — alpha below 0.05 means settled
          const alpha = window.__graph.d3Force("link")?.simulation?.alpha?.();
          if (alpha !== undefined) return alpha < 0.05;
        } catch {}
      }
      return false;
    },
    { timeout: 4000 }
  ).catch(() => {
    // If alpha check isn't available, fall back to a fixed wait
  });
  // Extra settle time for coordinate accuracy
  await new Promise((r) => setTimeout(r, 1000));
}

/** Wait for both window.__graph and window.__electricSim. */
async function waitForElectric(page, timeout = 15000) {
  await page.waitForFunction(
    () => window.__graph && window.__electricSim,
    { timeout }
  );
}

/**
 * Spawn `floop graph --format html --serve --no-open`, parse URL, poll until ready.
 * Returns { proc, url }. Caller must kill proc in a finally block.
 */
async function startServer(floopBin) {
  const proc = spawn(floopBin, ["graph", "--format", "html", "--serve", "--no-open"], {
    env: { ...process.env, GOWORK: "off" },
    stdio: ["ignore", "pipe", "pipe"],
  });

  // Parse URL from stdout/stderr
  const url = await new Promise((resolve, reject) => {
    const timeout = setTimeout(
      () => reject(new Error("Server did not emit URL within 8s")),
      8000
    );
    const handler = (data) => {
      const match = data.toString().match(/http:\/\/[^\s]+/);
      if (match) {
        clearTimeout(timeout);
        resolve(match[0]);
      }
    };
    proc.stdout.on("data", handler);
    proc.stderr.on("data", handler);
    proc.on("error", (err) => {
      clearTimeout(timeout);
      reject(err);
    });
  });

  // Poll until server is ready
  for (let i = 0; i < 25; i++) {
    try {
      const resp = await fetch(url);
      if (resp.ok) return { proc, url };
    } catch {}
    await new Promise((r) => setTimeout(r, 200));
  }
  proc.kill();
  throw new Error(`Server at ${url} not ready after 5s of polling`);
}

module.exports = {
  makeCounter,
  launchBrowser,
  waitForGraph,
  waitForElectric,
  startServer,
  SCREENSHOT_DIR,
};
