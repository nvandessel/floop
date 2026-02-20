#!/usr/bin/env node
// Minimal test: verify sparks render visually. Properly cleans up browser + server.
import { chromium } from 'playwright';
import { spawn } from 'child_process';

let server, browser;

// Ensure cleanup on ANY exit
function cleanup() {
  if (browser) { try { browser.close(); } catch {} browser = null; }
  if (server) { try { server.kill('SIGKILL'); } catch {} server = null; }
}
process.on('exit', cleanup);
process.on('SIGINT', () => { cleanup(); process.exit(1); });
process.on('SIGTERM', () => { cleanup(); process.exit(1); });
process.on('uncaughtException', (e) => { console.error(e); cleanup(); process.exit(1); });

async function main() {
  server = spawn('./floop', ['graph', '--format', 'html', '--serve'], {
    cwd: '/home/nvandessel/repos/feedback-loop/.worktrees/feature-electric-graph',
    env: { ...process.env, GOWORK: 'off' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  const url = await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('No URL')), 8000);
    const handler = (data) => {
      const match = data.toString().match(/http:\/\/[^\s]+/);
      if (match) { clearTimeout(timeout); resolve(match[0]); }
    };
    server.stderr.on('data', handler);
    server.stdout.on('data', handler);
  });

  // Wait for server ready
  for (let i = 0; i < 20; i++) {
    try { const r = await fetch(url); if (r.ok) break; } catch {}
    await new Promise(r => setTimeout(r, 200));
  }

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  await page.goto(url, { waitUntil: 'networkidle' });
  await page.waitForFunction(() => window.__graph && window.__electricSim, { timeout: 15000 });
  await page.waitForTimeout(2000); // settle

  // Pick highest-degree node
  const seed = await page.evaluate(() => {
    const gd = window.__graph.graphData();
    const deg = {};
    gd.nodes.forEach(n => deg[n.id] = 0);
    gd.links.forEach(l => {
      const s = l.source.id || l.source;
      const t = l.target.id || l.target;
      deg[s] = (deg[s] || 0) + 1;
      deg[t] = (deg[t] || 0) + 1;
    });
    const sorted = gd.nodes.slice().sort((a, b) => (deg[b.id] || 0) - (deg[a.id] || 0));
    return sorted[0].id;
  });
  console.log(`Seed: ${seed}`);

  // Activate electric mode
  await page.evaluate(async (id) => await window.__electricSim(id), seed);
  console.log('Electric mode activated');

  // Wait for sparks to reach peak (step 1.25)
  await page.waitForTimeout(2500);

  // Take the money shot
  await page.screenshot({ path: 'build/sparks-proof.png' });
  console.log('Screenshot: build/sparks-proof.png');

  // Run spark diagnostic
  const diag = await page.evaluate(() => window.__sparkDiag());
  console.log(`nodeFirstStepCount: ${diag.nodeFirstStepCount}`);
  console.log(`sparksVisible: ${diag.sparksVisible} / ${diag.totalLinks} edges`);
  if (diag.sparksVisible > 0) {
    console.log('SPARKS ARE RENDERING!');
  } else {
    console.log('NO SPARKS VISIBLE');
  }

  // Wait one more cycle, capture another frame
  await page.waitForTimeout(4000);
  await page.screenshot({ path: 'build/sparks-proof-2.png' });
  console.log('Screenshot: build/sparks-proof-2.png');

  cleanup();
  console.log('Done. Browser and server cleaned up.');
}

main().catch(err => {
  console.error('Error:', err);
  cleanup();
  process.exit(1);
});
