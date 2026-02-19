#!/usr/bin/env node
// Capture animated GIF of electric mode for visual verification
// Shows: graph loading → settling → node click → auto-zoom → animation
import { chromium } from 'playwright';
import { spawn, execSync } from 'child_process';

let server, browser;

async function main() {
  execSync('mkdir -p build/gif');

  // Start server
  server = spawn('./floop', ['graph', '--format', 'html', '--serve'], {
    cwd: '/home/nvandessel/repos/feedback-loop/.worktrees/feature-electric-graph',
    env: { ...process.env, GOWORK: 'off' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  const urlPromise = new Promise((resolve) => {
    const handler = (data) => {
      const match = data.toString().match(/http:\/\/[^\s]+/);
      if (match) resolve(match[0]);
    };
    server.stderr.on('data', handler);
    server.stdout.on('data', handler);
  });

  const url = await Promise.race([
    urlPromise,
    new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 8000))
  ]);

  // Wait for server ready
  for (let i = 0; i < 20; i++) {
    try { const r = await fetch(url); if (r.ok) break; } catch {}
    await new Promise(r => setTimeout(r, 200));
  }

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  await page.goto(url, { waitUntil: 'networkidle' });
  await page.waitForFunction(() => window.__graph && window.__electricSim, { timeout: 10000 });

  // Wait for graph to settle
  console.log('Waiting for graph to settle...');
  await page.waitForTimeout(3000);

  let frame = 0;
  const shot = async () => {
    const padded = String(frame).padStart(3, '0');
    await page.screenshot({ path: `build/gif/frame_${padded}.png` });
    frame++;
  };

  // Phase 1: Show the normal graph (1.5 seconds at 5fps = 8 frames)
  console.log('Phase 1: Normal graph overview...');
  for (let i = 0; i < 8; i++) {
    await shot();
    await page.waitForTimeout(200);
  }

  // Phase 2: Click a node — use actual mouse click on canvas
  // Find a well-connected node near the center for best visual effect
  const nodeInfo = await page.evaluate(() => {
    const nodes = window.__graph.graphData().nodes;
    const links = window.__graph.graphData().links;
    // Count connections per node
    const deg = {};
    nodes.forEach(n => deg[n.id] = 0);
    links.forEach(l => {
      const s = l.source.id || l.source;
      const t = l.target.id || l.target;
      deg[s] = (deg[s] || 0) + 1;
      deg[t] = (deg[t] || 0) + 1;
    });
    // Pick highest-degree node (most dramatic spread)
    const sorted = nodes.slice().sort((a, b) => (deg[b.id] || 0) - (deg[a.id] || 0));
    const best = sorted[0];
    // Convert graph coords to screen coords
    const screen = window.__graph.graph2ScreenCoords(best.x, best.y);
    return { id: best.id, x: screen.x, y: screen.y, deg: deg[best.id] };
  });

  console.log(`Phase 2: Clicking node "${nodeInfo.id}" (${nodeInfo.deg} connections) at (${Math.round(nodeInfo.x)}, ${Math.round(nodeInfo.y)})...`);

  // Capture the click moment — mouse moves to node position
  await page.mouse.move(nodeInfo.x, nodeInfo.y);
  await shot(); // frame right before click
  await page.mouse.click(nodeInfo.x, nodeInfo.y);
  await page.waitForTimeout(100);
  // Close the detail panel and move mouse away to hide tooltip
  await page.evaluate(() => window.closePanel());
  await page.mouse.move(10, 10); // top-left corner, away from graph
  await page.waitForTimeout(100);
  await shot(); // frame right after click (panel closed, tooltip gone)

  // Phase 3: Zoom animation (auto-zoom takes ~800ms, capture at 5fps)
  console.log('Phase 3: Auto-zoom to neighborhood...');
  for (let i = 0; i < 6; i++) {
    await page.waitForTimeout(200);
    await shot();
  }

  // Phase 4: Full animation cycle (10 seconds at 5fps = 50 frames)
  console.log('Phase 4: Electric mode animation...');
  for (let i = 0; i < 50; i++) {
    await page.waitForTimeout(200);
    await shot();
    if (i % 10 === 0) console.log(`  Frame ${frame}/${8 + 2 + 6 + 50}`);
  }

  console.log(`Total frames captured: ${frame}`);

  console.log('Assembling GIF...');
  execSync('ffmpeg -y -framerate 5 -i build/gif/frame_%03d.png -vf "fps=5,scale=800:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" build/electric-mode.gif 2>/dev/null');
  console.log('GIF saved: build/electric-mode.gif');
}

main()
  .catch(err => console.error('Error:', err))
  .finally(() => {
    if (browser) browser.close();
    if (server) server.kill();
  });
