import assert from 'node:assert/strict';
import fs from 'node:fs';
import { randomUUID } from 'node:crypto';
import { assertFixture, fixtureApi, withFixture, login, until, shot } from './support/fixture.mjs';

const base = process.env.WEALTH_E2E_BASE || 'http://127.0.0.1:5606';
const mobile = process.env.WEALTH_E2E_MOBILE === '1';
const code = 'FBAE68020E';
await assertFixture(base, ['wealth-browser']);
const api = await fixtureApi(base);
const day = (offset) => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date(Date.now() + offset * 86400000));
const previous = day(-2), latest = day(-1);
const fixture = async (name, input = {}) => {
  const response = await fetch(`${base}/api/v1/__test__/${name}`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) });
  assert.equal(response.status, 200);
};
const products = await api('GET', '/wealth/products');
const product = products.find((p) => p.code === code);
assert(product && product.registration_code === 'Z7007923000092');
assert(!products.some((p) => p.code === 'FBAE68020A'));
await fixture('wealth-quote', { code, date: previous, nav: '1.068000' });
await fixture('wealth-quote', { code, date: latest, nav: '1.068033' });
await api('POST', `/wealth/products/${product.id}/refresh`);
const account = await api('POST', '/accounts', { name: `民生净值-${mobile ? '手机' : '桌面'}-${Date.now()}`, type: 'asset', asset_class: 'wealth', request_key: randomUUID() });

async function chooseDate(page, input, value) {
  await input.click();
  const panel = page.locator('.ant-picker-dropdown:visible').last();
  const year = Number(await panel.locator('.ant-picker-year-btn').innerText());
  const month = Number(await panel.locator('.ant-picker-month-btn').innerText());
  const [targetYear, targetMonth] = value.split('-').map(Number);
  const offset = (targetYear - year) * 12 + targetMonth - month;
  for (let i = 0; i < Math.abs(offset); i++) await panel.locator(`.ant-picker-header-${offset < 0 ? 'prev' : 'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click();
  await panel.waitFor({ state: 'hidden' });
}

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: mobile ? 360 : 1280, height: 900 }, ...(mobile ? { isMobile: true, hasTouch: true } : {}) });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    await login(page, base);
    await page.goto(`${base}/wealth/${account.id}`);
    await page.getByRole('button', { name: '录入已有持仓', exact: true }).click();
    const modal = page.getByRole('dialog', { name: '期初持仓', exact: true });
    const combo = modal.getByRole('combobox');
    if (await combo.count()) {
      await combo.click();
      await page.locator('.ant-select-dropdown:visible').getByText(/民生理财贵竹固收增利天天盈9号理财产品E/).click();
    } else {
      await modal.getByRole('button', { name: '选择已接入理财产品', exact: true }).click();
      await page.getByRole('dialog', { name: '选择已接入理财产品', exact: true }).getByRole('button', { name: /民生理财贵竹固收增利天天盈9号理财产品E/ }).click();
    }
    await modal.getByLabel('已确认持有份额', { exact: true }).fill('10000');
    await modal.getByLabel('实际总成本（元）', { exact: true }).fill('10000');
    await chooseDate(page, modal.getByLabel('持仓生效日期', { exact: true }), previous);
    await modal.getByRole('button', { name: '保存记录', exact: true }).click();
    await modal.waitFor({ state: 'hidden' });
    const data = await api('GET', `/wealth/accounts/${account.id}`);
    const position = data.positions.find((p) => p.code === code);
    assert(position && position.market_value === '10680.33' && position.holding_pnl === '680.33' && position.daily_pnl === '0.33');
    await page.goto(`${base}/wealth-positions/${position.id}`);
    await page.getByText(/单位净值 1.068033/).waitFor();
    assert.equal(await page.getByRole('button', { name: '登记转出申请', exact: true }).count(), 0);
    assert.equal(await page.getByRole('button', { name: /^赎\s*回$/ }).count(), 1);
    assert.equal(await page.getByRole('button', { name: '登记待确认申购', exact: true }).count(), 1);
    assert.equal(await page.getByText(/甲类为已确认份额/).count(), 0);
    // Run the existing scheduled service after a revised public quote. Reading
    // the position again must update profit without a financial event.
    await fixture('wealth-quote', { code, date: latest, nav: '1.068043', advance: 86400 });
    await fixture('wealth-ensure');
    await page.reload();
    await page.getByText(/单位净值 1.068043/).waitFor();
    const revised = await api('GET', `/wealth/positions/${position.id}`);
    assert.equal(revised.market_value, '10680.43');
    assert.equal(revised.daily_pnl, '0.43');
    assert.equal(revised.cost, '10000');
    assert.equal(revised.shares, '10000');
    const events = await api('GET', `/wealth/positions/${position.id}/events`);
    assert.equal(events.length, 1);
    await shot(page, `wealth-cmbcwm-${mobile ? 'mobile' : 'desktop'}`);
    if (process.env.WEALTH_EVIDENCE_DIR) {
      fs.mkdirSync(process.env.WEALTH_EVIDENCE_DIR, { recursive: true });
      await page.screenshot({ path: `${process.env.WEALTH_EVIDENCE_DIR}/wealth-cmbcwm-${mobile ? 'mobile' : 'desktop'}.png`, fullPage: true });
    }
    await fixture('wealth-quote', { code, failure: true, advance: 86400 });
    await fixture('wealth-ensure');
    const failed = await api('GET', `/wealth/positions/${position.id}`);
    assert.equal(failed.source_status, 'failed');
    assert.equal(failed.unit_nav, '1.068043');
    assert.equal(failed.market_value, '10680.43');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    assert.equal(errors.length, 0, errors.join(';'));
    console.log(`PASS Minsheng E NAV, scheduled revaluation, revision and failure preservation (${mobile ? '360px' : 'desktop'})`);
  } catch (error) {
    await shot(page, 'wealth-cmbcwm-failure');
    throw error;
  } finally {
    await context.close();
  }
}, 'chromium');
