import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.WEALTH_E2E_BASE || 'http://127.0.0.1:5618';
const mobile = process.env.WEALTH_E2E_MOBILE === '1';
await assertFixture(base, ['wealth-browser']);
const api = await fixtureApi(base);
const day = (offset) => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date(Date.now() + offset * 86400000));
const baseline = day(-2), latest = day(-1);
const name = `直接理财零钱-${mobile ? '手机' : '桌面'}-${Date.now()}`;
let checks = 0;
const check = (condition, label) => { assert(condition, label); checks++; };
const fixture = async (path, input = {}) => {
  const r = await fetch(`${base}/api/v1/__test__/${path}`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) });
  assert.equal(r.status, 200);
};
await fixture('wealth-quote', { code: 'FBAE68020E', date: baseline, nav: '1.068000' });
await fixture('wealth-quote', { code: 'FBAE68020E', date: latest, nav: '1.068033' });
const product = (await api('GET', '/wealth/products')).find((p) => p.code === 'FBAE68020E');
assert(product, 'Minsheng E fixture product missing');
await api('POST', `/wealth/products/${product.id}/refresh`);

async function choose(page, modal, label, text) {
  await modal.getByLabel(label, { exact: true }).click();
  if (mobile) {
    const sheet = page.getByRole('dialog', { name: '选择' + label, exact: true });
    await sheet.getByRole('button', { name: text, exact: typeof text === 'string' }).click();
    await sheet.waitFor({ state: 'hidden' });
  } else await page.locator('.ant-select-dropdown:visible').getByText(text, { exact: typeof text === 'string' }).click();
}
async function chooseDate(page, input, value) {
  await input.click();
  const panel = page.locator('.ant-picker-dropdown:visible').last();
  const year = Number(await panel.locator('.ant-picker-year-btn').innerText()), month = Number(await panel.locator('.ant-picker-month-btn').innerText());
  const [targetYear, targetMonth] = value.split('-').map(Number), offset = (targetYear - year) * 12 + targetMonth - month;
  for (let i = 0; i < Math.abs(offset); i++) await panel.locator(`.ant-picker-header-${offset < 0 ? 'prev' : 'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click();
  await panel.waitFor({ state: 'hidden' }); assert.equal(await input.inputValue(), value);
}

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: mobile ? 360 : 1280, height: 900 }, ...(mobile ? { isMobile: true, hasTouch: true } : {}) });
  const page = await context.newPage(); page.setDefaultTimeout(15_000);
  const errors = []; page.on('pageerror', (e) => errors.push(e.message));
  let createCount = 0, associationCount = 0;
  const requests = [];
  page.on('request', (r) => { if (r.method() === 'PUT' && /\/wealth\/cash-accounts\/\d+$/.test(r.url())) associationCount++; });
  await page.route('**/api/v1/accounts', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    createCount++;
    requests.push(route.request().postDataJSON());
    const response = await route.fetch();
    if (createCount === 1) {
      assert.equal(response.status(), 200);
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 20000, message: '模拟新建回执丢失' }) });
    }
    return route.fulfill({ response });
  });
  try {
    await login(page, base); await page.goto(base + '/accounts');
    await page.getByRole('button', { name: /新建账户$/ }).click();
    let modal = page.getByRole('dialog', { name: '新建账户', exact: true });
    await modal.getByLabel('名称', { exact: true }).fill(name);
    await choose(page, modal, '资产类别', '理财零钱');
    await modal.getByRole('button', { name: /^保\s*存$/ }).click();
    await modal.getByText('请选择理财产品及份额', { exact: true }).waitFor();
    check(createCount === 0, 'missing product does not send a partial creation');
    await modal.getByLabel('理财零钱起始余额', { exact: true }).fill('10000.00');
    await choose(page, modal, '关联理财产品', /天天盈9号理财产品E/);
    await chooseDate(page, modal.getByLabel('理财零钱起算基准日', { exact: true }), baseline);
    await modal.locator('.ant-modal-body').evaluate((element) => { element.scrollTop = 0; });
    await shot(page, `wealth-cash-create-form-${mobile ? 'mobile' : 'desktop'}`);
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'creation form fits viewport');
    await modal.getByRole('button', { name: /^保\s*存$/ }).click();
    await page.getByText('模拟新建回执丢失', { exact: true }).waitFor();
    check(await modal.getByLabel('理财零钱起始余额').inputValue() === '10000.00', 'failed response retains starting balance');
    check(await modal.getByLabel('理财零钱起算基准日').inputValue() === baseline, 'failed response retains baseline');
    check((await api('GET', '/accounts')).filter((a) => a.name === name).length === 1, 'lost response created exactly one account');
    await modal.getByRole('button', { name: /^保\s*存$/ }).click(); await modal.waitFor({ state: 'hidden' });
    check(createCount === 2 && requests[0].request_key === requests[1].request_key, 'retry retains creation request key');
    check(requests[0].asset_class === 'wealth_cash' && requests[0].wealth_cash.start_date === baseline && requests[0].opening_balance === '10000.00', 'single account request includes balance and configuration');
    check(associationCount === 0, 'creation does not require a second association request');
    const accounts = await api('GET', '/accounts');
    const matching = accounts.filter((a) => a.name === name); check(matching.length === 1, 'retry does not duplicate account');
    const account = matching[0];
    let view = await api('GET', `/wealth/cash-accounts/${account.id}`);
    check(account.asset_class === 'wealth_cash' && view.link.active && view.link.policy === 'yulibao_20250715' && view.account.opening_balance === '10000', 'new account is configured immediately');
    await fixture('wealth-ensure');
    view = await api('GET', `/wealth/cash-accounts/${account.id}`);
    check(view.account.balance === '10000.31' && view.yields[0].amount === '0.31', 'new account participates in automatic return posting');
    const row = page.locator(mobile ? '.acct-row' : 'tr.ant-table-row').filter({ hasText: name });
    await row.getByRole('button', { name: mobile ? '理财零钱与收益' : '理财零钱', exact: true }).click();
    await page.getByRole('heading', { name, exact: true }).waitFor();
    await page.getByText('调整记录（2）', { exact: true }).click();
    await page.getByText('新建理财零钱', { exact: true }).waitFor();
    await page.locator('.ant-message-notice').last().waitFor({ state: 'hidden' });
    await shot(page, `wealth-cash-create-result-${mobile ? 'mobile' : 'desktop'}`);

    // The payment account's inline creator supports the same direct setup.
    await page.goto(base + '/'); await page.getByRole('button', { name: /快捷记账/ }).click();
    const transaction = page.getByRole('dialog', { name: '记一笔', exact: true });
    if (mobile) {
      await transaction.getByRole('button', { name: '账户', exact: true }).click();
      await page.getByRole('dialog', { name: '选择账户', exact: true }).getByRole('button', { name: '新建账户', exact: true }).click();
    } else {
      await transaction.getByRole('combobox').last().click();
      await page.locator('.ant-select-dropdown:visible').getByRole('button', { name: '新建账户', exact: true }).click();
    }
    modal = page.getByRole('dialog', { name: '新建账户', exact: true });
    const inlineName = name + '-内联';
    await modal.getByLabel('名称', { exact: true }).fill(inlineName);
    await choose(page, modal, '资产类别', '理财零钱');
    await modal.getByLabel('理财零钱起始余额').fill('50.00');
    await choose(page, modal, '关联理财产品', /天天盈9号理财产品E/);
    await modal.getByRole('button', { name: /^保\s*存$/ }).click(); await modal.waitFor({ state: 'hidden' });
    await transaction.getByText('理财零钱默认快速转出，当天停止对应收益', { exact: true }).waitFor();
    check((await api('GET', '/accounts')).some((a) => a.name === inlineName && a.asset_class === 'wealth_cash'), 'inline direct creation returns a payable wallet');
    check(associationCount === 0, 'inline creation is also one account operation');
    await transaction.getByRole('button', { name: /^取\s*消$/ }).click();
    check(errors.length === 0, 'no browser runtime errors');
  } catch (error) {
    await shot(page, `wealth-cash-create-failure-${mobile ? 'mobile' : 'desktop'}`);
    console.error((await page.locator('body').innerText()).slice(-3200)); throw error;
  } finally { await context.close(); }
}, 'chromium');
console.log(JSON.stringify({ checks, mobile }));
