import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, withFixture, login, shot, until } from './support/fixture.mjs';

const base = process.env.GOLD_E2E_BASE ?? 'http://127.0.0.1:5604';
await assertFixture(base, ['gold-browser']);
const prefix = `黄金验收-${Date.now()}`;
const day = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date());
const previous = new Date(day + 'T00:00:00+08:00'); previous.setDate(previous.getDate() - 1);
const yesterday = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(previous);
let assertions = 0;
function check(value, text) { assert(value, text); assertions++; }
async function client(username) {
  const response = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username, password: 'voucher-test-only' }) });
  assert.equal(response.status, 200); const token = (await response.json()).data.access_token;
  const headers = { authorization: `Bearer ${token}`, 'content-type': 'application/json' };
  const request = async (method, path, input, expected = 200) => {
    const r = await fetch(base + '/api/v1' + path, { method, headers, ...(input === undefined ? {} : { body: JSON.stringify(input) }) });
    const body = await r.json(); assert.equal(r.status, expected, `${path}: ${JSON.stringify(body)}`); assertions++; return body.data;
  };
  await request('GET', '/accounts'); return { request, headers };
}
const admin = await client('voucher-admin');
const viewer = await client('voucher-viewer');
const member = await client('voucher-member');
const cash = await admin.request('POST', '/accounts', { name: prefix + '现金', type: 'asset', asset_class: 'cash', opening_balance: '20000', request_key: randomUUID() });
const otherGold = await viewer.request('POST', '/accounts', { name: prefix + '只读黄金', type: 'asset', asset_class: 'gold', opening_balance: '0', request_key: randomUUID() });
await viewer.request('POST', `/gold/accounts/${otherGold.id}/opening`, { kind: 'opening', request_key: randomUUID(), grams: '1', cost_mode: 'total', cost: '800', trade_date: day });
await viewer.request('POST', `/gold/accounts/${otherGold.id}/events`, { kind: 'buy', request_key: randomUUID(), grams: '1', cash_amount: '800', cash_account_id: cash.id, trade_date: day }, 403);
await admin.request('GET', `/gold/accounts/${otherGold.id}`, undefined, 403);

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage(); page.setDefaultTimeout(12000); const errors = []; page.on('pageerror', (e) => errors.push(e.message));
  try {
  await login(page, base); await page.goto(base + '/gold');
  await page.getByRole('button', { name: '新建黄金账户', exact: true }).click();
  let modal = page.getByRole('dialog', { name: '新建账户', exact: true });
  await modal.getByLabel('名称', { exact: true }).fill(prefix);
  await modal.getByRole('button', { name: /保\s*存/ }).click();
  await page.waitForURL(/\/gold\/\d+$/); const id = Number(new URL(page.url()).pathname.split('/').at(-1));
  check(id > 0, 'gold account creation failed');
  await page.getByRole('button', { name: '录入已有持仓', exact: true }).click();
  modal = page.getByRole('dialog', { name: '录入已有持仓', exact: true });
  await modal.getByRole('textbox', { name: '黄金克数', exact: true }).fill('10');
  await modal.getByRole('textbox', { name: '黄金实际金额', exact: true }).fill('8000');
  await modal.getByRole('button', { name: '保存黄金记录' }).click(); await modal.waitFor({ state: 'hidden' });
  await page.getByTestId('gold-grams').filter({ hasText: '10 克' }).waitFor();
  check((await admin.request('GET', '/accounts')).find((a) => a.id === cash.id).balance === '20000', 'opening deducted cash');
  check((await admin.request('GET', `/gold/accounts/${id}`)).market_value === null, 'missing quote disguised as value');
  await page.goto(base + '/'); await page.getByText('报价不完整', { exact: true }).waitFor();
  await page.goto(base + `/gold/${id}`); await page.getByRole('button', { name: '手动刷新报价' }).click();
  await page.getByTestId('gold-value').filter({ hasText: '8,930.40' }).waitFor();
  const calls = async () => (await (await fetch(base + '/api/v1/__test__/gold-calls')).json()).calls;
  const afterRefresh = await calls(); await admin.request('GET', '/gold/quote'); await admin.request('GET', '/gold/accounts');
  check(await calls() === afterRefresh, 'ordinary cache GET contacted bank');
  const historical = await admin.request('POST', '/accounts', { name: prefix + '历史', type: 'asset', asset_class: 'gold', request_key: randomUUID() });
  await admin.request('POST', `/gold/accounts/${historical.id}/opening`, { kind: 'opening', request_key: randomUUID(), grams: '1', cost_mode: 'unit', cost: '800', trade_date: yesterday });
  const curve = await admin.request('GET', `/stats/assets?start_date=${yesterday}&end_date=${day}`);
  check(curve.points[0].accounts.find((a) => a.account_id === historical.id).balance === null, 'today quote backfilled history');

  // Ordinary recording retains its modes and draft; investments live on their own pages.
  await page.goto(base + '/'); await page.getByRole('button', { name: /快捷记账/ }).click();
  modal = page.getByRole('dialog', { name: '记一笔', exact: true });
  check(await modal.locator('.gold-form').count() === 0, 'ordinary modal embeds gold form');
  for (const label of ['买入黄金', '卖出黄金', '理财申购', '理财赎回']) {
    check(await modal.getByText(label, { exact: true }).count() === 0, 'ordinary modal exposes ' + label);
  }
  await modal.getByPlaceholder('0.00', { exact: true }).fill('12.34');
  for (const label of ['支出', '收入', '转账', '退款', '支出']) {
    await modal.getByText(label, { exact: true }).click();
    check(await modal.getByPlaceholder('0.00', { exact: true }).inputValue() === '12.34', 'ordinary mode change discarded draft');
  }
  check(await modal.getByRole('button', { name: '购券：先付款，使用时计消费', exact: true }).count() === 1, 'voucher entry missing');
  await shot(page, 'gold-ordinary-record');
  await modal.getByRole('button', { name: /取\s*消/ }).click();
  await page.goto(base + '/records');
  await page.getByRole('button', { name: /记一笔/ }).click();
  modal = page.getByRole('dialog', { name: '记一笔', exact: true });
  check(await modal.locator('.gold-form').count() === 0, 'records modal embeds gold form');
  check(await modal.getByText('买入黄金', { exact: true }).count() === 0, 'records modal exposes gold purchase');
  check(await modal.getByText('卖出黄金', { exact: true }).count() === 0, 'records modal exposes gold sale');
  await shot(page, 'gold-ordinary-records');
  await modal.getByRole('button', { name: /取\s*消/ }).click();
  await page.goto(base + `/gold/${id}`); await page.getByRole('button', { name: '买入黄金', exact: true }).click();
  modal = page.getByRole('dialog', { name: '买入黄金', exact: true });
  let form = modal.locator('.gold-form');
  await form.getByRole('textbox', { name: '黄金克数', exact: true }).fill('10');
  await form.getByRole('textbox', { name: '黄金实际金额', exact: true }).fill('9000');
  await form.getByRole('combobox').first().click(); await page.locator('.ant-select-dropdown:visible').getByText(prefix + '现金', { exact: true }).click();
  const keys = []; let attempts = 0;
  await page.route(`**/api/v1/gold/accounts/${id}/events`, async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    attempts++; keys.push(route.request().postDataJSON().request_key); const response = await route.fetch();
    if (attempts === 1) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '黄金测试响应丢失' }) });
    return route.fulfill({ response });
  });
  await form.getByRole('button', { name: '保存黄金记录' }).click(); await modal.getByText('黄金测试响应丢失').waitFor();
  check(await form.getByRole('textbox', { name: '黄金实际金额', exact: true }).inputValue() === '9000', 'failed save discarded draft');
  await form.getByRole('button', { name: '保存黄金记录' }).click(); await modal.waitFor({ state: 'hidden' });
  await page.unroute(`**/api/v1/gold/accounts/${id}/events`); check(attempts === 2 && keys[0] === keys[1], 'retry request key changed');
  let value = await admin.request('GET', `/gold/accounts/${id}`); check(value.grams === '20' && value.cost === '17000', 'purchase accounting wrong');
  check((await admin.request('GET', '/accounts')).find((a) => a.id === cash.id).balance === '11000', 'purchase deducted twice');

  await page.goto(base + `/gold/${id}`); await page.getByRole('button', { name: '卖出黄金', exact: true }).click();
  modal = page.getByRole('dialog', { name: '卖出黄金', exact: true }); form = modal.locator('.gold-form');
  await form.getByRole('textbox', { name: '黄金克数', exact: true }).fill('5');
  await form.getByRole('textbox', { name: '黄金实际金额', exact: true }).fill('4500');
  await form.getByRole('textbox', { name: '黄金手续费', exact: true }).fill('10');
  await form.getByRole('combobox').first().click(); await page.locator('.ant-select-dropdown:visible').getByText(prefix + '现金', { exact: true }).click();
  await fetch(base + '/api/v1/__test__/gold-quote', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ buy: '906', sell: '900', advance: 31 }) });
  await form.getByRole('button', { name: '手动刷新报价' }).click();
  await until(async () => (await admin.request('GET', '/gold/quote')).quote?.sell_price, (price) => price === '900', 'manual refreshed quote');
  check(await form.getByRole('textbox', { name: '黄金实际金额', exact: true }).inputValue() === '4500', 'quote changed actual proceeds');
  await form.getByRole('button', { name: '保存黄金记录' }).click(); await modal.waitFor({ state: 'hidden' });
  value = await admin.request('GET', `/gold/accounts/${id}`); check(value.grams === '15' && value.cost === '12750' && value.realized_pnl === '250', 'sale cost or fee doubled');
  await page.getByTestId('gold-realized-pnl').filter({ hasText: '250.00' }).waitFor();
  await page.locator('.ant-message-notice').first().waitFor({ state: 'hidden' });
  await shot(page, 'gold-desktop');
  const txns = await admin.request('GET', `/transactions?account_id=${id}`); const linked = txns.items.find((t) => t.gold_event_id);
  check(linked?.expense_effect === '0', 'gold counted as consumption');
  await admin.request('DELETE', `/transactions/${linked.id}`, undefined, 409);
  await admin.request('POST', '/transactions', { kind: 'transfer', amount: '1', account_id: cash.id, transfer_account_id: id, entry_date: day }, 409);
  const csv = await fetch(base + `/api/v1/transactions/export?account_id=${id}`, { headers: admin.headers });
  check((await csv.text()).includes('建行积存金卖出'), 'CSV lost gold purpose');
  const latest = value.events.filter((e) => !e.voided_at).at(-1);
  await admin.request('POST', `/gold/accounts/${id}/events/${latest.id}/undo`, { request_key: randomUUID() });
  value = await admin.request('GET', `/gold/accounts/${id}`); check(value.grams === '20' && value.cost === '17000' && value.realized_pnl === '0', 'undo failed');
  await admin.request('DELETE', `/accounts/${id}`, undefined, 409);
  await member.request('GET', `/gold/accounts/${id}`, undefined, 403);
  await fetch(base + '/api/v1/__test__/gold-quote', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ failure: true, advance: 7201 }) });
  const failed = await admin.request('POST', '/gold/quote/refresh'); check(failed.quote.sell_price === '900' && failed.status === 'update_error', 'failure discarded cached quote');
  await page.reload(); await page.getByText('报价已连续两小时未成功更新', { exact: true }).waitFor();
  await page.setViewportSize({ width: 360, height: 800 }); await shot(page, 'gold-mobile');
  check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'mobile horizontal overflow');
  await page.getByRole('button', { name: '买入黄金', exact: true }).click();
  modal = page.getByRole('dialog', { name: '买入黄金', exact: true });
  check(await modal.getByRole('textbox', { name: '黄金克数', exact: true }).evaluate((element) => parseFloat(getComputedStyle(element).fontSize) >= 16), 'touch input font too small');
  await modal.getByRole('textbox', { name: '黄金克数', exact: true }).fill('1');
  await modal.getByRole('textbox', { name: '黄金实际金额', exact: true }).fill('900');
  await modal.getByRole('button', { name: '选择本人现金资产账户', exact: true }).click();
  await page.getByRole('dialog', { name: '选择本人现金资产账户', exact: true }).getByRole('button', { name: prefix + '现金', exact: true }).click();
  await shot(page, 'gold-mobile-buy');
  await modal.getByRole('button', { name: '保存黄金记录' }).click(); await modal.waitFor({ state: 'hidden' });
  check((await admin.request('GET', `/gold/accounts/${id}`)).grams === '21', 'mobile purchase failed during quote outage');
  // Hold a successful save response, change account via SPA logout, then release it.
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.getByRole('button', { name: '买入黄金', exact: true }).click();
  modal = page.getByRole('dialog', { name: '买入黄金', exact: true });
  await modal.getByRole('textbox', { name: '黄金克数', exact: true }).fill('1');
  await modal.getByRole('textbox', { name: '黄金实际金额', exact: true }).fill('900');
  await modal.getByRole('combobox').first().click(); await page.locator('.ant-select-dropdown:visible').getByText(prefix + '现金', { exact: true }).click();
  let responseReady = false, releaseResponse;
  const responseGate = new Promise((resolve) => { releaseResponse = resolve; });
  await page.route(`**/api/v1/gold/accounts/${id}/events`, async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    const result = await route.fetch(); responseReady = true; await responseGate; await route.fulfill({ response: result });
  });
  await modal.getByRole('button', { name: '保存黄金记录' }).click();
  await until(async () => responseReady, Boolean, 'held successful gold response');
  await modal.locator('.ant-modal-close').click();
  await page.getByRole('menuitem', { name: /我的/ }).click();
  await page.getByRole('button', { name: /退出登录/ }).click();
  await page.waitForURL(/\/login$/);
  await page.getByPlaceholder('用户名').fill('voucher-viewer'); await page.getByPlaceholder('密码').fill('voucher-test-only');
  await page.getByRole('button', { name: /登\s*录/ }).click(); await page.waitForURL((url) => !url.pathname.endsWith('/login'));
  releaseResponse();
  await page.goto(base + '/gold');
  await page.getByRole('button', { name: prefix + '只读黄金', exact: true }).waitFor();
  check(await page.getByText(prefix, { exact: true }).count() === 0, 'old save populated next login');
  check((await viewer.request('GET', `/gold/accounts/${otherGold.id}`)).grams === '1', 'next login holdings changed');
  check((await admin.request('GET', `/gold/accounts/${id}`)).grams === '22', 'held trade was not committed once');
  check(errors.length === 0, 'browser errors: ' + errors.join('\n'));
  await context.close();
  } catch (error) { await shot(page, 'gold-failure'); throw error; }
});
console.log(`Gold browser acceptance passed: ${assertions} assertions`);
