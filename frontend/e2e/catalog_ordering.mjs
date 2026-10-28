import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import path from 'node:path';
import { withFixture, login, fixtureApi, until, shot } from './support/fixture.mjs';

const base = process.env.CATALOG_E2E_BASE ?? 'http://127.0.0.1:5616';
const prefix = `排序验收-${Date.now()}`;
const ids = (rows) => rows.map((row) => row.id);
const moved = (rows) => [rows[1].id, rows[0].id, ...rows.slice(2).map((row) => row.id)];
const flattenRoots = (forest, kind = 'expense') => forest.flatMap((tree) => tree.children ?? []).filter((row) => row.kind === kind);
const payload = (rows, target = moved(rows)) => ({ previous_ids: ids(rows), ordered_ids: target, request_key: randomUUID() });

async function keyboardMove(page, handles, cancel = false) {
  await handles.nth(1).scrollIntoViewIfNeeded();
  const source = await handles.nth(1).boundingBox();
  const target = await handles.first().boundingBox();
  await handles.nth(1).focus();
  await page.keyboard.press('Space');
  await page.waitForTimeout(180);
  await page.keyboard.press(Math.abs(source.x - target.x) > Math.abs(source.y - target.y) ? 'ArrowLeft' : 'ArrowUp');
  await page.waitForTimeout(180);
  await page.keyboard.press(cancel ? 'Escape' : 'Space');
  await page.waitForTimeout(400);
}
async function mouseMove(page, handles) {
  await handles.nth(1).scrollIntoViewIfNeeded();
  await handles.nth(1).evaluate((element) => element.scrollIntoView({ block: 'center', behavior: 'instant' }));
  await page.waitForTimeout(100);
  const from = await handles.nth(1).boundingBox();
  const to = await handles.first().locator('xpath=ancestor::*[@data-order-id][1]').boundingBox();
  assert(from && to);
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + from.width / 2 + 8, from.y + from.height / 2, { steps: 3 });
  const targetY = Math.max(12, Math.min(page.viewportSize().height - 80, to.y + 25));
  await page.mouse.move(to.x + 28, targetY, { steps: 12 });
  await page.waitForTimeout(200);
  await page.mouse.up();
}
async function touchMove(page, handles) {
  await handles.nth(1).scrollIntoViewIfNeeded();
  await handles.nth(1).evaluate((element) => element.scrollIntoView({ block: 'center', behavior: 'instant' }));
  await page.waitForTimeout(100);
  const from = await handles.nth(1).boundingBox();
  const to = await handles.first().locator('xpath=ancestor::*[@data-order-id][1]').boundingBox();
  assert(from && to);
  const cdp = await page.context().newCDPSession(page);
  const point = { x: from.x + from.width / 2, y: from.y + from.height / 2 };
  const targetY = Math.max(12, Math.min(page.viewportSize().height - 80, to.y + 25));
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [point] });
  await page.waitForTimeout(300);
  for (let step = 1; step <= 12; step++) {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{
      x: point.x + (to.x + 28 - point.x) * step / 12,
      y: point.y + (targetY - point.y) * step / 12,
    }] });
    await page.waitForTimeout(20);
  }
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await cdp.detach();
}

