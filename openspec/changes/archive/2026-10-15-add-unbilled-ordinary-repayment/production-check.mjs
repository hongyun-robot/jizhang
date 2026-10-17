import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createRequire } from 'node:module';
import path from 'node:path';
import fs from 'node:fs';
const require = createRequire(path.resolve('frontend/package.json'));
const { chromium } = require('playwright-core');
const BASE = 'https://jizhang.zhanghongyun.cn';
const ADMIN_PASS = process.env.JZ_PASS;
assert(ADMIN_PASS, 'administrator password required');
const username = 'deploy-advance-' + randomUUID();
const password = randomUUID();
const note = '提前还款上线验收-' + randomUUID();
const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
const future = new Date(today + 'T12:00:00+08:00'); future.setUTCDate(future.getUTCDate() + 1);
const tomorrow = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(future);
const shots = path.resolve('openspec/changes/archive/2026-10-15-add-unbilled-ordinary-repayment/screenshots');
fs.mkdirSync(shots, { recursive: true });
let adminToken, token, userId, card, source, operation, submitted, browser;
let checks = 0, failed = false;
const check = (condition, message) => { assert(condition, message); console.log('PASS ' + (++checks) + ': ' + message); };
async function request(method, endpoint, input, credential = token, expected = 200) {
 const response = await fetch(BASE + '/api/v1' + endpoint, { method, headers: { 'content-type': 'application/json', ...(credential ? { authorization: 'Bearer ' + credential } : {}) }, ...(input === undefined ? {} : { body: JSON.stringify(input) }) });
 const body = await response.json();
 assert.equal(response.status, expected, method + ' ' + endpoint + ': ' + body.message);
 if (expected === 200) assert.equal(body.code, 0, endpoint + ': ' + body.message);
 return body.data;
}
const cents = (v) => { const negative = v.startsWith('-'); const [whole, part = ''] = v.replace(/^-/, '').split('.'); return (negative ? -1n : 1n) * (BigInt(whole) * 100n + BigInt(part.padEnd(2, '0'))); };
const total = (rows) => rows.reduce((sum, a) => sum + cents(a.balance ?? '0'), 0n);
const label = (name) => new RegExp('^' + [...name].join('\\s*') + '$');
try {
 adminToken = (await request('POST', '/auth/login', { username: 'admin', password: ADMIN_PASS }, undefined)).access_token;
 const created = await request('POST', '/users', { username, password, role: 'admin' }, adminToken);
 userId = created.id;
 token = (await request('POST', '/auth/login', { username, password }, undefined)).access_token;
 const accounts = await request('GET', '/accounts');
 const bank = accounts.find((a) => a.type === 'asset' && a.asset_class !== 'fund' && a.status === 'active');
 const leaves = (rows) => rows.flatMap((r) => r.children?.length ? leaves(r.children) : [r]);
 const category = leaves(await request('GET', '/categories')).find((c) => c.kind === 'expense');
 assert(bank && category, 'private defaults must exist');
 card = await request('POST', '/accounts', { name: note, type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1 });
 source = await request('POST', '/transactions', { kind: 'expense', account_id: card.id, category_id: category.id, amount: '1000', entry_date: today, note });
 const before = await request('GET', '/accounts');
 const input = { account_id: card.id, from_account_id: bank.id, amount: '300', entry_date: today, mode: 'auto', targets: [], scope: 'unbilled_ordinary', request_key: randomUUID(), reason: note };
 const original = await request('GET', `/accounts/${card.id}/liability`);
 check(original.summary.unbilled === '1000' && original.statements.every((b) => !b.issued_at), 'private real unissued purchase is available');
 await request('POST', '/liabilities/repayments/preview', { ...input, amount: '1001' }, token, 409);
 await request('POST', '/liabilities/repayments/preview', { ...input, entry_date: tomorrow }, token, 400);
 await request('POST', '/liabilities/repayments', { ...input, scope: 'issued' }, token, 409);
 check((await request('GET', `/accounts/${card.id}/liability`)).transactions.length === 1, 'excess, future date and issued-only requests reject atomically');
 const quote = await request('POST', '/liabilities/repayments/preview', input);
 check(quote.available_amount === '1000' && quote.allocations.length === 1, 'production preview calculates ordinary capacity');
 check((await request('GET', `/accounts/${card.id}/liability`)).transactions.length === 1, 'preview creates no financial records');
 browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true });
 let context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
 let page = await context.newPage();
 const errors = [];
 page.on('pageerror', (error) => errors.push(error.message));
 page.on('request', (r) => { if (r.method() === 'POST' && new URL(r.url()).pathname === '/api/v1/liabilities/repayments') submitted = r.postDataJSON(); });
 await page.goto(BASE + '/login');
 await page.getByPlaceholder('用户名').fill(username);
 await page.getByPlaceholder('密码').fill(password);
 await page.getByRole('button', { name: /登\s*录/ }).click();
 await page.waitForURL((url) => !url.pathname.endsWith('/login'));
 await page.goto(BASE + `/liabilities/${card.id}`);
 await page.getByRole('button', { name: label('还款') }).click();
 let dialog = page.getByRole('dialog', { name: '记录还款', exact: true });
 await dialog.getByLabel('还款范围', { exact: true }).click();
 await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({ hasText: '提前还未出账' }).click();
 await dialog.getByLabel('还款金额', { exact: true }).fill('300');
 let save = dialog.getByRole('button', { name: /保\s*存/ });
 check(await save.isDisabled(), 'production UI requires confirmation before saving');
 await dialog.getByRole('button', { name: /核对还款金额/ }).click();
 await dialog.locator('.ordinary-repayment-confirmed').waitFor();
 await dialog.locator('.ant-modal-body').evaluate((el) => { el.scrollTop = 0; });
 await page.screenshot({ path: path.join(shots, 'prod-ordinary-advance-desktop.png'), fullPage: false });
 check((await dialog.textContent()).includes('1,000.00') && !(await save.isDisabled()), 'desktop entry and confirmed quote render on production');
 await context.close();
 context = await browser.newContext({ viewport: { width: 474, height: 884 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
 page = await context.newPage();
 page.on('pageerror', (error) => errors.push(error.message));
 page.on('request', (r) => { if (r.method() === 'POST' && new URL(r.url()).pathname === '/api/v1/liabilities/repayments') submitted = r.postDataJSON(); });
 await page.goto(BASE + '/login');
 await page.getByPlaceholder('用户名').fill(username);
 await page.getByPlaceholder('密码').fill(password);
 await page.getByRole('button', { name: /登\s*录/ }).click();
 await page.waitForURL((url) => !url.pathname.endsWith('/login'));
 await page.goto(BASE + `/liabilities/${card.id}`);
 await page.locator('.bottom-tabs').waitFor();
 await page.getByRole('button', { name: label('还款') }).click();
 dialog = page.getByRole('dialog', { name: '记录还款', exact: true });
 await dialog.getByLabel('还款范围', { exact: true }).click();
 await page.locator('.selection-sheet:visible .selection-sheet-item').filter({ hasText: '提前还未出账' }).click();
 await dialog.getByLabel('还款金额', { exact: true }).fill('300');
 await dialog.getByRole('button', { name: /核对还款金额/ }).click();
 await dialog.locator('.ordinary-repayment-confirmed').waitFor();
 save = dialog.getByRole('button', { name: /保\s*存/ });
 await dialog.locator('.ant-modal-body').evaluate((el) => { el.scrollTop = 0; });
 await page.screenshot({ path: path.join(shots, 'prod-ordinary-advance-mobile.png'), fullPage: false });
 check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'production mobile form has no horizontal overflow');
 await save.click();
 await dialog.waitFor({ state: 'hidden' });
 let detail = await request('GET', `/accounts/${card.id}/liability`);
 operation = detail.operations.find((o) => o.kind === 'repayment' && !o.reversed_at);
 check(detail.summary.unbilled === '700' && detail.credits.length === 1 && detail.credits[0].mode === 'ordinary_advance', 'mobile production save pays 300 and leaves 700');
 const after = await request('GET', '/accounts');
 check(cents(after.find((a) => a.id === bank.id).balance) === cents(before.find((a) => a.id === bank.id).balance) - 30000n && total(after) === total(before), 'cash changes exactly 300 and total net assets stay unchanged');
 assert(submitted && operation, 'browser posting must be captured');
 const replay = await request('POST', '/liabilities/repayments', submitted);
 check(replay.operation_id === operation.id && (await request('GET', `/accounts/${card.id}/liability`)).credits.length === 1, 'production retry returns the original repayment');
 check(errors.length === 0, 'production browser has no runtime exceptions');
 await request('POST', `/liabilities/operations/${operation.id}/reverse`, { request_key: randomUUID(), reason: note + '-撤销验收还款' });
 detail = await request('GET', `/accounts/${card.id}/liability`);
 check(detail.summary.unbilled === '1000' && detail.credits.length === 0, 'production reversal restores the exact original debt');
 operation = undefined;
} catch (error) {
 failed = true;
 const message = String(error).replaceAll(password, '<REDACTED>').replaceAll(ADMIN_PASS, '<REDACTED>');
 console.log('FAIL: ' + message.slice(0, 700));
} finally {
 try {
  if (card && token) {
   const detail = await request('GET', `/accounts/${card.id}/liability`);
   for (const op of detail.operations.filter((o) => o.kind === 'repayment' && !o.reversed_at)) {
    await request('POST', `/liabilities/operations/${op.id}/reverse`, { request_key: randomUUID(), reason: note + '-清理测试还款' });
   }
   if (source) await request('DELETE', `/transactions/${source.id}`);
   const remaining = await request('GET', '/transactions?page=1&page_size=100');
   check(remaining.total === 0, 'private production test has no remaining transactions');
   check((await request('GET', `/accounts/${card.id}/liability`)).summary.total_debt === '0', 'test liability returns to zero');
  }
 } catch (error) { failed = true; console.log('CLEANUP FAIL: ' + String(error).slice(0, 250)); }
 try {
  if (userId && adminToken) {
   await request('PATCH', `/users/${userId}/status`, { status: 'disabled' }, adminToken);
   check(true, 'temporary test account disabled and sessions revoked');
  }
 } catch (error) { failed = true; console.log('DISABLE FAIL: ' + String(error).slice(0, 250)); }
 if (browser) await browser.close();
}
console.log('PROD_ORDINARY_ADVANCE: ' + (failed ? 'FAILED' : 'ALL_GREEN') + ' checks=' + checks + ' test_user_id=' + userId);
if (failed) process.exitCode = 1;
