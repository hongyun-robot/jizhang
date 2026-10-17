import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, fixtureApi, withFixture, login, until, shot } from './support/fixture.mjs';

const base = process.env.WEALTH_E2E_BASE || 'http://127.0.0.1:5605';
const mobile = process.env.WEALTH_E2E_MOBILE === '1';
await assertFixture(base, ['wealth-browser']);
const api = await fixtureApi(base);
let checks = 0;
const check = (value, label) => { assert(value, label); checks++; };
const day = (offset) => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date(Date.now() + offset * 86400000));
const today = day(0), tradeDay = day(mobile ? -2 : -1);
const fixture = async (path, data) => { const response = await fetch(base + '/api/v1/__test__/' + path, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(data) }); assert.equal(response.status, 200); };
await fixture('wealth-admission', { status: 'enabled' });
const name = `待确认-${mobile ? '手机' : '桌面'}-${Date.now()}`;
const bank = await api('POST', '/accounts', { request_key: randomUUID(), name: name + '现金', type: 'asset', asset_class: 'cash', opening_balance: '10000' });
const account = await api('POST', '/accounts', { request_key: randomUUID(), name, type: 'asset', asset_class: 'wealth' });
const bankBalance = async () => (await api('GET', '/accounts')).find((a) => a.id === bank.id).balance;
async function chooseDate(page, input, value) {
  await input.click(); const panel = page.locator('.ant-picker-dropdown:visible').last();
  const y = Number(await panel.locator('.ant-picker-year-btn').innerText()), m = Number(await panel.locator('.ant-picker-month-btn').innerText());
  const [year, month] = value.split('-').map(Number); const offset = (year - y) * 12 + month - m;
  for (let i = 0; i < Math.abs(offset); i++) await panel.locator(`.ant-picker-header-${offset < 0 ? 'prev' : 'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click(); await panel.waitFor({ state: 'hidden' });
}
async function choose(page, modal, kind) {
  const combos = modal.getByRole('combobox');
  if (await combos.count()) { await (kind === 'product' ? combos.first() : combos.last()).click(); await page.locator('.ant-select-dropdown:visible').getByText(kind === 'product' ? /苏银理财恒源日申周赎1号A/ : bank.name, { exact: kind !== 'product' }).click(); }
  else { await modal.getByRole('button', { name: kind === 'product' ? '选择待确认申购产品' : kind === 'settlement' ? '选择本人现金资产账户' : '选择订单现金账户', exact: true }).click(); await page.getByRole('dialog', { name: kind === 'product' ? '选择已接入理财产品' : kind === 'settlement' ? '选择本人现金资产账户' : '选择本人现金账户', exact: true }).getByRole('button', { name: kind === 'product' ? /苏银理财恒源日申周赎1号A/ : bank.name }).click(); }
}
await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: mobile ? 360 : 1280, height: 900 }, ...(mobile ? { isMobile: true, hasTouch: true } : {}) });
  const page = await context.newPage(); page.setDefaultTimeout(12000); const errors = []; page.on('pageerror', (e) => errors.push(e.message));
  try {
    await login(page, base); await page.goto(base + '/wealth/' + account.id);
    await page.getByRole('button', { name: '登记待确认申购', exact: true }).click();
    let modal = page.getByRole('dialog', { name: '登记待确认申购', exact: true });
    await choose(page, modal, 'product'); await choose(page, modal, 'cash');
    await modal.getByLabel('实际已付款金额', { exact: true }).fill('8000');
    await chooseDate(page, modal.getByLabel('实际付款日期', { exact: true }), tradeDay);
    await chooseDate(page, modal.getByLabel('申请所属交易日（银行记录）', { exact: true }), tradeDay);
    await modal.getByLabel('银行申请时间（后进先出依据）', { exact: true }).fill('09:00:56');
    await chooseDate(page, modal.getByLabel('预计确认日期（仅提醒，可空）', { exact: true }), day(3));
    const keys = []; let attempts = 0; const routePath = `**/api/v1/wealth/accounts/${account.id}/orders`;
    await page.route(routePath, async (route) => { attempts++; keys.push(route.request().postDataJSON().request_key); const response = await route.fetch(); if (attempts === 1) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '订单回执丢失模拟' }) }); return route.fulfill({ response }); });
    await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await page.getByText('订单回执丢失模拟', { exact: true }).waitFor();
    check(await modal.getByLabel('实际已付款金额', { exact: true }).inputValue() === '8000', 'lost reply discarded order draft');
    await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' }); await page.unroute(routePath);
    check(attempts === 2 && keys[0] === keys[1], 'order retry identity changed');
    const accountView = await api('GET', `/wealth/accounts/${account.id}`), position = accountView.positions[0];
    check(accountView.total_value === '8000' && position.shares === '0' && position.pending_funds === '8000', 'paid pending asset missing');
    check(await bankBalance() === '2000', 'paid pending deducted twice');
    await page.goto(base + '/wealth-positions/' + position.id);
    let row = page.getByTestId(`wealth-order-${position.orders[0].event_id}`); await row.getByRole('button', { name: '确认申购份额', exact: true }).click();
    modal = page.getByRole('dialog', { name: '确认申购份额', exact: true });
    check(await modal.getByLabel('申请所属交易日（银行记录）', { exact: true }).inputValue() === tradeDay, 'used confirmation date as NAV date');
    check(await modal.getByLabel('成交单位净值（可修改）', { exact: true }).inputValue() === '', 'fell back to another date quote');
    await fixture('wealth-quote', { date: tradeDay, nav: '1.25' });
    await modal.getByRole('button', { name: '刷新指定日期净值', exact: true }).click();
    await until(() => modal.getByLabel('成交单位净值（可修改）', { exact: true }).inputValue(), (v) => v === '1.25', 'exact-date NAV auto-fill');
    await until(() => modal.getByLabel('银行实际确认份额（可修改）', { exact: true }).inputValue(), (v) => v === '6400.00', 'rounded share suggestion');
    await modal.getByLabel('成交单位净值（可修改）', { exact: true }).fill('1.3'); await modal.getByRole('button', { name: '刷新指定日期净值', exact: true }).click();
    check(await modal.getByLabel('成交单位净值（可修改）', { exact: true }).inputValue() === '1.3', 'refresh overwrote bank NAV edit');
    await modal.getByLabel('银行确认或调整说明', { exact: true }).fill('银行确认成交净值调整');
    await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    const confirmed = await api('GET', `/wealth/positions/${position.id}`);
    check(confirmed.shares === '6153.85' && confirmed.pending_funds === '0' && await bankBalance() === '2000', 'confirmation repeated cash debit');
    // A second order follows cancellation and two independent actual refunds.
    const pending = await api('POST', `/wealth/accounts/${account.id}/orders`, { request_key: randomUUID(), code: 'J07241', amount: '100', effective_date: today, trade_date: today, application_time: '10:00:00', cash_account_id: bank.id });
    await page.reload();
    row = page.getByTestId(`wealth-order-${pending.event_id}`); await row.getByRole('button', { name: '取消或登记确认失败', exact: true }).click();
    modal = page.getByRole('dialog', { name: '取消或确认失败', exact: true }); await modal.getByLabel('银行确认或调整说明', { exact: true }).fill('银行撤销成功'); await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    check(await bankBalance() === '1900', 'cancellation invented refund cash');
    for (const amount of ['40', '60']) {
      await row.getByRole('button', { name: '登记实际退款', exact: true }).click(); modal = page.getByRole('dialog', { name: '登记申购退款', exact: true });
      await choose(page, modal, 'cash'); await modal.getByLabel('本次实际退款金额', { exact: true }).fill(amount); await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    }
    check(await bankBalance() === '2000', 'partial refunds did not reconcile');
    await row.getByText('退款已登记完成', { exact: true }).waitFor();
    await page.getByRole('button', { name: '登记转出申请', exact: true }).click(); modal = page.getByRole('dialog', { name: '登记转出申请', exact: true });
    await modal.getByLabel('可转出申请金额（不含当日待确认）', { exact: true }).fill('125');
    await chooseDate(page, modal.getByLabel('上一交易日（银行记录）', { exact: true }), tradeDay);
    await modal.getByRole('button', { name: '核对后进先出分配', exact: true }).click(); await modal.getByText(/甲类已确认份额：125 元/).waitFor();
    await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    const outgoing = (await api('GET', `/wealth/positions/${position.id}`)).orders.find((o) => o.kind === 'request_redeem');
    check(outgoing?.suggested_shares === '100', 'confirmed balance suggestion wrong');
    async function confirmOutAndSettle(order) {
      await page.getByTestId(`wealth-order-${order.event_id}`).getByRole('button', { name: '确认转出结果', exact: true }).click();
      modal = page.getByRole('dialog', { name: '确认转出结果', exact: true });
      await modal.getByLabel('成交单位净值（可修改）', { exact: true }).fill('1.3'); await modal.getByLabel('银行确认或调整说明', { exact: true }).fill('银行实际转出开放日净值');
      await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
      const events = await api('GET', `/wealth/positions/${position.id}/events`), confirmed = events.find((e) => e.kind === 'confirm_redeem' && e.source_event_id === order.event_id);
      await page.locator(`#wealth-event-${confirmed.id}`).getByRole('button', { name: '登记到账', exact: true }).click();
      modal = page.getByRole('dialog', { name: '登记到账', exact: true }); await choose(page, modal, 'settlement');
      await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    }
    await confirmOutAndSettle(outgoing);
    check(await bankBalance() === '2130', 'actual NAV proceeds must differ from requested 125');
    const root = async (amount, date, application_time) => api('POST', `/wealth/accounts/${account.id}/orders`, { request_key: randomUUID(), code: 'J07241', amount, effective_date: date, trade_date: date, application_time, cash_account_id: bank.id });
    const older = await root('50', tradeDay, '09:00:00'), newer = await root('100', tradeDay, '10:00:00'); await root('20', today, '11:00:00');
    await page.reload(); await page.getByRole('button', { name: '登记转出申请', exact: true }).click(); modal = page.getByRole('dialog', { name: '登记转出申请', exact: true });
    await modal.getByLabel('可转出申请金额（不含当日待确认）', { exact: true }).fill('140'); await chooseDate(page, modal.getByLabel('上一交易日（银行记录）', { exact: true }), tradeDay);
    await modal.getByRole('button', { name: '核对后进先出分配', exact: true }).click(); await modal.getByText(`乙类申购 #${newer.event_id}：100 元 · 待原申购确认后计算份额`, { exact: true }).waitFor();
    await modal.getByRole('button', { name: '保存订单记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    const pendingOut = (await api('GET', `/wealth/positions/${position.id}`)).orders.find((o) => o.kind === 'request_redeem' && o.status === 'pending');
    check(pendingOut.data.allocations[0].source_id === newer.event_id && pendingOut.data.allocations[1].source_id === older.event_id, 'LIFO plan not linked to bank applications');
    check(!pendingOut.ready, 'unconfirmed beta allowed final redemption');
    for (const [source, shares] of [[older, '40'], [newer, '80']]) await api('POST', `/wealth/positions/${position.id}/events`, { request_key: randomUUID(), kind: 'confirm_buy', source_event_id: source.event_id, effective_date: today, shares });
    await page.reload(); const readyOut = (await api('GET', `/wealth/positions/${position.id}`)).orders.find((o) => o.event_id === pendingOut.event_id);
    check(readyOut.ready && readyOut.suggested_shares === '112', 'proportional beta shares incorrect');
    await confirmOutAndSettle(readyOut);
    check(await bankBalance() === '2105.6', 'beta confirmation/settlement repeated cash');
    await shot(page, `wealth-orders-${mobile ? 'mobile' : 'desktop'}`);
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'orders page overflows viewport');
    check(errors.length === 0, 'browser runtime errors: ' + errors.join(';'));
    console.log(`PASS wealth pending orders ${mobile ? '360px' : 'desktop'}: ${checks} assertions`);
  } catch (error) { await shot(page, 'wealth-orders-failure'); console.log('ORDER_FAILURE_UI', await page.locator('body').innerText()); throw error; }
  finally { await context.close(); }
}, 'chromium');
