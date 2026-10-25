import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.BUDGET_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser, cleanup) => {
  const clients = [];
  for (const [index, username] of ['voucher-admin', 'voucher-recorder', 'voucher-member', 'voucher-viewer'].entries()) {
    const api = await fixtureApi(base, username);
    const original = await api('GET', '/budgets');
    const period = original.period_key;
    cleanup(() => api('PUT', `/budgets/${period}`, {
      total: original.total.budget,
      items: original.categories.filter((row) => row.budget).map((row) => ({ category_id: row.category_id, amount: row.budget })),
    }));
    assert.equal(original.total.budget, null, 'new account must not inherit another account total');
    assert(original.categories.every((row) => row.budget === null));
    const category = original.categories[0].category_id;
    const total = String(8000 + index * 1000);
    const amount = String(3000 + index * 1000);
    const input = { total, items: [{ category_id: category, amount }], owner_id: 999 };
    await api('PUT', `/budgets/${period}`, input);
    clients.push({ api, username, period, category, total, amount, input });
  }
  for (const client of clients) {
    const view = await client.api('GET', `/budgets?period_key=${client.period}&owner_id=999`);
    assert.equal(view.total.budget, client.total + '.00');
    assert.equal(view.categories.find((row) => row.category_id === client.category).budget, client.amount + '.00');
    assert(!view.categories.some((row) => clients.some((other) => other !== client && other.category === row.category_id)));
  }
  assert.equal((await fetch(base + '/api/v1/budgets')).status, 401);
  assert.equal((await fetch(base + '/api/v1/budgets/2026-10', { method: 'PUT', headers: { 'content-type': 'application/json' }, body: '{}' })).status, 401);
  console.log('PASS all four roles configure independent totals and categories; spoofed owner ignored; anonymous requests denied');

  const admin = clients[0];
  const viewer = clients[3];
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, timezoneId: 'Asia/Shanghai' });
  const page = await context.newPage();
  try {
    await login(page, base, admin.username);
    await page.goto(base + '/budgets');
    const totalInput = page.locator('[data-budget="total"] input');
    await until(() => totalInput.inputValue(), (value) => Number(value) === 8000, 'admin total');
    assert.equal(await page.locator('.budget-name').first().innerText(), '总预算');
    await totalInput.fill('8500');
    const saved = page.waitForResponse((response) => response.request().method() === 'PUT' && response.url().includes('/budgets/'));
    await page.getByRole('button', { name: /保\s*存/ }).click();
    assert.equal((await saved).status(), 200);
    await page.getByText('有未保存修改').waitFor({ state: 'hidden' });
    assert.equal((await admin.api('GET', '/budgets')).total.budget, '8500.00');
    assert.equal((await viewer.api('GET', '/budgets')).total.budget, '11000.00');
    await shot(page, 'budgets-personal-desktop');
    for (const route of ['/', '/stats']) {
      await page.goto(base + route);
      const card = page.locator('.ant-card', { hasText: '预算进度' }).first();
      await until(() => card.innerText(), (text) => text.includes('8500.00'), route + ' personal total');
      assert(!(await card.innerText()).includes('11000.00'));
    }
    await page.goto(base + '/mine');
    await page.getByRole('button', { name: '退出登录' }).click();
    await page.waitForURL(/\/login$/);
    await login(page, base, viewer.username);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(base + '/mine');
    await page.getByText('预算管理', { exact: true }).click();
    await page.waitForURL(/\/budgets$/);
    await until(() => totalInput.inputValue(), (value) => Number(value) === 11000, 'switch login does not reuse admin draft');
    await totalInput.fill('12000');
    await page.getByRole('button', { name: /保\s*存/ }).click();
    await page.getByText('有未保存修改').waitFor({ state: 'hidden' });
    assert.equal((await viewer.api('GET', '/budgets')).total.budget, '12000.00');
    assert.equal((await admin.api('GET', '/budgets')).total.budget, '8500.00');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page must fit viewport');
    await shot(page, 'budgets-personal-mobile');
    console.log('PASS desktop and mobile save; home/statistics progress; same-browser login switch; viewer self-service');

    const auth = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username: admin.username, password: 'voucher-test-only' }) });
    const token = (await auth.json()).data.access_token;
    const rejected = await fetch(base + `/api/v1/budgets/${admin.period}`, { method: 'PUT', headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' }, body: JSON.stringify({ total: '1', items: [{ category_id: viewer.category, amount: '1' }] }) });
    assert.equal(rejected.status, 400, 'admin cannot configure foreign categories');
    assert.equal((await admin.api('GET', '/budgets')).total.budget, '8500.00', 'rejected update must be atomic');

    const accounts = await admin.api('GET', '/accounts');
    const account = accounts.find((row) => row.type === 'asset');
    const entryDate = admin.period + '-01';
    const transaction = await admin.api('POST', '/transactions', { kind: 'expense', amount: '100', account_id: account.id, category_id: admin.category, entry_date: entryDate });
    cleanup(() => admin.api('DELETE', `/transactions/${transaction.id}`));
    const low = { total: '80', items: [{ category_id: admin.category, amount: '50' }] };
    await admin.api('PUT', `/budgets/${admin.period}`, low);
    await admin.api('PUT', `/budgets/${admin.period}`, low);
    const progress = await admin.api('GET', '/budgets');
    assert.equal(progress.total.consumed, '100.00');
    assert.equal(progress.total.over_amount, '20.00');
    assert.equal(progress.categories.find((row) => row.category_id === admin.category).over_amount, '50.00');
    assert.equal((await admin.api('GET', '/notifications')).list.filter((row) => row.kind === 'budget_overdue').length, 2);
    for (const other of clients.slice(1)) {
      assert.equal((await other.api('GET', '/budgets')).total.consumed, '0.00');
      assert.equal((await other.api('GET', '/notifications')).list.filter((row) => row.kind === 'budget_overdue').length, 0);
    }
    await admin.api('PUT', `/budgets/${admin.period}`, {});
    assert.equal((await admin.api('GET', '/budgets')).total.budget, null);
    assert.equal((await viewer.api('GET', '/budgets')).total.budget, '12000.00');
    console.log('PASS foreign-category rejection; personal consumption and notification deduplication; clearing one account preserves others');
  } finally { await context.close(); }
}, 'chromium');