await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const roots = [];
  for (const name of ['餐饮', '交通', '其他']) {
    const row = await api('POST', '/categories', { kind: 'expense', name: `${prefix}-${name}` });
    roots.push(row);
    cleanup(() => api('DELETE', `/categories/${row.id}`));
  }
  for (let index = 0; index < 20; index++) {
    const row = await api('POST', '/categories', { kind: 'expense', parent_id: roots[0].id, name: `${prefix}-小项${index}` });
    cleanup(() => api('DELETE', `/categories/${row.id}`));
  }
  for (const name of ['收入甲', '收入乙']) {
    const row = await api('POST', '/categories', { kind: 'income', name: `${prefix}-${name}` });
    cleanup(() => api('DELETE', `/categories/${row.id}`));
  }
  for (const name of ['Z常用', 'A后来']) {
    const row = await api('POST', '/voucher-platforms', { name: `${prefix}-${name}` });
    cleanup(() => api('DELETE', `/voucher-platforms/${row.id}`));
  }
  for (const input of [{ name: `${prefix}-现金`, type: 'asset' }, { name: `${prefix}-基金`, type: 'asset', asset_class: 'fund' }, { name: `${prefix}-停用`, type: 'asset' }]) {
    const row = await api('POST', '/accounts', { ...input, request_key: randomUUID() });
    if (input.name.endsWith('停用')) await api('PUT', `/accounts/${row.id}`, { ...input, opening_balance: '0', status: 'disabled' });
    cleanup(() => api('DELETE', `/accounts/${row.id}`));
  }
  // HTTP status/role/ownership checks also exercise the registered static reorder routes.
  for (const role of ['admin', 'recorder', 'member', 'viewer']) {
    const own = await fixtureApi(base, 'voucher-' + role);
    const request = async (resource, data, expected) => own('PUT', '/' + resource + '/reorder', data, expected);
    for (const resource of ['accounts', 'voucher-platforms', 'categories']) {
      const all = await own('GET', `/${resource}`);
      const rows = resource === 'categories' ? flattenRoots(all) : all;
      const extra = resource === 'categories' ? { kind: 'expense', parent_id: null } : {};
      await request(resource, { ...payload(rows, ids(rows)), ...extra }, 200);
      await request(resource, { ...payload(rows, ids(rows)), ordered_ids: [rows[0].id, rows[0].id], previous_ids: [rows[0].id, rows[0].id], ...extra }, 400);
      if (rows.length > 1) await request(resource, { ...payload(rows.slice(0, -1), ids(rows.slice(0, -1))), ...extra }, 409);
      await request(resource, { ordered_ids: [9999999], previous_ids: [9999999], request_key: randomUUID(), ...extra }, 404);
      if (role !== 'admin') {
        const foreign = resource === 'categories' ? roots[0].id : (await api('GET', `/${resource}`))[0].id;
        await request(resource, { ordered_ids: [foreign], previous_ids: [foreign], request_key: randomUUID(), ...extra }, 403);
      }
    }
  }
  const unauthorized = await fetch(base + '/api/v1/accounts/reorder', { method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ ordered_ids: [], previous_ids: [], request_key: randomUUID() }) });
  assert.equal(unauthorized.status, 401);
  console.log('PASS all roles, full lists, invalid/foreign/missing IDs and authentication');

  const errors = [];
  for (const width of [1280, 390, 360, 767]) {
    const context = await browser.newContext({ viewport: { width, height: 884 }, hasTouch: width < 768, colorScheme: width === 360 ? 'dark' : 'light', reducedMotion: width === 767 ? 'reduce' : 'no-preference' });
    const page = await context.newPage();
    page.on('pageerror', (error) => errors.push(error.message));
    await login(page, base);
    for (const [path, resource] of [['/platforms', 'voucher-platforms'], ['/accounts', 'accounts'], ['/categories', 'categories']]) {
      await page.goto(base + path);
      const list = resource === 'categories' ? page.locator('.cat-section').first() : resource === 'accounts' ? page.locator(width < 768 ? '.acct-list' : '.ant-table-tbody') : page.locator('.platform-list');
      const handles = list.getByRole('button', { name: /拖动排序/ });
      await handles.nth(1).waitFor();
      const read = async () => { const rows = await api('GET', `/${resource}`); return resource === 'categories' ? flattenRoots(rows) : rows; };
      let before = await read();
      let writes = 0;
      const count = (request) => { if (request.url().endsWith(`/${resource}/reorder`) && request.method() === 'PUT') writes++; };
      page.on('request', count);
      await keyboardMove(page, handles, true);
      await page.waitForTimeout(250);
      assert.equal(writes, 0, 'cancel must not save');
      assert.deepEqual(ids(await read()), ids(before));
      await keyboardMove(page, handles);
      await until(read, (rows) => JSON.stringify(ids(rows)) === JSON.stringify(moved(before)), `keyboard ${resource} ${width}`);
      await until(() => handles.first().isEnabled(), Boolean, 'sorting complete');
      assert.equal(writes, 1, 'one drag must send one request');
      before = await read();
      await page.waitForTimeout(450);
      if (width < 768) {
        const box = await handles.first().boundingBox();
        assert(box.width >= 44 && box.height >= 44, 'touch target');
        await touchMove(page, handles);
      } else await mouseMove(page, handles);
      await until(async () => ids(await read()), (rows) => JSON.stringify(rows) === JSON.stringify(moved(before)), `pointer ${resource} ${width}`);
      await until(() => handles.first().isEnabled(), Boolean, 'pointer save complete');
      assert.equal(writes, 2);
      page.off('request', count);
      await page.reload();
      await handles.first().waitFor();
      assert.deepEqual(ids(await read()), moved(before));
      assert.equal(await page.getByRole('dialog').count(), 0, 'sorting must not open editor');
      if (resource === 'voucher-platforms') {
        await page.getByPlaceholder('搜索平台 / 商家').fill('Z常用');
        await page.getByText('清空搜索后可拖动排序').waitFor();
        assert(await page.getByRole('button', { name: /拖动排序/ }).first().isDisabled());
        await page.getByPlaceholder('搜索平台 / 商家').fill('');
        await until(() => handles.first().isEnabled(), Boolean, 'search cleared');
      }
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, 'horizontal overflow');
      await shot(page, `catalog-order-${resource}-${width}`);
    }
    await page.getByRole('button', { name: `管理子分类 ${roots[0].name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: `子分类 · ${roots[0].name}`, exact: true });
    await dialog.waitFor();
    assert(!(await page.locator(':focus').evaluate((element) => element.matches('input'))), 'modal must not focus text');
    const children = () => api('GET', '/categories').then((forest) => flattenRoots(forest).find((row) => row.id === roots[0].id).children);
    const handles = dialog.getByRole('button', { name: /拖动排序/ });
    const before = await children();
    await keyboardMove(page, handles, true);
    assert(await dialog.isVisible(), 'Escape during drag closed parent dialog');
    assert.deepEqual(ids(await children()), ids(before));
    await keyboardMove(page, handles);
    await until(children, (rows) => JSON.stringify(ids(rows)) === JSON.stringify(moved(before)), 'child sort');
    await until(() => handles.first().isEnabled(), Boolean, 'child saved');
    // Reach beyond the visible child list using the actual mouse drag and cancel without a write.
    await page.waitForTimeout(450);
    const scroll = dialog.locator('.cat-child-list');
    const firstBox = await handles.first().boundingBox();
    const scrollBox = await scroll.boundingBox();
    await page.mouse.move(firstBox.x + firstBox.width / 2, firstBox.y + firstBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(firstBox.x + firstBox.width / 2 + 8, firstBox.y + firstBox.height / 2);
    await page.mouse.move(scrollBox.x + 25, scrollBox.y + scrollBox.height - 6, { steps: 14 });
    await page.waitForTimeout(800);
    assert(await scroll.evaluate((element) => element.scrollTop) > 0, 'modal edge did not autoscroll');
    await page.keyboard.press('Escape'); await page.mouse.up();
    await page.waitForTimeout(450);
    assert(await dialog.isVisible());
    await scroll.evaluate((element) => { element.scrollTop = 0; });
    await page.screenshot({ path: path.resolve(`e2e/shots/refactor-frontend/catalog-order-children-${width}.png`) });
    await dialog.getByRole('button', { name: /完\s*成/ }).click();
    await page.getByRole('button', { name: `管理子分类 ${roots[0].name}`, exact: true }).click();
    await dialog.waitFor();
    assert.deepEqual(ids(await children()), moved(before));
    await dialog.getByRole('button', { name: /完\s*成/ }).click();
    await context.close();
    console.log(`PASS keyboard, ${width < 768 ? 'touch' : 'mouse'}, persistence, child modal, autoscroll and layout ${width}px`);
  }

  // Saved on server, response lost: retry must reuse the original request key and order.
  const context = await browser.newContext({ viewport: { width: 390, height: 884 }, hasTouch: true });
  const page = await context.newPage();
  await login(page, base);
  await page.goto(base + '/platforms');
  const handles = page.locator('.platform-list').getByRole('button', { name: /拖动排序/ });
  await handles.nth(1).waitFor();
  const before = await api('GET', '/voucher-platforms');
  let lost;
  await page.route('**/voucher-platforms/reorder', async (route) => {
    lost = route.request().postDataJSON();
    await route.fetch();
    await route.abort('failed');
  });
  await keyboardMove(page, handles);
  await page.getByRole('button', { name: '重试保存', exact: true }).waitFor();
  assert.deepEqual(ids(await api('GET', '/voucher-platforms')), moved(before));
  assert.deepEqual(await page.locator('.platform-row').evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId))), ids(before), 'failure must restore confirmed UI order');
  await page.unroute('**/voucher-platforms/reorder');
  let retry;
  page.on('request', (request) => { if (request.url().endsWith('/voucher-platforms/reorder')) retry = request.postDataJSON(); });
  await page.getByRole('button', { name: '重试保存', exact: true }).click();
  await until(() => page.getByRole('button', { name: '重试保存', exact: true }).count(), (count) => count === 0, 'retry completes');
  assert.deepEqual(retry, lost, 'retry changed saved request');
  // Stale device snapshot must show conflict and refresh the newer order.
  const baseline = await api('GET', '/voucher-platforms');
  const newer = [...ids(baseline).slice(1), baseline[0].id];
  await api('PUT', '/voucher-platforms/reorder', payload(baseline, newer));
  await keyboardMove(page, handles);
  await page.getByText('列表已变化，请刷新后重新排序', { exact: true }).waitFor();
  await page.getByRole('button', { name: '刷新列表', exact: true }).click();
  await until(() => page.locator('.platform-row').evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId))), (rows) => JSON.stringify(rows) === JSON.stringify(newer), 'conflict refresh');
  console.log('PASS response loss/retry preserves key, UI rollback and stale device 409 recovery');

  // A late sort response after switching accounts cannot touch the new account.
  let release;
  let held = false;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route('**/voucher-platforms/reorder', async (route) => { const response = await route.fetch(); held = true; await gate; await route.fulfill({ response }); });
  await keyboardMove(page, handles);
  await until(() => held, Boolean, 'late save committed');
  await page.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).click();
  await page.getByRole('button', { name: /退出所有设备$/ }).click();
  await page.getByPlaceholder('用户名').fill('voucher-viewer');
  await page.getByPlaceholder('密码').fill('voucher-test-only');
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((url) => !url.pathname.endsWith('/login'));
  await page.goto(base + '/platforms');
  await page.locator('.platform-list').waitFor();
  const viewer = await fixtureApi(base, 'voucher-viewer');
  const viewerBefore = await viewer('GET', '/voucher-platforms');
  release(); await page.waitForTimeout(600);
  assert.deepEqual(await page.locator('.platform-row').evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId))), ids(viewerBefore));
  assert.equal(await page.getByRole('button', { name: '重试保存', exact: true }).count(), 0);
  await context.close();
  assert.deepEqual(errors, [], 'browser runtime errors');
  console.log('PASS late save after account switch; no browser runtime errors');
}, 'chromium');
