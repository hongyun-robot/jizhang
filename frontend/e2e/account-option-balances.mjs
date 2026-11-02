import assert from 'node:assert/strict';
import dayjs from 'dayjs';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.ACCOUNT_BALANCE_E2E_BASE ?? 'http://127.0.0.1:5619';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const cash = await api('POST', '/accounts', { name: '验收现金', type: 'asset', opening_balance: '1234.56' });
  cleanup(() => api('DELETE', `/accounts/${cash.id}`));
  const longName = '长名称账户用于检查窄屏下余额完整展示';
  const large = await api('POST', '/accounts', { name: longName, type: 'asset', opening_balance: '99999999.99' });
  cleanup(() => api('DELETE', `/accounts/${large.id}`));
  const credit = await api('POST', '/accounts', { name: '验收信用', type: 'liability', opening_balance: '0', liability_type: 'credit_card', statement_day: 15, repayment_day: 10, repayment_month_offset: 1 });
  cleanup(() => api('PUT', `/accounts/${credit.id}`, { name: credit.name, type: 'liability', opening_balance: '0', liability_type: 'credit_card', status: 'disabled' }));
  const categoryId = (await api('GET', '/budgets')).categories[0].category_id;
  const txn = await api('POST', '/transactions', { kind: 'expense', amount: '300', entry_date: dayjs().format('YYYY-MM-DD'), account_id: credit.id, category_id: categoryId, note: '选择器欠款验收' });
  cleanup(() => api('DELETE', `/transactions/${txn.id}`));
  assert.equal(Number((await api('GET', '/accounts')).find((row) => row.id === credit.id).balance), -300);

  for (const width of [431, 360, 1280]) {
    const mobile = width < 768;
    const context = await browser.newContext({ viewport: { width, height: 884 }, timezoneId: 'Asia/Shanghai', isMobile: mobile, hasTouch: mobile });
    const page = await context.newPage();
    const errors = [];
    let writes = 0;
    page.on('pageerror', (error) => errors.push(error.message));
    page.on('request', (request) => { if (request.method() !== 'GET' && new URL(request.url()).pathname.startsWith('/api/v1/transactions')) writes++; });
    try {
      await login(page, base);
      await page.getByRole('button', { name: /快捷记账$/ }).click();
      const form = page.getByRole('dialog', { name: '记一笔', exact: true });
      const open = async () => {
        if (mobile) await form.locator('.selection-trigger:not(.voucher-platform-trigger)').first().click();
        else await form.locator('.ant-select').filter({ hasText: /账户|验收现金/ }).click();
        const menu = mobile ? page.getByRole('dialog', { name: '选择账户', exact: true }) : page.locator('.ant-select-dropdown:visible');
        await menu.waitFor();
        return menu;
      };
      const rowFor = (menu, name) => mobile
        ? menu.getByRole('button', { name, exact: true })
        : menu.locator('.ant-select-item-option').filter({ has: page.locator('.selection-option-label').getByText(name, { exact: true }) });
      let menu = await open();
      const cashRow = rowFor(menu, '验收现金');
      await until(() => cashRow.innerText(), (text) => text.includes('1,234.56'), 'cash balance');
      const debtRow = rowFor(menu, '验收信用（信用）');
      assert(!(await cashRow.innerText()).includes('余额'));
      assert(!(await debtRow.innerText()).includes('总欠款'));
      assert.equal(await debtRow.locator('.money').innerText(), '¥300.00');
      assert((await rowFor(menu, longName).innerText()).includes('99,999,999.99'));
      if (mobile) {
        assert(await cashRow.getAttribute('aria-describedby'), 'balance is exposed as the option description');
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page fits viewport');
        const label = await rowFor(menu, longName).locator('span').first().boundingBox();
        const amount = await rowFor(menu, longName).locator('.selection-option-description').boundingBox();
        assert(label.x + label.width <= amount.x + 1, 'long name and balance do not overlap');
        await menu.getByLabel('搜索选项').fill('验收现金');
        assert.equal(await rowFor(menu, '验收现金').count(), 1, 'name search survives rich options');
        assert.equal(await rowFor(menu, '验收信用（信用）').count(), 0);
        await menu.getByLabel('搜索选项').clear();
      }
      if (width !== 360) await shot(page, `account-option-balances-${mobile ? 'mobile' : 'desktop'}`);
      await cashRow.click();
      await menu.waitFor({ state: 'hidden' });
      if (mobile) assert.equal(await form.locator('.selection-trigger:not(.voucher-platform-trigger)').first().innerText(), '验收现金');
      else assert.equal(await form.locator('.ant-select').filter({ hasText: '验收现金' }).innerText(), '验收现金');
      await form.getByPlaceholder('0.00', { exact: true }).fill('88.88');
      let balance = '1200.00';
      await page.route('**/api/v1/accounts', async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        body.data = body.data.map((account) => account.id === cash.id ? { ...account, balance } : account);
        await route.fulfill({ response, json: body });
      });
      const refreshed = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/accounts');
      await page.evaluate(() => {
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
        document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
      });
      await refreshed;
      menu = await open();
      await until(() => rowFor(menu, '验收现金').innerText(), (text) => text.includes('1,200.00'), 'balance refresh');
      assert.equal(await form.getByPlaceholder('0.00', { exact: true }).inputValue(), '88.88', 'refresh preserves transaction draft');
      if (width === 431) {
        await page.emulateMedia({ colorScheme: 'dark' });
        await until(() => page.locator('html').getAttribute('data-theme'), (theme) => theme === 'dark', 'dark theme');
        await shot(page, 'account-option-balances-mobile-dark');
        balance = null;
        const unknown = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/accounts');
        await page.evaluate(() => {
          Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
          document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
          Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
          document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
        });
        await unknown;
        await until(() => rowFor(menu, '验收现金').innerText(), (text) => text.includes('—'), 'unknown balance is not presented as zero');
      }
      assert.equal(writes, 0, 'choosing accounts never submits a transaction');
      assert.deepEqual(errors, []);
      console.log(`PASS ${width}px: cash balance, total debt, large amount, name search, selection, refresh and draft preservation`);
    } finally { await context.close(); }
  }
}, 'chromium');
