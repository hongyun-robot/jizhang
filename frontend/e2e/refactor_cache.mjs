import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const baseline = process.argv.includes('--baseline');
const name = '缓存回归-' + crypto.randomUUID();
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  cleanup(async () => {
    const rows = await api('GET', '/vouchers?keyword=' + encodeURIComponent(name));
    for (const row of rows.items) await api('DELETE', `/vouchers/${row.id}`);
  });
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, timezoneId: 'Asia/Shanghai' });
  const page = await context.newPage();
  try {
    await login(page, base);
    await page.getByRole('menuitem', { name: /流水/ }).click();
    await page.locator('.page h2').first().waitFor();
    await page.getByRole('button', { name: /记一笔$/ }).click();
    await page.getByRole('button', { name: /购券：先付款/ }).click();
    const form = page.getByRole('dialog', { name: '登记购券', exact: true });
    await form.getByLabel('券名称', { exact: true }).fill(name);
    await form.getByLabel('实付金额', { exact: true }).fill('10');
    await form.getByLabel('支付账户', { exact: true }).click();
    await page.locator('.ant-select-dropdown:visible').getByText('测试现金', { exact: true }).click();
    await form.locator('.cat-leaves').getByRole('checkbox', { name: '测试餐饮', exact: true }).click();
    await form.getByRole('button', { name: '登记购券', exact: true }).click();
    await form.waitFor({ state: 'hidden' });
    assert((await api('GET', '/vouchers?keyword=' + encodeURIComponent(name))).items.length === 1, 'purchase must have committed');
    if (baseline) {
      await page.waitForTimeout(300);
      assert.equal(await page.locator('.ant-table-tbody').getByText(name, { exact: true }).count(), 0);
      console.log('BASELINE FAIL confirmed: committed voucher is missing from active infinite transaction list');
    } else {
      await until(() => page.locator('.ant-table-tbody').getByText(name, { exact: true }).count(), (count) => count === 1, 'purchase transaction must refresh automatically');
      console.log('PASS purchase refreshes the current transaction list');
    }
  } finally { await context.close(); }
});
