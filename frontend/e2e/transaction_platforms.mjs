import { firefox } from 'playwright-core';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const base = process.env.VOUCHER_E2E_BASE ?? 'http://127.0.0.1:5601';
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname));
const browser = await firefox.launch({ headless: true, executablePath: process.env.FIREFOX_EXE ?? path.join(process.env.LOCALAPPDATA, 'ms-playwright/firefox-1543/firefox/firefox.exe') });
const context = await browser.newContext({ viewport: { width: 897, height: 884 } });
const page = await context.newPage();
const errors = [];
page.on('pageerror', (error) => errors.push(error.message));
const prefix = `流水平台-${Date.now()}`;
const created = [];
let headers;
const shots = path.resolve('e2e/shots');
fs.mkdirSync(shots, { recursive: true });
const api = async (method, endpoint, data) => {
  const response = await context.request.fetch(base + '/api/v1' + endpoint, { method, headers, data });
  assert.equal(response.status(), 200, await response.text());
  return (await response.json()).data;
};
const dialog = (editing = false) => page.getByRole('dialog', { name: editing ? '编辑流水' : '记一笔', exact: true });
const field = (editing = false) => dialog(editing).getByLabel('平台 / 商家', { exact: true });
const option = (name) => page.locator('.voucher-platform-dropdown:visible .ant-select-item-option-content').getByText(name, { exact: true });
const settle = async () => page.locator('.ant-modal:visible').evaluate(async (element) => {
  await Promise.all(element.getAnimations({ subtree: true }).filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => {})));
});
try {
  assert.equal((await (await context.request.get(base + '/api/v1/__test__/info')).json()).fixture, 'voucher-browser');
  const signedIn = await context.request.post(base + '/api/v1/auth/login', { data: { username: 'voucher-member', password: 'voucher-test-only' } });
  headers = { Authorization: 'Bearer ' + (await signedIn.json()).data.access_token };
  await api('POST', '/voucher-platforms', { name: '美团' });
  const balanceBefore = (await api('GET', '/accounts'))[0].balance;
  await page.goto(base + '/login');
  await page.getByPlaceholder('用户名').fill('voucher-member');
  await page.getByPlaceholder('密码').fill('voucher-test-only');
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL(base + '/');
  await page.getByRole('button', { name: /快捷记账/ }).click();
  await field().waitFor();
  for (const kind of ['收入', '转账', '退款', '支出']) {
    await dialog().getByText(kind, { exact: true }).click();
    assert(await field().isVisible());
  }
  const platform = prefix + '-新商家';
  await field().fill(platform);
  await field().press('Tab');
  assert(!(await api('GET', '/voucher-platforms')).some((item) => item.name === platform));
  await field().fill('');
  await field().fill(platform);
  const createResponse = page.waitForResponse((r) => r.url().endsWith('/voucher-platforms') && r.request().method() === 'POST');
  await option(`创建「${platform}」`).click();
  assert.equal((await createResponse).status(), 200);
  await dialog().getByRole('button', { name: /记\s*账/, exact: true }).waitFor();
  await page.waitForFunction(() => !document.querySelector('.ant-modal-footer .ant-btn-primary')?.disabled);
  assert.equal((await api('GET', '/accounts'))[0].balance, balanceBefore);
  await dialog().getByPlaceholder('0.00').fill('20');
  await dialog().locator('.cat-leaves').getByRole('checkbox', { name: '测试餐饮', exact: true }).click();
  await dialog().locator('.ant-select').filter({ has: page.getByText('账户', { exact: true }) }).click();
  await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').getByText('测试现金', { exact: true }).click();
  await dialog().getByPlaceholder('备注（可选）').fill(prefix);
  await settle();
  await page.screenshot({ path: path.join(shots, 'transaction-platform-desktop.png') });
  await dialog().getByRole('button', { name: /记\s*账/, exact: true }).click();
  await dialog().waitFor({ state: 'hidden' });
  const transaction = (await api('GET', `/transactions?keyword=${encodeURIComponent(platform)}`)).items.find((item) => item.note === prefix);
  assert(transaction);
  created.push(transaction.id);
  assert.equal(transaction.platform, platform);
  assert.equal(transaction.note, prefix);
  assert.equal(Number((await api('GET', '/accounts'))[0].balance), Number(balanceBefore) - 20);

  await page.goto(base + '/records');
  const row = page.getByRole('row').filter({ hasText: prefix });
  await row.getByText(platform, { exact: true }).waitFor();
  await row.getByRole('button', { name: /编\s*辑/ }).click();
  assert.equal(await field(true).inputValue(), platform);
  await field(true).fill('美团');
  await option('美团').click();
  await dialog(true).getByRole('button', { name: /保\s*存/, exact: true }).click();
  await dialog(true).waitFor({ state: 'hidden' });
  assert.equal((await api('GET', `/transactions?keyword=${encodeURIComponent(prefix)}`)).items[0].platform, '美团');
  const exportResponse = await context.request.get(base + `/api/v1/transactions/export?keyword=${encodeURIComponent(prefix)}`, { headers });
  assert.equal(exportResponse.status(), 200);
  assert((await exportResponse.text()).includes(',美团\r\n'));

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(base + '/records');
  const mobileRow = page.locator('.txn-row').filter({ hasText: prefix });
  await mobileRow.getByText(/美团/).waitFor();
  await mobileRow.getByRole('button', { name: /编\s*辑/ }).click();
  assert.equal(await field(true).innerText(), '美团');
  await field(true).click();
  const sheet = page.getByRole('dialog', { name: '选择平台 / 商家', exact: true });
  await sheet.getByRole('button', { name: '美团', exact: true }).waitFor();
  await settle();
  await page.screenshot({ path: path.join(shots, 'transaction-platform-mobile.png') });
  await sheet.getByRole('button', { name: '清空平台 / 商家', exact: true }).click();
  await sheet.waitFor({ state: 'hidden' });
  await dialog(true).getByRole('button', { name: /保\s*存/, exact: true }).click();
  await dialog(true).waitFor({ state: 'hidden' });
  assert.equal((await api('GET', `/transactions?keyword=${encodeURIComponent(prefix)}`)).items[0].platform, '');
  assert.equal(Number((await api('GET', '/accounts'))[0].balance), Number(balanceBefore) - 20);
  assert.deepEqual(errors, []);
  console.log('PASS transaction platform: all modes, personal options, inline creation, save, edit, search, CSV, desktop/mobile clearing, unchanged amount effects');
} catch (error) {
  await page.screenshot({ path: path.join(shots, 'transaction-platform-failure.png') }).catch(() => {});
  throw error;
} finally {
  for (const id of created) await api('DELETE', `/transactions/${id}`);
  await browser.close();
}
