import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, fixtureApi, login, until, shot } from './support/fixture.mjs';

const base = process.env.TRANSACTION_ORDER_E2E_BASE ?? 'http://127.0.0.1:5625';
const rowIDs = (page, selector) => page.locator(selector).evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId)));
const saved = (response) => response.url().endsWith('/transactions/reorder') && response.request().method() === 'PUT';
const swapped = (ids) => [ids[1], ids[0], ...ids.slice(2)];

async function keyboardMove(page, handles, cancel = false) {
  await until(() => handles.nth(1).isEnabled(), Boolean, 'sorting ready');
  await page.waitForTimeout(300);
  await handles.nth(1).scrollIntoViewIfNeeded();
  await handles.nth(1).focus();
  await page.keyboard.press('Space');
  await page.waitForTimeout(180);
  await page.keyboard.press('ArrowUp');
  await page.waitForTimeout(180);
  await page.keyboard.press(cancel ? 'Escape' : 'Space');
}

async function pointerMove(page, handles, touch = false) {
  await until(() => handles.nth(1).isEnabled(), Boolean, 'sorting ready');
  await page.waitForTimeout(300);
  await handles.nth(1).evaluate((element) => element.scrollIntoView({ block: 'center', behavior: 'instant' }));
  const from = await handles.nth(1).boundingBox();
  const to = await handles.first().boundingBox();
  assert(from && to);
  const point = { x: from.x + from.width / 2, y: from.y + from.height / 2 };
  if (touch) {
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [point] });
    await page.waitForTimeout(300);
    for (let i = 1; i <= 12; i++) {
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: point.x, y: point.y + (to.y + to.height / 2 - point.y) * i / 12 }] });
      await page.waitForTimeout(25);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await cdp.detach();
  } else {
    await page.mouse.move(point.x, point.y);
    await page.mouse.down();
    await page.mouse.move(point.x + 8, point.y, { steps: 3 });
    await page.mouse.move(point.x, to.y + to.height / 2, { steps: 12 });
    await page.waitForTimeout(180);
    await page.mouse.up();
  }
}

await withFixture(base, async (browser) => {
  const api = await fixtureApi(base);
  const account = await api('POST', '/accounts', { name: '日常银行卡', type: 'asset', opening_balance: '1000', request_key: randomUUID() });
  const root = await api('POST', '/categories', { kind: 'expense', name: '日常消费' });
  const category = await api('POST', '/categories', { kind: 'expense', parent_id: root.id, name: '餐饮' });
  const today = new Date();
  const month = today.toISOString().slice(0, 7);
  for (let i = 0; i < 24; i++) {
    await api('POST', '/transactions', { kind: 'expense', amount: String(i + 1), account_id: account.id, category_id: category.id, entry_date: `${month}-${String(Math.min(i + 1, today.getDate())).padStart(2, '0')}`, note: ['早餐', '午餐', '晚餐', '咖啡'][i % 4] });
  }
  const read = () => api('GET', '/transactions?order=custom&page_size=100');
  let expected = (await read()).items.map((row) => row.id);
  const mobile = await browser.newContext({ viewport: { width: 427, height: 884 }, isMobile: true, hasTouch: true });
  const page = await mobile.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await login(page, base);
  await until(() => rowIDs(page, '.recent-row'), (ids) => ids.length === 10, 'home rows');
  const dates = await page.locator('.recent-date').allTextContents();
  assert.equal(dates.length, 10);
  assert(dates.every((date) => /^\d{4}-\d{2}-\d{2}$/.test(date.trim())));
  const mobileHandles = page.locator('.recent-row [data-sort-handle]');
  const touchSave = page.waitForResponse(saved);
  await pointerMove(page, mobileHandles, true);
  assert.equal((await touchSave).status(), 200);
  expected = swapped(expected);
  await until(read, (data) => data.items[0].id === expected[0], 'touch order saved');
  await page.reload();
  await until(() => rowIDs(page, '.recent-row'), (ids) => ids[0] === expected[0], 'refresh retains home order');
  await shot(page, 'transaction-order-home-mobile');
  await page.locator('.recent-content').first().click();
  await page.waitForURL(/\/records/);
  await until(() => rowIDs(page, '.txn-row'), (ids) => ids.length >= 20 && ids[0] === expected[0], 'records share home order');
  await page.waitForTimeout(600);
  const recordHandles = page.locator('.txn-row [data-sort-handle]');
  const keySave = page.waitForResponse(saved);
  await keyboardMove(page, recordHandles);
  assert.equal((await keySave).status(), 200);
  expected = swapped(expected);
  await until(read, (data) => data.items[0].id === expected[0], 'keyboard order saved');
  await until(() => rowIDs(page, '.txn-row'), (ids) => ids[0] === expected[0], 'records settled');
  let requests = 0;
  page.on('request', (request) => { if (request.url().endsWith('/transactions/reorder')) requests++; });
  await keyboardMove(page, recordHandles, true);
  await page.waitForTimeout(350);
  assert.equal(requests, 0, 'cancel must not save');
  const beforeFailure = await rowIDs(page, '.txn-row');
  const keys = [];
  let reject = true;
  await page.route('**/transactions/reorder', async (route) => {
    keys.push(route.request().postDataJSON().request_key);
    if (reject) {
      reject = false;
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 500, message: '排序保存测试失败' }) });
    } else await route.continue();
  });
  const failedSave = page.waitForResponse(saved);
  await keyboardMove(page, recordHandles);
  assert.equal((await failedSave).status(), 500);
  await page.getByRole('button', { name: '重试保存' }).waitFor();
  await until(() => rowIDs(page, '.txn-row'), (ids) => JSON.stringify(ids) === JSON.stringify(beforeFailure), 'failed save restores confirmed rows');
  const retry = page.waitForResponse(saved);
  await page.getByRole('button', { name: '重试保存' }).click();
  assert.equal((await retry).status(), 200);
  assert.equal(keys.length, 2);
  assert.equal(keys[0], keys[1], 'retry must reuse request key');
  expected = swapped(expected);
  await until(() => rowIDs(page, '.txn-row'), (ids) => ids[0] === expected[0], 'retry updates rows');
  await shot(page, 'transaction-order-records-mobile');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.goto(base + '/');
  await until(() => rowIDs(page, '.recent-row'), (ids) => ids[0] === expected[0], 'home shares records order');

  const desktop = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const desk = await desktop.newPage();
  desk.on('pageerror', (error) => errors.push(error.message));
  await login(desk, base);
  await desk.goto(base + '/records');
  await until(() => rowIDs(desk, 'tr[data-order-id]'), (ids) => ids.length === 20, 'desktop rows');
  const mouseSave = desk.waitForResponse(saved);
  await pointerMove(desk, desk.locator('tr[data-order-id] [data-sort-handle]'));
  assert.equal((await mouseSave).status(), 200);
  expected = swapped(expected);
  await until(read, (data) => data.items[0].id === expected[0], 'mouse order saved');
  await desk.reload();
  await until(() => rowIDs(desk, 'tr[data-order-id]'), (ids) => ids[0] === expected[0], 'desktop persisted');
  await shot(desk, 'transaction-order-records-desktop');
  await desk.goto(base + '/');
  await until(() => rowIDs(desk, '.recent-row'), (ids) => ids[0] === expected[0], 'desktop home sync');
  await shot(desk, 'transaction-order-home-desktop');
  assert.deepEqual(errors, []);
  await desktop.close();
  await mobile.close();
  console.log('Transaction dates and ordering passed: touch, mouse, keyboard, cancel, retry, refresh, shared views.');
}, 'chromium');
