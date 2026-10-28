import { firefox } from 'playwright-core';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fixtureToken, registerFixturePage } from './support/fixture.mjs';

const base = process.env.VOUCHER_E2E_BASE ?? 'http://127.0.0.1:5601';
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname));
const browser = await firefox.launch({
  headless: true,
  executablePath: process.env.FIREFOX_EXE ?? path.join(process.env.LOCALAPPDATA, 'ms-playwright/firefox-1543/firefox/firefox.exe'),
});
const context = await browser.newContext({ viewport: { width: 1139, height: 884 } });
const page = await context.newPage();
registerFixturePage(page,base,'voucher-member');
const pageErrors = [];
page.on('pageerror', (error) => pageErrors.push(error.message));
const prefix = `平台验收-${Date.now()}`;
const shots = path.resolve('e2e/shots');
fs.mkdirSync(shots, { recursive: true });
const wait = async (fn, label) => {
  for (let i = 0; i < 70; i++) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out: ${label}`);
};
const auth = async (username) => {
  const response = await fetch(base + '/api/v1/auth/login', { method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({ protocol_version: 2, username, password: 'voucher-test-only' }) });
  assert.equal(response.status, 200);
  return { Authorization: 'Bearer ' + (await response.json()).data.access_token };
};
const headers = {};
const api = async (method, endpoint, data, role = 'member', expected = 200) => {
  const token=fixtureToken(base,'voucher-'+role,headers[role]?.Authorization?.slice(7));
  const response = await context.request.fetch(base + '/api/v1' + endpoint, { method, headers: {Authorization:'Bearer '+token}, data });
  const body = await response.json();
  assert.equal(response.status(), expected, JSON.stringify(body));
  return body.data;
};
const modal = (title = '登记购券') => page.getByRole('dialog', { name: title, exact: true });
const field = (title) => modal(title).getByLabel('平台 / 商家', { exact: true });
const open = async () => {
  await page.goto(base + '/vouchers');
  await page.locator('.page-head').getByRole('button', { name: '登记购券', exact: true }).click();
  await field().waitFor();
};
const cancel = async (title) => {
  await modal(title).getByRole('button', { name: /取\s*消/ }).click();
  await modal(title).waitFor({ state: 'hidden' });
};
const option = (text) => page.locator('.voucher-platform-dropdown:visible .ant-select-item-option-content').getByText(text, { exact: true });
const settleModal = async () => {
  await page.locator('.ant-modal:visible').evaluate(async (element) => {
    const finite = element.getAnimations({ subtree: true }).filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity);
    await Promise.all(finite.map((animation) => animation.finished.catch(() => {})));
  });
};
const create = async (name, keyboard = false) => {
  await field().fill('');
  await field().fill(name);
  await option(`创建「${name}」`).waitFor();
  if (keyboard) {
    await field().press('ArrowDown');
    await page.locator('.voucher-platform-dropdown:visible .ant-select-item-option-active').getByText(`创建「${name}」`, { exact: true }).waitFor();
    await field().press('Enter');
  } else await option(`创建「${name}」`).click();
  await wait(async () => (await api('GET', '/voucher-platforms')).some((row) => row.name === name), 'created option');
  await modal().getByRole('button', { name: '登记购券', exact: true }).waitFor();
};
const fillPurchase = async (name) => {
  await modal().getByLabel('券名称', { exact: true }).fill(name);
  await modal().getByLabel('实付金额', { exact: true }).fill('10');
  await modal().getByLabel('支付账户', { exact: true }).click();
  await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').getByText('测试现金', { exact: true }).click();
  await modal().locator('.cat-leaves').getByRole('checkbox', { name: '测试餐饮', exact: true }).click();
};
const save = async (title = '登记购券') => {
  await modal(title).getByRole('button', { name: title === '登记购券' ? '登记购券' : /保\s*存/, exact: true }).click();
  await modal(title).waitFor({ state: 'hidden' });
};
let voucherPosts = 0;
page.on('request', (request) => {
  if (request.method() === 'POST' && request.url() === base + '/api/v1/vouchers') voucherPosts++;
});
const createdVouchers = [];
try {
  assert.equal((await (await context.request.get(base + '/api/v1/__test__/info')).json()).fixture, 'voucher-browser');
  for (const role of ['admin', 'recorder', 'member', 'viewer']) headers[role] = await auth(`voucher-${role}`);
  for (const method of ['GET', 'POST']) {
    const response = await context.request.fetch(base + '/api/v1/voucher-platforms', { method, data: method === 'POST' ? { name: prefix } : undefined });
    assert.equal(response.status(), 401);
  }
  const balanceBefore = (await api('GET', '/accounts'))[0].balance;
  const statsBefore = await api('GET', '/stats/compare');
  const personalIds = new Set();
  for (const role of ['admin', 'recorder', 'member', 'viewer']) {
    const row = await api('POST', '/voucher-platforms', { name: `  ${prefix}-全员  ` }, role);
    assert(!personalIds.has(row.id));
    personalIds.add(row.id);
    assert((await api('GET', '/voucher-platforms', undefined, role)).some((item) => item.id === row.id));
    await api('POST', '/voucher-platforms', { name: '   ' }, role, 400);
  }
  await api('POST', '/vouchers', {}, 'viewer', 403);
  await api('POST', '/transactions', { kind: 'expense', amount: '1', entry_date: '2026-10-10', account_id: (await api('GET', '/accounts'))[0].id, category_id: 1 }, 'viewer', 403);
  assert.equal((await api('GET', '/accounts'))[0].balance, balanceBefore);
  assert.deepEqual(await api('GET', '/stats/compare'), statsBefore);
  console.log('PASS API: all roles, independent same-name identities, validation, authentication, financial permissions unchanged');

  await page.goto(base + '/login');
  await page.getByPlaceholder('用户名').fill('voucher-member');
  await page.getByPlaceholder('密码').fill('voucher-test-only');
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL(base + '/');
  await open();
  const standalone = prefix + '-单独创建';
  await field().fill(standalone);
  await field().dispatchEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 229, which: 229, isComposing: true });
  assert(!(await api('GET', '/voucher-platforms')).some((row) => row.name === standalone));
  await field().press('Tab');
  assert(!(await api('GET', '/voucher-platforms')).some((row) => row.name === standalone));
  await field().click();
  await option(`创建「${standalone}」`).waitFor();
  await wait(async () => {
    const box = await page.locator('.voucher-platform-dropdown:visible').boundingBox();
    return box && box.width > 250 && box.y + box.height <= 884;
  }, 'desktop dropdown placement');
  await settleModal();
  await page.screenshot({ path: path.join(shots, 'voucher-platform-desktop.png') });
  await create(standalone, true);
  assert.equal(await field().inputValue(), standalone);
  assert.equal(voucherPosts, 0, 'keyboard creation submitted the purchase');
  await cancel();
  assert.equal((await api('GET', '/accounts'))[0].balance, balanceBefore);
  console.log('PASS standalone: no creation on Tab, keyboard confirmation, cancel persistence');

  await open();
  await field().click();
  await field().fill(standalone);
  await option(standalone).click();
  assert.equal(await field().inputValue(), standalone);
  await modal().locator('.voucher-platform-input .ant-input-clear-icon').click();
  assert.equal(await field().inputValue(), '');
  const direct = prefix + '-直接保存';
  await field().fill(direct);
  await fillPurchase(prefix + '-券');
  await save();
  const voucher = (await api('GET', '/vouchers', undefined, 'member')).items.find((row) => row.name === prefix + '-券');
  assert(voucher);
  createdVouchers.push(voucher.id);
  assert.equal(voucher.platform, direct);
  assert((await api('GET', '/voucher-platforms')).some((row) => row.name === direct));
  console.log('PASS direct purchase: selected/cleared platform, typed new name saved');
  await page.goto(base + `/vouchers?id=${voucher.id}`);
  await page.locator('.voucher-actions').getByRole('button', { name: /编\s*辑\s*券/ }).click();
  assert.equal(await field('编辑券').inputValue(), direct);
  const edited = prefix + '-编辑新名称';
  await field('编辑券').fill(edited);
  await save('编辑券');
  assert.equal((await api('GET', `/vouchers/${voucher.id}`)).platform, edited);
  await page.locator('.voucher-actions').getByRole('button', { name: /编\s*辑\s*券/ }).click();
  await settleModal();
  await modal('编辑券').locator('.voucher-platform-input .ant-input-clear-icon').click();
  assert.equal(await field('编辑券').inputValue(), '');
  const clearedRequest = page.waitForRequest((request) => request.method() === 'PUT' && request.url().endsWith(`/vouchers/${voucher.id}`));
  await save('编辑券');
  assert.equal((await clearedRequest).postDataJSON().platform, '', 'cleared platform must be explicit in update');
  assert.equal((await api('GET', `/vouchers/${voucher.id}`)).platform, '');
  console.log('PASS desktop: keyboard creation, cancel persistence, selection, clearing, direct save, editing');

  await page.setViewportSize({ width: 390, height: 844 });
  await open();
  await field().click();
  const sheet = page.getByRole('dialog', { name: '选择平台 / 商家', exact: true });
  await sheet.getByPlaceholder('搜索，或输入新名称创建').fill(standalone);
  await sheet.getByRole('button', { name: standalone, exact: true }).waitFor();
  const box = await sheet.boundingBox();
  assert(box && box.x >= 0 && box.x + box.width <= 390);
  await page.screenshot({ path: path.join(shots, 'voucher-platform-mobile.png') });
  await sheet.getByRole('button', { name: standalone, exact: true }).click();
  await field().click();
  const mobileName = prefix + '-手机创建';
  await sheet.getByPlaceholder('搜索，或输入新名称创建').fill(mobileName);
  await sheet.getByRole('button', { name: '创建「' + mobileName + '」', exact: true }).click();
  await sheet.waitFor({ state: 'hidden' });
  await cancel();
  console.log('PASS mobile: personal options, selection, creation, bottom sheet placement');
  await page.setViewportSize({ width: 1139, height: 884 });

  // 读取失败保留输入，仍能直接提交券。
  await page.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'GET') await route.fulfill({ status: 500, json: { code: 20000, message: '测试读取失败' } });
    else await route.continue();
  });
  await open();
  await modal().getByText('选项加载失败，可直接输入。', { exact: false }).waitFor();
  const fallback = prefix + '-加载失败保存';
  await field().fill(fallback);
  await fillPurchase(prefix + '-失败读取券');
  await save();
  const fallbackVoucher = (await api('GET', '/vouchers')).items.find((row) => row.name === prefix + '-失败读取券');
  assert.equal(fallbackVoucher.platform, fallback);
  createdVouchers.push(fallbackVoucher.id);
  await page.unroute('**/api/v1/voucher-platforms');

  // 明确创建成功但响应丢失，重试仍得到同一项。
  const lost = prefix + '-丢失响应';
  let lostAttempts = 0;
  await page.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'POST' && route.request().postDataJSON().name === lost && ++lostAttempts === 1) {
      assert.equal((await route.fetch()).status(), 200);
      await route.fulfill({ status: 504, json: { code: 20000, message: '测试创建响应超时' } });
    } else await route.continue();
  });
  await open();
  await field().fill(lost);
  await option(`创建「${lost}」`).click();
  await page.getByText('测试创建响应超时', { exact: true }).waitFor();
  assert.equal(await field().inputValue(), lost);
  await option(`创建「${lost}」`).waitFor({ state: 'hidden' });
  await field().fill('');
  await field().fill(lost);
  await option(`创建「${lost}」`).click();
  await wait(async () => lostAttempts === 2 && !(await modal().getByRole('button', { name: '登记购券', exact: true }).isDisabled()), 'retry creation');
  assert.equal((await api('GET', '/voucher-platforms')).filter((row) => row.name === lost).length, 1);
  await cancel();
  await page.unroute('**/api/v1/voucher-platforms');

  // 两次请求的迟到响应分别覆盖“改输”和“关闭后重开”场景。
  for (const closeAndReopen of [false, true]) {
    const delayed = prefix + (closeAndReopen ? '-迟到重开' : '-迟到改输');
    let release;
    let responseWaiting = false;
    const gate = new Promise((resolve) => { release = resolve; });
    await page.route('**/api/v1/voucher-platforms', async (route) => {
      if (route.request().method() === 'POST' && route.request().postDataJSON().name === delayed) {
        const response = await route.fetch();
        responseWaiting = true;
        await gate;
        await route.fulfill({ response });
      } else await route.continue();
    });
    await open();
    await field().fill(delayed);
    await option(`创建「${delayed}」`).click();
    await wait(() => responseWaiting, 'delayed response');
    if (closeAndReopen) { await cancel(); await open(); }
    const next = prefix + '-后续输入';
    await field().fill(next);
    release();
    await wait(async () => !(await modal().getByRole('button', { name: '登记购券', exact: true }).isDisabled()), 'delayed request finished');
    assert.equal(await field().inputValue(), next);
    await cancel();
    await page.unroute('**/api/v1/voucher-platforms');
  }
  console.log('PASS failures: read fallback, committed response loss retry, late responses, no accidental purchases');
  assert.deepEqual(pageErrors, []);
} catch (error) {
  await page.screenshot({ path: path.join(shots, 'voucher-platform-failure.png') }).catch(() => {});
  throw error;
} finally {
  for (const id of createdVouchers) await api('DELETE', `/vouchers/${id}`);
  await browser.close();
}
