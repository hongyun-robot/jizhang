import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';
import { createServer } from 'vite';

// Isolated UI fixture: real Card, EChart, asset options and styles; no API or ledger writes.
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const shots = path.join(root, 'e2e/shots/stats-assets-tooltip');
const fixturePath = '/__stats_assets_tooltip.html';
const html = `<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<body><div id="fixture" style="width:min(1160px,calc(100% - 32px));margin:400px auto 16px"></div>
<script type="module">
import React from 'react';
import { createRoot } from 'react-dom/client';
import { Card } from 'antd';
import EChart from '/src/components/EChart.tsx';
import { assetOptions } from '/src/features/statistics/options.ts';
import '/src/styles.css';
const accounts = Array.from({ length: 24 }, (_, i) => ({
  account_id: i + 1, name: '测试账户' + String(i + 1).padStart(2, '0'),
  type: 'cash', balance: String((i + 1) * 100),
}));
const data = { start: '2026-11-01', end: '2026-11-02', ending_total: '30000',
  points: ['2026-11-01', '2026-11-02'].map(date => ({ date, total: '30000', accounts })) };
const option = assetOptions(data, { dark: false, isMobile: innerWidth < 768,
  TEAL: '#0a7568', GREEN: '#0f7a52', RED: '#c0392b',
  PALETTE: ['#0a7568', '#40aaa2', '#8a929c'], pieBorder: '#fff', prevBar: '#ddd' });
createRoot(document.getElementById('fixture')).render(
  React.createElement(Card, { size: 'small', className: 'panel', title: '资产走势' },
    React.createElement(EChart, { option, height: 280, ariaLabel: '资产走势' })));
</script></body></html>`;

const server = await createServer({
  root,
  server: { host: '127.0.0.1', port: 5621, strictPort: true },
  plugins: [{
    name: 'stats-assets-tooltip-fixture',
    configureServer(vite) {
      vite.middlewares.use(async (request, response, next) => {
        if (request.url !== fixturePath) return next();
        response.setHeader('Content-Type', 'text/html');
        response.end(await vite.transformIndexHtml(fixturePath, html));
      });
    },
  }],
});
let browser;
try {
  await server.listen();
  await fs.mkdir(shots, { recursive: true });
  browser = await chromium.launch({
    executablePath: process.env.CHROME_EXE ?? 'C:/Program Files/Google/Chrome/Application/chrome.exe',
    headless: true,
  });
  for (const [name, viewport] of [
    ['desktop', { width: 1920, height: 911 }],
    ['mobile', { width: 390, height: 844 }],
  ]) {
    const page = await browser.newPage({ viewport });
    await page.goto('http://127.0.0.1:5621' + fixturePath);
    const chart = page.getByRole('img', { name: '资产走势', exact: true });
    await chart.locator('canvas').waitFor();
    await chart.scrollIntoViewIfNeeded();
    const box = await chart.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    const tooltip = chart.locator(':scope > div').last();
    await tooltip.getByText('测试账户24', { exact: true }).waitFor();
    await page.screenshot({ path: path.join(shots, name + '.png'), fullPage: true });
    const bounds = await tooltip.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      let top = 0, left = 0, right = innerWidth, bottom = innerHeight;
      for (let parent = element.parentElement; parent; parent = parent.parentElement) {
        const style = getComputedStyle(parent);
        const parentRect = parent.getBoundingClientRect();
        if (/(hidden|clip|auto|scroll)/.test(style.overflowY)) {
          top = Math.max(top, parentRect.top);
          bottom = Math.min(bottom, parentRect.bottom);
        }
        if (/(hidden|clip|auto|scroll)/.test(style.overflowX)) {
          left = Math.max(left, parentRect.left);
          right = Math.min(right, parentRect.right);
        }
      }
      return { top: rect.top, left: rect.left, right: rect.right, bottom: rect.bottom,
        visible: { top, left, right, bottom }, scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight };
    });
    console.log(name, JSON.stringify(bounds));
    assert(bounds.top >= bounds.visible.top - 1 && bounds.bottom <= bounds.visible.bottom + 1,
      name + ': asset tooltip is vertically clipped');
    assert(bounds.left >= bounds.visible.left - 1 && bounds.right <= bounds.visible.right + 1,
      name + ': asset tooltip is horizontally clipped');
    assert(bounds.scrollHeight > bounds.clientHeight, name + ': long account list can scroll');
    const tooltipBox = await tooltip.boundingBox();
    await page.mouse.move(tooltipBox.x + tooltipBox.width / 2, tooltipBox.y + tooltipBox.height / 2);
    const pageY = await page.evaluate(() => scrollY);
    await page.mouse.wheel(0, 900);
    await page.waitForFunction((element) => element.scrollTop > 0, await tooltip.elementHandle());
    const lastRow = await tooltip.getByText('测试账户24', { exact: true }).boundingBox();
    assert(lastRow.y >= tooltipBox.y && lastRow.y + lastRow.height <= tooltipBox.y + tooltipBox.height,
      name + ': final account is readable after scrolling');
    assert.equal(await page.evaluate(() => scrollY), pageY, name + ': tooltip scroll keeps the page still');
    await page.screenshot({ path: path.join(shots, name + '-scrolled.png'), fullPage: true });
    console.log('PASS ' + name + ': all asset accounts remain accessible inside the card');
    await page.close();
  }
} finally {
  await browser?.close();
  await server.close();
}
