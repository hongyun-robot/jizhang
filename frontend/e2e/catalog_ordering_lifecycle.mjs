import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, login, fixtureApi, until } from './support/fixture.mjs';

const base = process.env.CATALOG_E2E_BASE ?? 'http://127.0.0.1:5616';
const prefix = `排序生命周期-${Date.now()}`;
const ids = (rows) => rows.map((row) => row.id);
const request = (rows, ordered) => ({ previous_ids: ids(rows), ordered_ids: ordered, request_key: randomUUID() });
const rootsOf = (forest) => forest.flatMap((tree) => tree.children ?? []);
async function sortByKeyboard(page, handles) {
  await handles.nth(1).focus();
  await page.keyboard.press('Space'); await page.waitForTimeout(180);
  await page.keyboard.press('ArrowUp'); await page.waitForTimeout(180);
  await page.keyboard.press('Space'); await page.waitForTimeout(400);
}

await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const credit = await api('POST', '/accounts', { request_key: randomUUID(), name: prefix, type: 'liability', opening_balance: '0', liability_type: 'credit_card', statement_day: 20, repayment_day: 25, repayment_month_offset: 0 });
  cleanup(() => api('PUT', `/accounts/${credit.id}`, { name: prefix + '-设置完成', type: 'liability', opening_balance: '0', status: 'disabled', liability_type: 'credit_card' }));
  let before = await api('GET', '/accounts');
  await api('PUT', '/accounts/reorder', request(before, [credit.id, ...ids(before).filter((id) => id !== credit.id)]));
  before = await api('GET', '/accounts');
  const context = await browser.newContext({ viewport: { width: 1280, height: 884 } });
  const page = await context.newPage();
  await login(page, base);
  await page.goto(base + '/accounts');
  await page.locator(`[data-order-id="${credit.id}"]`).getByRole('button', { name: /编\s*辑/ }).click();
  let editor = page.getByRole('dialog', { name: '编辑账户', exact: true });
  assert.equal(await editor.getByLabel('排序（小在前）').count(), 0);
  await editor.getByLabel('名称', { exact: true }).fill(prefix + '-改名');
  await editor.getByRole('button', { name: /保\s*存/ }).click();
  await editor.waitFor({ state: 'hidden' });
  assert.deepEqual(ids(await api('GET', '/accounts')), ids(before));
  await page.goto(base + `/liabilities/${credit.id}`);
  await page.getByRole('button', { name: '账户设置', exact: true }).click();
  editor = page.getByRole('dialog', { name: '账户设置', exact: true });
  await editor.getByLabel('账户名称', { exact: true }).fill(prefix + '-设置完成');
  await editor.getByRole('button', { name: /保\s*存/ }).click();
  await editor.waitFor({ state: 'hidden' });
  assert.deepEqual(ids(await api('GET', '/accounts')), ids(before));
  console.log('PASS account editor and personal liability settings preserve saved positions');

  const first = await api('POST', '/categories', { kind: 'expense', name: prefix + '-小项' });
  const second = await api('POST', '/categories', { kind: 'expense', name: prefix + '-空大类' });
  cleanup(() => api('DELETE', `/categories/${first.id}`));
  cleanup(() => api('DELETE', `/categories/${second.id}`));
  await page.goto(base + '/categories');
  await page.getByRole('button', { name: `管理子分类 ${second.name}`, exact: true }).click();
  let dialog = page.getByRole('dialog', { name: `子分类 · ${second.name}`, exact: true });
  await dialog.getByRole('button', { name: /新增子分类$/ }).click();
  const childEditor = page.getByRole('dialog', { name: '新建分类（子分类）', exact: true });
  await childEditor.getByPlaceholder('分类名称').fill('弹窗新小项');
  await childEditor.getByRole('button', { name: /确\s*定/ }).click();
  await childEditor.waitFor({ state: 'hidden' });
  const added = rootsOf(await api('GET', '/categories')).find((row) => row.id === second.id).children[0];
  assert.equal(added.name, '弹窗新小项');
  cleanup(() => api('DELETE', `/categories/${added.id}`));
  await dialog.getByRole('button', { name: /完\s*成/ }).click();
  for (const name of ['小项甲', '小项乙']) {
    const child = await api('POST', '/categories', { kind: 'expense', parent_id: first.id, name });
    cleanup(() => api('DELETE', `/categories/${child.id}`));
  }
  await page.reload();
  await page.getByRole('button', { name: `管理子分类 ${first.name}`, exact: true }).click();
  dialog = page.getByRole('dialog', { name: `子分类 · ${first.name}`, exact: true });
  let release;
  let held = false;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route('**/categories/reorder', async (route) => { const response = await route.fetch(); held = true; await gate; await route.fulfill({ response }); });
  await sortByKeyboard(page, dialog.getByRole('button', { name: /拖动排序/ }));
  await until(() => held, Boolean, 'child response held');
  await dialog.getByRole('button', { name: /完\s*成/ }).click();
  await page.getByRole('button', { name: `管理子分类 ${second.name}`, exact: true }).click();
  const nextDialog = page.getByRole('dialog', { name: `子分类 · ${second.name}`, exact: true });
  await nextDialog.waitFor();
  release(); await page.waitForTimeout(500);
  assert(await nextDialog.isVisible());
  assert.equal(await nextDialog.getByText('弹窗新小项', { exact: true }).count(), 1);
  assert.equal(await page.getByRole('dialog', { name: `子分类 · ${first.name}`, exact: true }).count(), 0);
  await nextDialog.getByRole('button', { name: /完\s*成/ }).click();
  console.log('PASS empty category creation and late sort after closing/switching parent');

  // A read-only controlled response changes the displayed balance while a save is pending.
  // No financial mutation is issued; rollback must use the newest objects, not a snapshot.
  await page.goto(base + '/accounts');
  const handles = page.locator('.ant-table-tbody').getByRole('button', { name: /拖动排序/ });
  await handles.nth(1).waitFor();
  const accounts = await api('GET', '/accounts');
  const cash = accounts.find((row) => row.type === 'asset' && row.asset_class !== 'fund');
  let releaseFailure;
  let saving = false;
  let read = false;
  const failureGate = new Promise((resolve) => { releaseFailure = resolve; });
  await page.route('**/accounts/reorder', async (route) => { saving = true; await failureGate; await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '受控排序失败' }) }); });
  await page.route('**/api/v1/accounts', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    read = true;
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'ok', data: accounts.map((row) => row.id === cash.id ? { ...row, balance: '321.00' } : row) }) });
  });
  await sortByKeyboard(page, handles);
  await until(() => saving, Boolean, 'account save held');
  assert(await page.getByRole('button', { name: /新建账户$/ }).isDisabled());
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
    window.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForTimeout(150);
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
    window.dispatchEvent(new Event('visibilitychange'));
  });
  await until(() => read, Boolean, 'background balance read');
  await page.locator(`[data-order-id="${cash.id}"]`).getByText(/321.00/).waitFor();
  const target = [accounts[1].id, accounts[0].id, ...ids(accounts.slice(2))];
  assert.deepEqual(await page.locator('.ant-table-tbody [data-order-id]').evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId))), target, 'read overwrote pending order');
  releaseFailure();
  await page.getByText('受控排序失败', { exact: true }).waitFor();
  assert.deepEqual(await page.locator('.ant-table-tbody [data-order-id]').evaluateAll((rows) => rows.map((row) => Number(row.dataset.orderId))), ids(accounts));
  assert.equal(await page.locator(`[data-order-id="${cash.id}"]`).getByText(/321.00/).count(), 1, 'rollback restored an old balance object');
  await page.unroute('**/api/v1/accounts');
  await page.unroute('**/accounts/reorder');
  await page.getByRole('button', { name: '刷新列表', exact: true }).click();
  await until(() => page.getByText('受控排序失败', { exact: true }).count(), (count) => count === 0, 'failure refreshed');
  console.log('PASS pending save freezes order, blocks CRUD, retains fresh balance on rollback and refreshes');
  await context.close();
}, 'chromium');
