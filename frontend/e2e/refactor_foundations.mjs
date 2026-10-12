import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const baseline = process.argv.includes('--baseline');
const report = [];
const prefix = 'refactor-' + crypto.randomUUID();

await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const accounts = await api('GET', '/accounts');
  const forest = await api('GET', '/categories');
  const account = accounts.find((a) => a.type === 'asset' && a.status === 'active');
  const category = forest.flatMap((w) => w.children ?? []).flatMap((r) => r.children?.length ? r.children : [r]).find((c) => c.kind === 'expense');
  assert(account && category);
  const expense = await api('POST', '/transactions', { kind: 'expense', amount: '100', account_id: account.id, category_id: category.id, entry_date: '2026-01-02', note: prefix });
  cleanup(() => api('DELETE', `/transactions/${expense.id}`));
  const refund = await api('POST', '/transactions', { kind: 'refund', amount: '20', account_id: account.id, category_id: category.id, related_txn_id: expense.id, entry_date: '2026-02-02', note: prefix + '-refund' });
  cleanup(() => api('DELETE', `/transactions/${refund.id}`));
  const scope = await api('GET', '/budgets');
  cleanup(() => api('PUT', `/budgets/${scope.period_key}`, { total: scope.total.budget, items: scope.categories.filter((c) => c.budget).map((c) => ({ category_id: c.category_id, amount: c.budget })) }));
  await api('PUT', `/budgets/${scope.period_key}`, { total: '8000', items: [] });

  async function check(name, run) {
    const only = process.argv.find((argument) => argument.startsWith('--only='))?.slice(7);
    if (only && only !== name) return;
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    try {
      await run(page, context);
      report.push({ name, passed: true });
      console.log('PASS ' + name);
    } catch (error) {
      await shot(page, (baseline ? 'baseline-' : 'failed-') + name);
      report.push({ name, passed: false, error: error.message });
      console.log((baseline ? 'BASELINE FAIL ' : 'FAIL ') + name + ': ' + error.message);
    } finally { await context.close(); }
  }

  await check('login-return-url', async (page) => {
    const target = '/records?kind=refund&page=2#history';
    await page.goto(base + target);
    await page.getByPlaceholder('用户名').fill('voucher-admin');
    await page.getByPlaceholder('密码').fill('voucher-test-only');
    await page.getByRole('button', { name: /登\s*录/ }).click();
    await page.waitForURL((url) => url.pathname.endsWith('/records'));
    assert.equal(page.url(), base + target);
  });
  await check('viewer-entry-policy', async (page) => {
    await login(page, base, 'voucher-viewer');
    assert.equal(await page.getByRole('button', { name: /快捷记账$/ }).count(), 0);
    await page.goto(base + '/records');
    await page.locator('.page h2').first().waitFor();
    assert.equal(await page.getByRole('button', { name: /记一笔$/ }).count(), 0);
    assert.equal(await page.getByRole('button', { name: '导出', exact: true }).count(), 1);
    const signed = await page.request.post(base + '/api/v1/auth/login', { data: { username: 'voucher-viewer', password: 'voucher-test-only' } });
    const token = (await signed.json()).data.access_token;
    const denied = await page.request.post(base + '/api/v1/transactions', { headers: { Authorization: 'Bearer ' + token }, data: { kind: 'expense', amount: '1', account_id: account.id, category_id: category.id, entry_date: '2026-10-12' } });
    assert.equal(denied.status(), 403, 'server must enforce the same financial permission');
  });
  await check('notifications-error', async (page) => {
    await page.route('**/api/v1/notifications', (route) => route.fulfill({ status: 500, json: { code: 20000, message: '测试提醒读取失败' } }));
    await login(page, base);
    await page.goto(base + '/notifications');
    await page.getByText('测试提醒读取失败', { exact: true }).waitFor();
    assert.equal(await page.getByText('暂无提醒', { exact: true }).count(), 0);
    await page.unroute('**/api/v1/notifications');
    await page.getByRole('button', { name: /重\s*试/ }).click();
    await page.getByText('暂无提醒', { exact: true }).waitFor();
  });
  await check('csv-related-refunds', async (page) => {
    await login(page, base);
    await page.goto(base + `/records?related_txn_id=${expense.id}`);
    const downloaded = page.waitForEvent('download');
    await page.getByRole('button', { name: '导出', exact: true }).click();
    const download = await downloaded;
    const output = await download.path();
    const bytes = fs.readFileSync(output);
    assert.deepEqual([...bytes.subarray(0, 3)], [239, 187, 191]);
    const rows = bytes.toString('utf8').trim().split(/\r?\n/);
    assert.equal(rows.length, 2, 'only the related refund should be exported');
    assert(rows[1].includes(prefix + '-refund'));
  });
  await check('refresh-expired-token', async (page) => {
    await login(page, base);
    await page.waitForTimeout(300);
    let refreshes = 0;
    page.on('request', (request) => { if (request.url().endsWith('/auth/refresh')) refreshes++; });
    let requests = 0;
    await page.route('**/api/v1/accounts', async (route) => {
      if (++requests === 1) await route.fulfill({ status: 401, json: { code: 10001, message: '测试 token 过期' } });
      else await route.continue();
    });
    await page.getByRole('menuitem', { name: /我的/ }).click();
    await page.getByRole('button', { name: /账户管理/ }).click();
    await until(() => requests, (n) => n >= 2, 'authenticated request retry');
    assert.equal(refreshes, 1, '401 must actually refresh');
  });
  await check('budget-draft-refresh', async (page) => {
    await login(page, base);
    await page.goto(base + '/budgets');
    const total = page.locator('[data-budget="total"] input');
    await total.waitFor();
    await total.fill('9000');
    // Network reconnection is a real React Query background refresh trigger.
    await api('PUT', `/budgets/${scope.period_key}`, { total: '8100', items: [] });
    const reread = page.waitForResponse((response) => response.url().includes('/api/v1/budgets') && response.request().method() === 'GET');
    await page.evaluate(() => { window.dispatchEvent(new Event('offline')); window.dispatchEvent(new Event('online')); });
    await reread;
    await page.waitForTimeout(100);
    assert.equal(await total.inputValue(), '9000');
  });
  await check('custom-range-rehydration', async (page) => {
    await login(page, base);
    await page.goto(base + '/records?from=2026-01-01&to=2026-02-28');
    await page.locator('.period-picker .ant-picker input').first().waitFor();
    assert.equal(await page.locator('.period-picker .ant-picker input').nth(0).inputValue(), '2026-01-01');
    assert.equal(await page.locator('.period-picker .ant-picker input').nth(1).inputValue(), '2026-02-28');
  });
  await check('fixture-resource-cleanup', async () => {
    const row = await api('POST', '/categories', { name: prefix, kind: 'expense', sort: 0 });
    await api('DELETE', `/categories/${row.id}`);
    const remaining = await api('GET', '/categories');
    assert(!JSON.stringify(remaining).includes(prefix));
  });
  for (const width of process.argv.some((argument) => argument.startsWith('--only=')) ? [] : [360, 1280]) {
    const context = await browser.newContext({ viewport: { width, height: 900 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    const page = await context.newPage();
    await login(page, base);
    for (const target of ['/', '/records', '/stats', '/liabilities']) {
      await page.goto(base + target, { waitUntil: 'domcontentloaded' });
      await page.locator('.page h2').first().waitFor();
      await page.waitForTimeout(300);
      await shot(page, `${baseline ? 'baseline' : 'current'}-${width}-${target.replaceAll('/', '') || 'home'}`);
    }
    await context.close();
  }
});
const folder = path.resolve('e2e/shots/refactor-frontend');
fs.mkdirSync(folder, { recursive: true });
fs.writeFileSync(path.join(folder, baseline ? 'baseline.json' : 'foundations.json'), JSON.stringify({ base, node: process.version, report }, null, 2));
if (!baseline) assert(report.every((item) => item.passed), 'foundation regressions failed; see report');
else assert(report.some((item) => !item.passed), 'baseline must demonstrate the existing regressions');
