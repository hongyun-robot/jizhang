import assert from 'node:assert/strict';
import { withFixture, login } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  try {
    await login(page, base);
    const target = base + '/records?kind=refund&page=2';
    await page.goto(target);
    await page.getByRole('menuitem', { name: /统计/ }).click();
    await page.getByRole('menuitem', { name: /流水/ }).click();
    assert.equal(page.url(), target);
    const history = await page.evaluate(() => history.length);
    await page.getByRole('menuitem', { name: /流水/ }).click();
    assert.equal(await page.evaluate(() => history.length), history);
    await page.getByRole('menuitem', { name: /我的/ }).click();
    await page.getByRole('button', { name: /退出所有设备/ }).click();
    await page.waitForURL((url) => url.pathname.endsWith('/login'));
    await login(page, base, 'voucher-viewer');
    await page.getByRole('menuitem', { name: /流水/ }).click();
    assert.equal(new URL(page.url()).search, '', 'previous session filters must be cleared');
    console.log('PASS tab restoration, repeated navigation, logout and account switch');
  } finally { await context.close(); }
});
