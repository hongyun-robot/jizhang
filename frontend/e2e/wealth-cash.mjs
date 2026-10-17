import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.WEALTH_E2E_BASE || 'http://127.0.0.1:5618';
const mobile = process.env.WEALTH_E2E_MOBILE === '1';
await assertFixture(base, ['wealth-browser']);
const api = await fixtureApi(base);
let checks = 0;
const check = (condition, label) => { assert(condition, label); checks++; };
const day = (offset) => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date(Date.now() + offset * 86400000));
const previous = day(-2), latest = day(-1), code = 'FBAE68020E';
const fixture = async (name, input = {}) => {
  const response = await fetch(`${base}/api/v1/__test__/${name}`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) });
  assert.equal(response.status, 200);
};
await fixture('wealth-quote', { code, date: previous, nav: '1.068000' });
await fixture('wealth-quote', { code, date: latest, nav: '1.068033', advance: 61 });
const wallet = await api('POST', '/accounts', { request_key: randomUUID(), name: `余利宝-${mobile ? '手机' : '桌面'}-${Date.now()}`, type: 'asset', asset_class: 'cash', opening_balance: '10000' });
const category = await api('POST', '/categories', { kind: 'expense', name: `零钱测试${Date.now()}` });
const read = () => api('GET', `/wealth/cash-accounts/${wallet.id}`);

