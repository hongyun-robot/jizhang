import { assertFixture, fixturePageRequest as pageRequest, registerFixturePage } from './support/fixture.mjs';
import { firefox } from 'playwright-core';
import assert from 'node:assert/strict';
import path from 'node:path';

const base = process.env.VOUCHER_E2E_BASE ?? 'http://127.0.0.1:5601';
await assertFixture(base);
const browser = await firefox.launch({ headless: true, executablePath: process.env.FIREFOX_EXE ?? path.join(process.env.LOCALAPPDATA, 'ms-playwright/firefox-1543/firefox/firefox.exe') });
const ctx = await browser.newContext();
const page = await ctx.newPage();
registerFixturePage(page,base);
const pageErrors = [];
page.on('pageerror', (e) => pageErrors.push(e.message));
try {
  assert.equal((await (await ctx.request.get(base + '/api/v1/__test__/info')).json()).fixture, 'voucher-browser');
  await page.goto(base + '/login');
  await page.getByPlaceholder('用户名').fill('voucher-admin');
  await page.getByPlaceholder('密码').fill('voucher-test-only');
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL(base + '/');
  const api = async (method, endpoint) => { const reply = await pageRequest(page, method, endpoint); assert.equal(reply.status, 200, JSON.stringify(reply.body)); return reply.body.data; };
  const before = Number((await api('GET', '/accounts'))[0].balance);
  await page.goto(base + '/vouchers');
  await page.getByRole('button', { name: '登记购券', exact: true }).click();
  const modal = page.getByRole('dialog', { name: '登记购券', exact: true });
  const name = `重试-${Date.now()}`;
  await modal.getByLabel('券名称', { exact: true }).fill(name);
  await modal.getByLabel('实付金额', { exact: true }).fill('100');
  for (const [label, text] of [['支付账户', '测试现金'], ['消费分类', '测试餐饮']]) {
    if (label === '消费分类') {
      await modal.locator('.cat-leaves').getByRole('checkbox', { name: text, exact: true }).click();
      continue;
    }
    await modal.getByLabel(label, { exact: true }).click();
    await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: text }).first().click();
  }
  const keys = [];
  await page.route('**/api/v1/vouchers', async (route) => {
    if (route.request().method() !== 'POST') { await route.continue(); return; }
    keys.push(route.request().postDataJSON().request_key);
    if (keys.length === 1) {
      const committed = await route.fetch(); assert.equal(committed.status(), 200);
      await route.fulfill({ status: 504, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '测试连接超时，请核对' }) });
    } else await route.continue();
  });
  await modal.getByRole('button', { name: '登记购券', exact: true }).click();
  await page.getByText('测试连接超时，请核对', { exact: true }).waitFor();
  await modal.getByLabel('实付金额', { exact: true }).fill('101');
  const conflict = page.waitForResponse((r) => r.url() === base + '/api/v1/vouchers' && r.status() === 409);
  await modal.getByRole('button', { name: '登记购券', exact: true }).click();
  await conflict;
  await page.getByText('这次操作已记录，请刷新核对后再发起新操作', { exact: true }).waitFor();
  assert.equal(keys.length, 2); assert.equal(keys[0], keys[1]);
  const rows = (await api('GET', '/vouchers')).items.filter((v) => v.name === name);
  assert.equal(rows.length, 1); assert.equal(Number(rows[0].paid_amount), 100);
  assert.equal(Number((await api('GET', '/accounts'))[0].balance), before - 100);
  await api('DELETE', `/vouchers/${rows[0].id}`);
  assert.equal(Number((await api('GET', '/accounts'))[0].balance), before);
  assert.deepEqual(pageErrors, []);
  console.log('PASS committed timeout + edited retry: same request key, 409, one purchase, balance restored');
} finally { await browser.close(); }
