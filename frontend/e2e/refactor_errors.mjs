import assert from 'node:assert/strict';
import { withFixture, login } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const pages = [
  ['/records', '/transactions*'],
  ['/accounts', '/accounts'], ['/categories', '/categories'], ['/budgets', '/budgets*'],
  ['/recurring', '/recurring'], ['/settings', '/settings'], ['/audit', '/audit-logs*'],
  ['/liabilities', '/liabilities'], ['/notifications', '/notifications'], ['/vouchers', '/vouchers*'],
  ['/stats', '/stats/category*'], ['/', '/stats/compare*'],
];
await withFixture(base, async (browser) => {
  for (const [target, endpoint] of pages) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    try {
      await login(page, base);
      const pattern = '**/api/v1' + endpoint;
      await page.route(pattern, async (route) => {
        if (route.request().method() === 'GET') await route.fulfill({ status: 500, json: { code: 20000, message: '隔离读取失败' } });
        else await route.continue();
      });
      await page.goto(base + target, { waitUntil: 'domcontentloaded' });
      await page.getByText('隔离读取失败', { exact: true }).first().waitFor();
      assert(await page.getByRole('button', { name: /重\s*试/ }).count() > 0, target + ' needs recovery');
      await page.unroute(pattern);
      await page.getByRole('button', { name: /重\s*试/ }).first().click();
      await page.getByText('隔离读取失败', { exact: true }).first().waitFor({ state: 'hidden' });
      console.log('PASS error and recovery ' + target);
    } finally { await context.close(); }
  }
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  try {
    await login(page, base);
    await page.goto(base + '/mine');
    await page.route('**/api/v1/users', (route) => route.fulfill({ status: 500, json: { code: 20000, message: '用户读取失败' } }));
    await page.getByRole('button', { name: /用户管理/ }).click();
    await page.getByText('用户读取失败', { exact: true }).waitFor();
    await page.unroute('**/api/v1/users');
    await page.getByRole('button', { name: /重\s*试/ }).click();
    await page.getByText('用户读取失败', { exact: true }).waitFor({ state: 'hidden' });
    console.log('PASS error and recovery user manager');
  } finally { await context.close(); }
});