async function chooseDate(page, input, value) {
  await input.click();
  const panel = page.locator('.ant-picker-dropdown:visible').last();
  const year = Number(await panel.locator('.ant-picker-year-btn').innerText()), month = Number(await panel.locator('.ant-picker-month-btn').innerText());
  const [targetYear, targetMonth] = value.split('-').map(Number);
  const offset = (targetYear - year) * 12 + targetMonth - month;
  for (let i = 0; i < Math.abs(offset); i++) await panel.locator(`.ant-picker-header-${offset < 0 ? 'prev' : 'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click();
  await panel.waitFor({ state: 'hidden' });
  assert.equal(await input.inputValue(), value);
}

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: mobile ? 360 : 1280, height: 900 }, ...(mobile ? { isMobile: true, hasTouch: true } : {}) });
  const page = await context.newPage(); page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', (error) => errors.push(error.message));
  try {
    await login(page, base);
    await page.goto(`${base}/accounts`);
    const row = page.locator(mobile ? '.acct-row' : 'tr.ant-table-row').filter({ hasText: wallet.name });
    await row.getByRole('button', { name: mobile ? '关联理财零钱' : '理财零钱', exact: true }).click();
    await page.getByRole('button', { name: '启用理财零钱', exact: true }).click();
    let modal = page.getByRole('dialog', { name: '启用理财零钱', exact: true });
    await modal.getByLabel('关联理财产品', { exact: true }).click();
    await page.locator('.ant-select-dropdown:visible').getByText(/天天盈9号理财产品E/).click();
    await chooseDate(page, modal.getByLabel('理财零钱基准日', { exact: true }), previous);
    await modal.getByRole('button', { name: /^确\s*定$/ }).click();
    await modal.waitFor({ state: 'hidden' });
    await page.getByRole('button', { name: '同步收益', exact: true }).click();
    await page.getByText('已同步并核对收益', { exact: true }).waitFor();
    let view = await until(read, (v) => v.yields.length === 1, 'initial scheduled return');
    check(view.account.id === wallet.id && view.account.asset_class === 'wealth_cash', 'same account converted');
    check(view.account.balance === '10000.31' && view.yields[0].amount === '0.31', 'initial return added once');
    check(view.link.policy === 'yulibao_20250715', 'MYbank policy selected by product metadata');
    const direct = await api('GET', '/accounts');
    check(direct.find((a) => a.id === wallet.id).balance === view.account.balance, 'account ledger and wealth view agree');
    const pure = await api('GET', '/wealth/accounts');
    check(!pure.some((a) => a.account.id === wallet.id), 'no duplicate investment account');

    // Ordinary expense UI must offer the same wallet and default to quick.
    await page.goto(base + '/');
    await page.getByRole('button', { name: /快捷记账/ }).click();
    modal = page.getByRole('dialog', { name: '记一笔', exact: true });
    await modal.getByPlaceholder('0.00', { exact: true }).fill('1000');
    await modal.locator('.cat-pick').getByText(category.name, { exact: true }).first().click();
    await modal.locator('.cat-pick').getByText(category.name, { exact: true }).last().click();
    if (mobile) {
      await modal.getByRole('button', { name: '账户', exact: true }).click();
      const sheet = page.getByRole('dialog', { name: '选择账户', exact: true });
      await sheet.getByRole('textbox').fill(wallet.name);
      await sheet.getByRole('button', { name: wallet.name, exact: true }).click();
    } else {
      await modal.getByRole('combobox').last().click();
      await modal.getByRole('combobox').last().fill(wallet.name);
      await page.locator('.ant-select-dropdown:visible').getByText(wallet.name, { exact: true }).click();
    }
    await modal.getByText('理财零钱默认快速转出，当天停止对应收益', { exact: true }).waitFor();
    check((await modal.getByLabel('理财零钱转出方式').locator('..').innerText()).includes('快速转出'), 'expense defaults to quick');
    await chooseDate(page, modal.getByLabel('流水发生日期', { exact: true }), latest);
    await modal.getByRole('button', { name: /^记\s*账$/ }).click();
    await modal.waitFor({ state: 'hidden' });
    view = await read(); check(view.account.balance === '9000.31', 'expense changes available ledger balance immediately');
    await fixture('wealth-ensure');
    view = await read(); check(view.yields[0].amount === '0.28' && view.account.balance === '9000.28', 'quick withdrawal loses same-day return');

    await page.goto(`${base}/records?account_id=${wallet.id}`);
    const yieldRow = page.locator(mobile ? '.txn-row' : 'tr.ant-table-row').filter({ hasText: '理财零钱收益' });
    await yieldRow.getByRole('button', { name: '修改收益', exact: true }).click();
    modal = page.getByRole('dialog', { name: '修改理财收益', exact: true });
    await modal.getByLabel('实际理财收益', { exact: true }).fill('0.45');
    await modal.getByLabel('收益调整备注').fill('银行实际收益核对');
    await modal.getByRole('button', { name: /^确\s*定$/ }).click(); await modal.waitFor({ state: 'hidden' });
    view = await read(); check(view.account.balance === '9000.45' && view.yields[0].manual_override, 'ledger yield edit adjusts balance');
    await fixture('wealth-quote', { code, date: latest, nav: '1.068043', advance: 61 });
    await api('POST', `/wealth/cash-accounts/${wallet.id}/refresh`);
    view = await read(); check(view.account.balance === '9000.45' && view.yields[0].calculated_amount === '0.36', 'revised NAV preserves user return');
    await page.goto(`${base}/wealth-cash/${wallet.id}`);
    await page.getByRole('button', { name: '校准余额', exact: true }).click();
    modal = page.getByRole('dialog', { name: '校准账户余额', exact: true });
    await modal.getByLabel('当前实际余额', { exact: true }).fill('9001.23');
    await modal.getByRole('button', { name: /^确\s*定$/ }).click(); await modal.waitFor({ state: 'hidden' });
    view = await read(); check(view.account.balance === '9001.23' && view.yields[0].amount === '1.23', 'balance calibration recorded as adopted return');
    await page.locator('.ant-message-notice').last().waitFor({ state: 'hidden' });
    await shot(page, `wealth-cash-${mobile ? 'mobile' : 'desktop'}`);
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'no horizontal overflow');
    await page.goto(`${base}/wealth`);
    const cashCard = page.locator('.wealth-account-list .ant-card').filter({ hasText: wallet.name });
    await cashCard.getByRole('button', { name: '收益与设置', exact: true }).click();
    await page.getByRole('button', { name: '停止自动收益', exact: true }).click();
    modal = page.getByRole('dialog', { name: '停止自动收益？', exact: true });
    await modal.getByRole('button', { name: /^确\s*定$/ }).click(); await modal.waitFor({ state: 'hidden' });
    view = await until(read, (v) => !v.link.active, 'stop automatic income');
    check(view.account.asset_class === 'cash' && view.account.balance === '9001.23' && view.yields.length === 1, 'stop preserves account and history');
    await fixture('wealth-ensure');
    check((await read()).account.balance === '9001.23', 'stopped wallet is not accrued');
    check(errors.length === 0, 'no browser runtime errors: ' + errors.join('; '));
  } catch (error) {
    await shot(page, `wealth-cash-failed-${mobile ? 'mobile' : 'desktop'}`);
    console.error((await page.locator('body').innerText()).slice(-3500));
    throw error;
  } finally { await context.close(); }
}, 'chromium');
console.log(JSON.stringify({ checks, mobile, account: wallet.id }));
