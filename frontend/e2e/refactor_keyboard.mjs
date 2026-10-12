import assert from 'node:assert/strict';
import { withFixture, login, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 390, height: 850 } });
  await context.addInitScript(() => {
    const viewport = new EventTarget();
    Object.assign(viewport, { height: 850, width: 390, offsetTop: 0, offsetLeft: 0, scale: 1 });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
  });
  const page = await context.newPage();
  await login(page, base);
  let writes = 0;
  page.on('request', (request) => { if (request.method() === 'POST' && request.url().endsWith('/vouchers')) writes++; });
  for (const [route, button, title] of [['/vouchers', '登记购券', '登记购券'], ['/records', '记一笔', '记一笔'], ['/liabilities', '新增负债账户', '新增负债账户']]) {
    await page.goto(base + route);
    await page.getByRole('button', { name: new RegExp(button + '$') }).first().click();
    const dialog = page.getByRole('dialog', { name: title, exact: true });
    await dialog.waitFor();
    await page.evaluate(() => { visualViewport.height = 500; visualViewport.dispatchEvent(new Event('resize')); });
    await until(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--kb-inset')), (value) => value === '350px', 'keyboard inset');
    const footer = await dialog.locator('.ant-modal-footer').boundingBox();
    assert(footer && footer.y + footer.height <= 501, title + ' footer must stay above keyboard');
    if (route === '/vouchers') {
      await dialog.getByRole('button', { name: '登记购券', exact: true }).dblclick();
      await dialog.getByText('请填写券名称', { exact: true }).waitFor();
      assert.equal(writes, 0, 'invalid validation must not write');
    }
    await dialog.getByRole('button', { name: /取\s*消/ }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(await page.evaluate(() => document.documentElement.style.getPropertyValue('--kb-inset')), '');
    await page.evaluate(() => { visualViewport.height = 850; visualViewport.dispatchEvent(new Event('resize')); });
  }
  await context.close();
  console.log('PASS voucher/transaction/personal modal switching, simulated soft keyboard footer, invalid submit and observer cleanup');
});
