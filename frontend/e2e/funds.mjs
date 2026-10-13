import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, withFixture, login, shot, until } from './support/fixture.mjs';

const base = process.env.FUND_E2E_BASE ?? 'http://127.0.0.1:5603';
await assertFixture(base, ['fund-browser']);
const prefix = `基金验收-${Date.now()}`;
let assertions = 0;
function check(value, message) { assert(value, message); assertions++; }
const clients = [];
for (const username of ['voucher-admin', 'voucher-recorder', 'voucher-member', 'voucher-viewer']) {
  const auth = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username, password: 'voucher-test-only' }) });
  assert.equal(auth.status, 200);
  const token = (await auth.json()).data.access_token;
  const request = async (method, endpoint, input, expected = 200) => {
    const response = await fetch(base + '/api/v1' + endpoint, { method, headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' }, ...(input === undefined ? {} : { body: JSON.stringify(input) }) });
    const body = await response.json(); assert.equal(response.status, expected, `${method} ${endpoint}: ${JSON.stringify(body)}`); assertions++;
    return body.data;
  };
  await request('GET', '/accounts');
  const bank = await request('POST', '/accounts', { name: prefix + '-' + username + '-现金', type: 'asset', asset_class: 'cash', opening_balance: '1000', request_key: randomUUID() });
  clients.push({ username, request, bank, token });
}

await withFixture(base, async (browser) => {
  const admin = clients[0];
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await login(page, base);
  await page.goto(base + '/funds');
  await page.getByRole('button', { name: /新建基金账户$/ }).click();
  const editor = page.getByRole('dialog', { name: '新建账户', exact: true });
  await editor.getByLabel('名称', { exact: true }).fill(prefix);
  await editor.getByRole('button', { name: /保\s*存/ }).click();
  await editor.waitFor({ state: 'hidden' });
  await page.waitForURL(/\/funds\/\d+$/);
  const accountId = Number(new URL(page.url()).pathname.split('/').at(-1));
  check(accountId > 0, 'fund account not saved');
  await page.getByRole('button', { name: '录入已有持仓', exact: true }).click();
  const opening = page.getByRole('dialog', { name: '期初持仓', exact: true });
  await opening.getByLabel('基金代码', { exact: true }).fill('023564');
  await opening.getByLabel('已确认持有份额', { exact: true }).fill('5028.62');
  await opening.getByLabel('持仓成本价', { exact: true }).fill('1.4119');
  await opening.getByLabel('持仓生效日期', { exact: true }).fill('2026-10-09');
  await opening.getByRole('button', { name: '保存记录', exact: true }).click();
  await opening.getByText('请确认估值口径', { exact: true }).waitFor();
  check(await opening.getByLabel('已确认持有份额', { exact: true }).inputValue() === '5028.62', 'validation discarded opening draft');
  await opening.getByRole('checkbox', { name: '这是人民币场外净值型基金' }).check();
  const openingKeys = [];
  let openingRequests = 0;
  const openingPath = `**/api/v1/funds/accounts/${accountId}/positions`;
  await page.route(openingPath, async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    openingRequests++;
    openingKeys.push(route.request().postDataJSON().request_key);
    const response = await route.fetch();
    if (openingRequests === 1) {
      assert.equal(response.status(), 200);
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '测试响应丢失，请原样重试' }) });
    }
    return route.fulfill({ response });
  });
  await opening.getByRole('button', { name: '保存记录', exact: true }).click();
  await page.getByText('测试响应丢失，请原样重试', { exact: true }).waitFor();
  check(await opening.getByLabel('已确认持有份额', { exact: true }).inputValue() === '5028.62', 'failed save discarded opening draft');
  check((await admin.request('GET', `/funds/accounts/${accountId}`)).positions.length === 1, 'response-loss write was not committed');
  await opening.getByRole('button', { name: '保存记录', exact: true }).click();
  await opening.waitFor({ state: 'hidden' });
  check(openingRequests === 2 && openingKeys[0] === openingKeys[1], 'UI retry changed request key');
  await page.unroute(openingPath);
  await page.getByText('测试响应丢失，请原样重试', { exact: true }).waitFor({ state: 'hidden' });
  await page.getByTestId('fund-total').filter({ hasText: '6,007.19' }).waitFor();
  const account = await admin.request('GET', `/funds/accounts/${accountId}`);
  const position = account.positions[0];
  assert.equal(position.market_value, '6007.19'); assertions++;
  assert.equal(position.holding_pnl, '-1092.72'); assertions++;
  assert.equal(position.daily_pnl, '-180.53'); assertions++;
  check((await admin.request('GET', '/accounts')).find((a) => a.id === admin.bank.id).balance === '1000', 'opening debited bank');
  await shot(page, 'funds-desktop-account');
  await page.getByRole('button', { name: /详\s*情$/ }).click();
  await page.waitForURL(/\/fund-positions\/\d+$/);
  await page.getByText('5028.62', { exact: true }).first().waitFor();
  check((await page.locator('.fund-event-row').first().innerText()).includes('7,099.91'), 'opening cost currency precision wrong');
  await shot(page, 'funds-desktop-position');
  const second = await admin.request('POST', `/funds/accounts/${accountId}/positions`, { request_key: randomUUID(), code: '012345', shares: '10', cost_price: '1', effective_date: '2026-10-13', valuation_basis: 'cny_otc_unit_nav', basis_confirmed: true });
  const secondView = await admin.request('GET', `/funds/positions/${second.position_id}`);
  check(secondView.daily_pnl === null && secondView.daily_status === 'no_history', 'invented historical daily return');
  check((await admin.request('GET', `/funds/accounts/${accountId}`)).positions.length === 2, 'multi-position account not preserved');

  const buyInput = { request_key: randomUUID(), kind: 'buy', shares: '100', amount: '120', fee: '0.54', trade_nav: '1.1946', effective_date: '2026-10-12', cash_date: '2026-10-12', cash_account_id: admin.bank.id };
  const buy = await admin.request('POST', `/funds/positions/${position.id}/events`, buyInput);
  const replay = await admin.request('POST', `/funds/positions/${position.id}/events`, buyInput);
  check(buy.event_id === replay.event_id, 'request replay duplicated buy');
  await admin.request('POST', `/funds/positions/${position.id}/events`, { ...buyInput, amount: '121', fee: '1.54' }, 409);
  const sell = await admin.request('POST', `/funds/positions/${position.id}/events`, { request_key: randomUUID(), kind: 'redeem', shares: '100', amount: '119.46', fee: '1', trade_nav: '1.1946', effective_date: '2026-10-12' });
  const pending = await admin.request('GET', `/funds/positions/${position.id}`);
  check(pending.pending_funds === '118.46', 'pending redemption disappeared');
  const paid = await admin.request('POST', `/funds/positions/${position.id}/events`, { request_key: randomUUID(), kind: 'settlement', source_event_id: sell.event_id, effective_date: '2026-10-13', cash_account_id: admin.bank.id });
  check((await admin.request('GET', `/funds/positions/${position.id}`)).pending_funds === '0', 'settlement left pending balance');
  check((await admin.request('GET', '/accounts')).find((a) => a.id === admin.bank.id).balance === '998.46', 'fees or cash credited twice');
  const flows = await admin.request('GET', `/transactions?account_id=${accountId}&page_size=50`);
  const flowRows = flows.items ?? flows.list ?? flows.rows ?? [];
  const managed = flowRows.find((f) => f.fund_event_id === paid.event_id);
  check(managed?.fund_code === '023564.OF', 'fund code missing from cash flow');
  await admin.request('DELETE', `/transactions/${managed.id}`, undefined, 409);
  await admin.request('POST', '/transactions', { kind: 'transfer', amount: '10', account_id: admin.bank.id, transfer_account_id: accountId, entry_date: '2026-10-13' }, 409);
  const csvResponse = await fetch(base + '/api/v1/transactions/export?account_id=' + accountId, { headers: {} });
  check(csvResponse.status === 401, 'unauthenticated export allowed');
  const ownCSV = await fetch(base + '/api/v1/transactions/export?account_id=' + accountId, { headers: { authorization: 'Bearer ' + admin.token } });
  check(ownCSV.status === 200, 'own filtered export failed');
  const csvText = await ownCSV.text();
  check(csvText.includes('023564.OF') && csvText.includes('基金买入付款') && csvText.includes('基金赎回到账'), 'fund CSV lacks code/purpose');
  check(csvText.trim().split('\n').length === flowRows.length + 1, 'CSV/list scope mismatch');
  const countAfter = (await admin.request('GET', `/funds/positions/${position.id}/events`)).length;
  check(countAfter === 4, 'unexpected event duplication');

  const viewer = clients[3];
  const viewerFund = await viewer.request('POST', '/accounts', { name: prefix + '-只读', type: 'asset', asset_class: 'fund', opening_balance: '0', request_key: randomUUID() });
  const viewerOpening = await viewer.request('POST', `/funds/accounts/${viewerFund.id}/positions`, { request_key: randomUUID(), code: '023564', shares: '20', cost_price: '1', effective_date: '2026-10-09', valuation_basis: 'cny_otc_unit_nav', basis_confirmed: true });
  await viewer.request('POST', `/funds/positions/${viewerOpening.position_id}/events`, { ...buyInput, request_key: randomUUID(), cash_account_id: viewer.bank.id }, 403);
  await admin.request('GET', `/funds/accounts/${viewerFund.id}`, undefined, 403);
  await viewer.request('GET', `/funds/positions/${position.id}`, undefined, 403);
  const viewerContext = await browser.newContext({ viewport: { width: 360, height: 900 } });
  const viewerPage = await viewerContext.newPage();
  await login(viewerPage, base, 'voucher-viewer');
  await viewerPage.goto(base + `/fund-positions/${viewerOpening.position_id}`);
  await viewerPage.getByRole('button', { name: '更正期初', exact: true }).waitFor();
  check(await viewerPage.getByRole('button', { name: /买\s*入$/ }).count() === 0, 'readonly buy visible');
  await shot(viewerPage, 'funds-mobile-readonly');
  await viewerContext.close();

  const mobile = await browser.newContext({ viewport: { width: 360, height: 900 } });
  const mobilePage = await mobile.newPage();
  await login(mobilePage, base);
  await mobilePage.goto(base + `/funds/${accountId}`);
  await mobilePage.getByText('023564.OF', { exact: false }).first().waitFor();
  await shot(mobilePage, 'funds-mobile-account');
  check(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'mobile page overflow');
  await mobilePage.goto(base + `/fund-positions/${position.id}`);
  await mobilePage.getByRole('button', { name: '现金分红', exact: true }).click();
  const dividend = mobilePage.getByRole('dialog', { name: '现金分红', exact: true });
  await dividend.getByLabel('实际分红金额', { exact: true }).fill('10');
  await dividend.getByLabel('除息 / 权益生效日期', { exact: true }).fill('2026-10-13');
  await dividend.getByRole('switch').click();
  await dividend.getByRole('button', { name: '保存记录', exact: true }).click();
  await dividend.waitFor({ state: 'hidden' });
  await shot(mobilePage, 'funds-mobile-position');
  check((await admin.request('GET', `/funds/positions/${position.id}`)).dividends === '10', 'cash dividend not recorded');
  const observers = [];
  for (const path of ['/', '/accounts', '/stats']) {
    const observer = await context.newPage();
    const observed = { path, page: observer, accounts: [], assets: [] };
    observer.on('response', async (response) => {
      const pathname = new URL(response.url()).pathname;
      if (response.status() !== 200 || !['/api/v1/accounts', '/api/v1/stats/assets'].includes(pathname)) return;
      const body = await response.json().catch(() => null);
      if (body?.data) observed[pathname.endsWith('/accounts') ? 'accounts' : 'assets'].push(body.data);
    });
    await observer.goto(base + path);
    await observer.locator('.page h2').first().waitFor();
    observers.push(observed);
  }
  // Poll responses are observed from already-open pages; no financial mutation
  // or manual reload invalidates their caches after this quote is queued.
  const postQuote = await fetch(base + '/api/v1/__test__/fund-quote', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date: '2026-10-13', nav: '1.2' }) });
  assert.equal(postQuote.status, 200); assertions++;
  await until(() => admin.request('GET', `/funds/positions/${position.id}`), (v) => v.nav_date === '2026-10-13', 'background quote update', 15000);
  await until(async () => (await mobilePage.locator('.page').innerText()), (text) => text.includes('净值截至 2026-10-13'), 'open page background refresh', 75000);
  const latestAccount = await admin.request('GET', `/funds/accounts/${accountId}`);
  for (const observer of observers) {
    if (observer.path === '/stats') {
      await until(async () => observer.assets.at(-1), (data) => data?.points?.some((point) => point.accounts.some((a) => a.account_id === accountId && a.balance === latestAccount.total_value)), 'asset chart background refresh', 75000);
    } else {
      await until(async () => observer.accounts.at(-1), (data) => data?.some((a) => a.id === accountId && a.balance === latestAccount.total_value && a.nav_date === '2026-10-13'), observer.path + ' background account refresh', 75000);
    }
    check(true, observer.path + ' reflects background quotes');
    await observer.page.close();
  }
  await mobilePage.goto(base + `/fund-positions/${second.position_id}`);
  await mobilePage.getByRole('button', { name: '红利再投资', exact: true }).click();
  const reinvest = mobilePage.getByRole('dialog', { name: '红利再投资', exact: true });
  await reinvest.getByLabel('实际确认份额', { exact: true }).fill('5');
  await reinvest.getByLabel('实际分红金额', { exact: true }).fill('6');
  await reinvest.getByLabel('实际成交净值', { exact: true }).fill('1.2');
  await reinvest.getByLabel('份额生效日期', { exact: true }).fill('2026-10-13');
  await reinvest.getByLabel('除息 / 权益生效日期', { exact: true }).fill('2026-10-13');
  await reinvest.getByRole('button', { name: '保存记录', exact: true }).click();
  await reinvest.waitFor({ state: 'hidden' });
  const reinvested = await admin.request('GET', `/funds/positions/${second.position_id}`);
  check(reinvested.shares === '15' && reinvested.cost === '16' && reinvested.dividends === '6', 'reinvestment shares/cost wrong');
  const bankBeforeFailure = (await admin.request('GET', '/accounts')).find((a) => a.id === admin.bank.id).balance;
  await admin.request('PUT', `/accounts/${accountId}`, { name: prefix, type: 'asset', opening_balance: '0', status: 'disabled' });
  await admin.request('POST', `/funds/positions/${position.id}/events`, { ...buyInput, request_key: randomUUID() }, 400);
  await fetch(base + '/api/v1/__test__/fund-quote', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date: '2026-10-13', nav: '1.2', failure: true }) });
  // The automatic evening run has completed. Explicit refreshes still allow
  // users to check failures and corrections after the per-code stop marker.
  await admin.request('POST', `/funds/accounts/${accountId}/refresh`);
  const limited = await until(() => admin.request('GET', `/funds/positions/${position.id}`), (v) => v.source_status === 'rate_limited', 'rate limit preserves assets', 15000);
  check(limited.unit_nav === '1.2' && Number(limited.market_value) > 0, 'rate limit zeroed valid NAV');
  await fetch(base + '/api/v1/__test__/fund-quote', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date: '2026-10-13', nav: '1.25', failure: false }) });
  await admin.request('POST', `/funds/accounts/${accountId}/refresh`);
  await until(() => admin.request('GET', `/funds/positions/${position.id}`), (v) => v.unit_nav === '1.25' && v.source_status === 'ready', 'quote recovery', 15000);
  check((await admin.request('GET', '/accounts')).find((a) => a.id === admin.bank.id).balance === bankBeforeFailure, 'quote recovery changed bank');
  await shot(mobilePage, 'funds-mobile-reinvestment');
  check(errors.length === 0, 'browser exceptions: ' + errors.join('; '));
  await mobile.close(); await context.close();
  console.log(`PASS funds: ${assertions} assertions, desktop/360px screenshots, owner isolation, cash/fees, pending settlement, dividends/reinvestment, duplicate protection, independent background refresh, rate-limit recovery`);
});
