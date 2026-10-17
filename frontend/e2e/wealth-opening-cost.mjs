import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, fixtureApi, withFixture, login, until, shot } from './support/fixture.mjs';

const base = process.env.WEALTH_E2E_BASE || 'http://127.0.0.1:5605';
const mobile = process.env.WEALTH_E2E_MOBILE === '1';
await assertFixture(base, ['wealth-browser']);
const api = await fixtureApi(base);
let checks = 0;
const check = (condition, label) => { assert(condition, label); checks++; };
const today = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date());
const fixture = async (name, input) => { const r = await fetch(base + '/api/v1/__test__/' + name, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) }); assert.equal(r.status, 200); };
await fixture('wealth-admission', { status: 'enabled' });
await fixture('wealth-quote', { date: today, nav: '1.02142' });
await api('POST', '/wealth/products/1/refresh');
const name = `期初总成本-${mobile ? '手机' : '桌面'}-${Date.now()}`;
const cash = await api('POST', '/accounts', { name: name + '现金', type: 'asset', asset_class: 'cash', opening_balance: '100', request_key: randomUUID() });
const account = await api('POST', '/accounts', { name, type: 'asset', asset_class: 'wealth', request_key: randomUUID() });
const balance = async () => (await api('GET', '/accounts')).find((a) => a.id === cash.id).balance;
async function chooseDate(page, input, value) {
  if (!await input.evaluate((e) => e.readOnly)) {await input.fill(value);await input.press('Enter');return;}
  await input.click();const panel=page.locator('.ant-picker-dropdown:visible').last();const y=Number(await panel.locator('.ant-picker-year-btn').innerText()),m=Number(await panel.locator('.ant-picker-month-btn').innerText());const [year,month]=value.split('-').map(Number),offset=(year-y)*12+month-m;
  for(let i=0;i<Math.abs(offset);i++)await panel.locator(`.ant-picker-header-${offset<0?'prev':'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click();await panel.waitFor({state:'hidden'});
}

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: mobile ? 360 : 1280, height: 900 }, ...(mobile ? { isMobile: true, hasTouch: true } : {}) });
  const page = await context.newPage(); page.setDefaultTimeout(12000); const errors = []; page.on('pageerror', (error) => errors.push(error.message));
  try {
    await login(page, base); await page.goto(base + `/wealth/${account.id}`);
    await page.getByRole('button', { name: '录入已有持仓', exact: true }).click();
    let modal = page.getByRole('dialog', { name: '期初持仓', exact: true });
    const combo = modal.getByRole('combobox');
    if (await combo.count()) { await combo.click(); await page.locator('.ant-select-dropdown:visible').getByText(/苏银理财恒源日申周赎1号A/).click(); }
    else { await modal.getByRole('button', { name: '选择已接入理财产品', exact: true }).click(); await page.getByRole('dialog', { name: '选择已接入理财产品', exact: true }).getByRole('button', { name: /苏银理财恒源日申周赎1号A/ }).click(); }
    check(await modal.getByLabel('期初单位成本', { exact: true }).count() === 0, 'old editable unit cost still present');
    await modal.getByLabel('已确认持有份额', { exact: true }).fill('1.14');
    await modal.getByLabel('实际总成本（元）', { exact: true }).fill('1.16');
    const unit = modal.getByLabel('单位成本（自动计算）', { exact: true });
    await until(() => unit.inputValue(), (v) => v === '1.01754386', 'unit cost decimal rounding'); checks++;
    check(await unit.evaluate((e) => e.readOnly), 'derived unit cost is editable');
    await until(() => modal.getByLabel('单位净值（可填写）', {exact:true}).inputValue(), (s) => s === '1.02142', 'same-date NAV default'); checks++;
    check(await modal.getByLabel('单位净值（可填写）', {exact:true}).isEditable(), 'opening NAV is not editable');
    const previous = new Intl.DateTimeFormat('sv-SE', {timeZone:'Asia/Shanghai'}).format(new Date(Date.now()-86400000));
    await chooseDate(page,modal.getByLabel('持仓生效日期',{exact:true}),previous);
    await until(() => modal.getByLabel('单位净值（可填写）',{exact:true}).inputValue(), (s) => s === '', 'missing selected-date NAV fell back to latest');checks++;
    await chooseDate(page,modal.getByLabel('持仓生效日期',{exact:true}),today);
    await until(() => modal.getByLabel('单位净值（可填写）',{exact:true}).inputValue(), (s) => s === '1.02142', 'date change did not reset NAV default');checks++;
    await modal.getByLabel('单位净值（可填写）',{exact:true}).fill('1.022');
    await fixture('wealth-quote', { date: today, nav: '1.02143' }); await modal.getByRole('button', { name: '刷新参考净值', exact: true }).click();
    await until(() => modal.getByTestId('wealth-reference-nav').innerText(), (s) => s.includes('1.02143'), 'reference NAV refresh'); checks++;
    check(await modal.getByLabel('实际总成本（元）', { exact: true }).inputValue() === '1.16' && await unit.inputValue() === '1.01754386', 'quote refresh altered cost');
    check(await modal.getByLabel('单位净值（可填写）',{exact:true}).inputValue() === '1.022', 'quote refresh overwrote editable snapshot');
    await shot(page, `wealth-opening-total-${mobile ? 'mobile' : 'desktop'}`);
    const payloads = []; let attempts = 0; const path = `**/api/v1/wealth/accounts/${account.id}/positions`;
    await page.route(path, async (route) => { if (route.request().method() !== 'POST') return route.continue(); attempts++; payloads.push(route.request().postDataJSON()); const response = await route.fetch(); if (attempts === 1) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '总成本回执丢失模拟' }) }); return route.fulfill({ response }); });
    await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await page.getByText('总成本回执丢失模拟', { exact: true }).waitFor();
    check(await modal.getByLabel('实际总成本（元）', { exact: true }).inputValue() === '1.16', 'failure lost total-cost draft');
    await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' }); await page.unroute(path);
    check(attempts === 2 && payloads[0].request_key === payloads[1].request_key, 'retry key changed');
    check(payloads[0].total_cost === '1.16' && payloads[0].cost_price === undefined, 'opening sent rounded unit cost');
    let data = await api('GET', `/wealth/accounts/${account.id}`); const position = data.positions[0];
    check(data.positions.length === 1 && position.cost === '1.16' && position.cost_price === '1.01754386', 'total cost did not survive server roundtrip');
    check(position.opening_nav === '1.022' && position.opening_nav_date === today && position.unit_nav === '1.02143','snapshot changed effective valuation NAV or was lost');
    const events = await api('GET', `/wealth/positions/${position.id}/events`);
    check(events.length === 1 && events[0].amount === '1.16', 'stored cost reconstructed from rounded unit cost');
    check(await balance() === '100', 'opening debited cash');
    await page.goto(base + `/wealth-positions/${position.id}`); await page.getByRole('button', { name: '更正期初', exact: true }).click();
    modal = page.getByRole('dialog', { name: '更正期初持仓', exact: true });
    check(await modal.getByLabel('实际总成本（元）', { exact: true }).inputValue() === '1.16', 'amendment initialized wrong total');
    await until(() => modal.getByLabel('单位净值（可填写）',{exact:true}).inputValue(), (s) => s === '1.022', 'amendment lost saved NAV snapshot');checks++;
    await modal.getByLabel('实际总成本（元）', { exact: true }).fill('1.17');
    await until(() => modal.getByLabel('单位成本（自动计算）', { exact: true }).inputValue(), (v) => v === '1.02631579', 'amendment recalculated unit cost'); checks++;
    await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    check((await api('GET', `/wealth/positions/${position.id}`)).cost === '1.17', 'amendment changed exact total');
    await page.getByRole('button', { name: '更正期初', exact: true }).click(); modal = page.getByRole('dialog', { name: '更正期初持仓', exact: true });
    await modal.getByLabel('实际总成本（元）', { exact: true }).fill(''); await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await modal.getByText('请填写实际金额', { exact: true }).waitFor(); checks++;
    await modal.getByLabel('实际总成本（元）', { exact: true }).fill('0');
    await until(() => modal.getByLabel('单位成本（自动计算）', { exact: true }).inputValue(), (v) => v === '0.00000000', 'explicit zero cost'); checks++;
    await modal.getByRole('button', { name: '保存记录', exact: true }).click(); await modal.waitFor({ state: 'hidden' });
    check((await api('GET', `/wealth/positions/${position.id}`)).cost === '0' && await balance() === '100', 'zero amendment created cash or cost');
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'viewport overflow');
    check(errors.length === 0, 'browser errors: ' + errors.join(';'));
    console.log(`PASS wealth opening actual total ${mobile ? '360px' : 'desktop'}: ${checks} assertions`);
  } catch (error) { await shot(page, 'wealth-opening-total-failure'); throw error; }
  finally { await context.close(); }
}, 'chromium');
