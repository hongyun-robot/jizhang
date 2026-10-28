import assert from 'node:assert/strict';
import { withFixture, login, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  for (const username of ['voucher-admin', 'voucher-viewer']) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    await context.addInitScript(() => {
      const original = window.setInterval.bind(window);
      window.setInterval = (callback, interval, ...args) => original(callback, interval === 60000 ? 150 : interval, ...args);
    });
    const page = await context.newPage();
    const counts = { vouchers: 0, liabilities: 0, recurring: 0, transactions: 0, budgets: 0, stats: 0 };
    await page.route('**/api/v1/**/ensure', async (route) => {
      const key = new URL(route.request().url()).pathname.split('/').at(-2);
      counts[key]++;
      if (key === 'vouchers' && counts.vouchers === 1) { await route.fulfill({ status: 500, json: { code: 20000, message: 'temporary test failure' } }); return; }
      await route.fulfill({ json: { code: 0, message: 'ok', data: { generated: 0, failed: key === 'vouchers' && counts.vouchers === 2 ? 1 : 0 } } });
    });
    page.on('request', (request) => {
      if (request.method() !== 'GET') return;
      const endpoint = new URL(request.url()).pathname;
      if (endpoint === '/api/v1/transactions') counts.transactions++;
      if (endpoint.startsWith('/api/v1/budgets')) counts.budgets++;
      if (endpoint.startsWith('/api/v1/stats')) counts.stats++;
    });
    await login(page, base, username);
    await page.locator('.hero').waitFor();
    await until(() => counts.liabilities, (count) => count >= 3, 'repeated reconciliation');
    const reads = { transactions: counts.transactions, budgets: counts.budgets, stats: counts.stats };
    await page.waitForTimeout(400);
    assert.deepEqual({ transactions: counts.transactions, budgets: counts.budgets, stats: counts.stats }, reads, 'no broad reload on no changes or targeted failure');
    assert.equal(counts.recurring, 1, 'recurring retains startup-only frequency');
    assert.equal(counts.vouchers > 1, username === 'voucher-admin', 'viewer skips voucher financial processing');
    await page.getByRole('menuitem', { name: /我的/ }).click();
    await page.getByRole('button', { name: /退出所有设备/ }).click();
    await page.waitForURL((url) => url.pathname.endsWith('/login'));
    const last = { ...counts };
    await page.waitForTimeout(400);
    assert.deepEqual(counts, last, 'logout stops all timers');
    await context.close();
  }
  console.log('PASS reconciliation request counts, zero changes, failure recovery, role policy and logout cleanup');
});
