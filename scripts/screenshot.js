#!/usr/bin/env node
// Usage: node scripts/screenshot.js <input.html> [output.png] [--width 1920] [--height 1080] [--wait 2000]
// Requires: npx puppeteer (auto-downloads Chromium on first run)

const puppeteer = require("puppeteer");
const path = require("path");

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = { width: 1920, height: 1080, wait: 2000 };
  const positional = [];

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--width" && args[i + 1]) {
      opts.width = parseInt(args[++i], 10);
    } else if (args[i] === "--height" && args[i + 1]) {
      opts.height = parseInt(args[++i], 10);
    } else if (args[i] === "--wait" && args[i + 1]) {
      opts.wait = parseInt(args[++i], 10);
    } else if (!args[i].startsWith("--")) {
      positional.push(args[i]);
    }
  }

  if (positional.length === 0) {
    console.error(
      "Usage: node scripts/screenshot.js <input.html> [output.png] [--width 1920] [--height 1080] [--wait 2000]"
    );
    process.exit(1);
  }

  opts.input = path.resolve(positional[0]);
  opts.output = positional[1]
    ? path.resolve(positional[1])
    : opts.input.replace(/\.html$/, ".png");

  return opts;
}

async function screenshot(opts) {
  const browser = await puppeteer.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: opts.width, height: opts.height });
    await page.goto(`file://${opts.input}`, { waitUntil: "networkidle0" });

    // Wait for force-graph physics to settle
    await new Promise((r) => setTimeout(r, opts.wait));

    await page.screenshot({ path: opts.output, fullPage: false });
    console.log(`Screenshot saved: ${opts.output}`);
  } finally {
    await browser.close();
  }
}

const opts = parseArgs(process.argv);
screenshot(opts).catch((err) => {
  console.error("Screenshot failed:", err.message);
  process.exit(1);
});
