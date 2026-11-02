import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.DASHBOARD_E2E_BASE ?? 'http://127.0.0.1:5619';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const currentBudget = await api('GET', '/budgets');
  const current = currentBudget.period_key;
  const account = (await api('GET', '/accounts')).find((row) => row.type === 'asset');
  const categoryId = currentBudget.categories[0].category_id;
  for (const [period, amount] of [['2025-12', '200'], ['2025-11', '100']]) {
    await api('PUT', `/budgets/${period}`, { total: amount, items: [] });
    cleanup(() => api('PUT', `/budgets/${period}`, {}));
  }
  for (const [kind, amount, date, note] of [
    ['expense', '7', current + '-01', '本期消费'],
    ['expense', '80', '2025-12-30', '历史消费'],
    ['refund', '20', '2025-12-31', '历史退款'],
    ['expense', '35', '2025-11-30', '上期消费'],
  ]) {
    const txn = await api('POST', '/transactions', { kind, amount, entry_date: date, note, account_id: account.id, category_id: categoryId });
    cleanup(() => api('DELETE', `/transactions/${txn.id}`));
  }
  const snapshot = await api('GET', '/stats/compare?period_key=2025-12');
  assert.equal(Number(snapshot.current.expense), 60);
  assert.equal(Number(snapshot.current.balance), -60);
  assert.equal(Number(snapshot.previous.expense), 35);

  async function selectMonth(page, key) {
    await page.getByRole('textbox', { name: '选择账期' }).click();
    const panel = page.locator('.ant-picker-dropdown:visible');
    const year = Number(key.slice(0, 4));
    for (let tries = 0; tries < 6; tries++) {
      const displayed = Number((await panel.locator('.ant-picker-year-btn').innerText()).replace(/\D/g, ''));
      if (displayed === year) break;
      await panel.locator(displayed > year ? '.ant-picker-header-super-prev-btn' : '.ant-picker-header-super-next-btn').click();
    }
    await panel.locator(`.ant-picker-cell[title="${key}"]`).click();
    await until(() => page.getByRole('textbox', { name: '选择账期' }).inputValue(), (value) => value === key, 'selected period');
  }
  async function expectAmount(page, amount) {
    await until(() => page.locator('.hero-value').innerText(), (text) => text.includes(amount), 'period balance');
  }

  for (const viewport of [{ width: 1280, height: 900 }, { width: 431, height: 884 }, { width: 360, height: 800 }]) {
    const context = await browser.newContext({ viewport, timezoneId: 'Asia/Shanghai', isMobile: viewport.width < 768, hasTouch: viewport.width < 768 });
    const page = await context.newPage();
    try {
      await login(page, base);
      await until(() => page.getByRole('textbox', { name: '选择账期' }).inputValue(), (value) => value === current, 'current period');
      await expectAmount(page, '7.00');
      await selectMonth(page, '2025-12');
      await expectAmount(page, '60.00');
      assert.equal(new URL(page.url()).searchParams.get('period_key'), '2025-12');
      await until(() => page.locator('.hero-stat').filter({ hasText: '预算剩余' }).innerText(), (text) => text.includes('140.00'), 'historical budget remaining');
      await page.locator('.budget-list[data-period="2025-12"]').waitFor();
      await until(() => page.locator('.recent-row').count(), (count) => count === 2, 'historical transactions');
      assert(!(await page.locator('.recent-list').innerText()).includes('本期消费'));
      assert((await page.locator('.hero-side').innerText()).includes('当前总资产'));
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'page must fit viewport');
      const label = await page.locator('.hero-heading .hero-label').boundingBox();
      const controls = await page.locator('.hero-period-controls').boundingBox();
      assert(Math.abs(label.y + label.height / 2 - controls.y - controls.height / 2) < 2, 'period controls must stay aligned with the heading');
      assert(await page.getByRole('textbox', { name: '选择账期' }).evaluate((input) => {
        const canvas = document.createElement('canvas');
        const context = canvas.getContext('2d');
        context.font = getComputedStyle(input).font;
        return context.measureText(input.value).width <= input.clientWidth;
      }), 'selected period must be fully readable');
      if (viewport.width !== 360) await shot(page, `dashboard-periods-${viewport.width < 768 ? 'mobile' : 'desktop'}`);
      if (viewport.width === 431) {
        await page.emulateMedia({ colorScheme: 'dark' });
        await until(() => page.locator('html').getAttribute('data-theme'), (value) => value === 'dark', 'dark theme');
        await shot(page, 'dashboard-periods-mobile-dark');
        await page.emulateMedia({ colorScheme: 'light' });
        await until(() => page.locator('html').getAttribute('data-theme'), (value) => value === 'light', 'light theme');
      }

      await page.getByRole('button', { name: '查看全部', exact: true }).click();
      await page.waitForURL((url) => url.pathname === '/records' && url.searchParams.get('period_key') === '2025-12');
      await until(() => page.locator('.page').innerText(), (text) => text.includes('历史消费') && text.includes('历史退款'), 'historical records navigation');
      if (viewport.width >= 768) {
        const response = page.waitForResponse((reply) => reply.url().includes('/api/v1/transactions?') && !new URL(reply.url()).searchParams.has('period_key'));
        await page.getByText('本月', { exact: true }).click();
        assert.equal((await response).status(), 200, 'changing date filter clears the exact billing period');
      }
      await page.goto(base + '/?period_key=2025-12');
      await expectAmount(page, '60.00');
      await page.reload();
      await expectAmount(page, '60.00');
      await selectMonth(page, '2025-11');
      await expectAmount(page, '35.00');
      await page.goBack();
      await expectAmount(page, '60.00');
      await page.goForward();
      await expectAmount(page, '35.00');
      await selectMonth(page, '2024-02');
      await page.getByText('该期暂无流水', { exact: true }).waitFor();
      await expectAmount(page, '0.00');
      if (viewport.width === 431) {
        let fail = true;
        const failedUrl = '**/api/v1/stats/compare?period_key=2024-03';
        await page.route(failedUrl, (route) => fail
          ? route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 500, message: '历史查询暂时失败' }) })
          : route.continue());
        await selectMonth(page, '2024-03');
        await page.locator('.hero-main [role="alert"]').first().waitFor();
        assert(await page.getByRole('textbox', { name: '选择账期' }).isEnabled(), 'picker remains available after a failed query');
        fail = false;
        await page.locator('.hero-main').getByRole('button', { name: /重\s*试/ }).click();
        await expectAmount(page, '0.00');
        await page.unroute(failedUrl);

        let release;
        const delayed = new Promise((resolve) => { release = resolve; });
        const delayHandler = async (route) => {
          if (new URL(route.request().url()).searchParams.get('period_key') === '2025-10') await delayed;
          await route.continue().catch(() => {});
        };
        await page.route('**/api/v1/**', delayHandler);
        try {
          const requested = page.waitForRequest((request) => new URL(request.url()).searchParams.get('period_key') === '2025-10');
          await selectMonth(page, '2025-10');
          await requested;
          assert.equal(await page.locator('.recent-row').count(), 0, 'old period rows must not appear while the new period loads');
          await selectMonth(page, '2025-12');
          await expectAmount(page, '60.00');
          release();
          await page.waitForTimeout(500);
          assert.equal(await page.getByRole('textbox', { name: '选择账期' }).inputValue(), '2025-12');
          await expectAmount(page, '60.00');
          assert.equal(await page.locator('.recent-row').count(), 2);
        } finally {
          release();
          await page.unroute('**/api/v1/**', delayHandler);
        }
        console.log('PASS failed-query retry, available picker, and delayed old requests cannot replace the selected period');
      }
      await page.getByRole('button', { name: '回到本期', exact: true }).click();
      await expectAmount(page, '7.00');
      assert(!new URL(page.url()).searchParams.has('period_key'));
      await until(() => page.locator('.recent-row').count(), (count) => count === 4, 'current period restores recent transactions across all months');
      const input = page.getByRole('textbox', { name: '选择账期' });
      if (viewport.width < 768) {
        assert(await input.getAttribute('readonly') !== null, 'mobile picker must not open keyboard');
        assert((await page.locator('.hero-range').boundingBox()).height >= 40, 'mobile touch target');
      }
      await input.click();
      const popup = await page.locator('.ant-picker-dropdown:visible').boundingBox();
      assert(popup.x >= 0 && popup.x + popup.width <= viewport.width + 1, 'month popup must fit viewport');
      if (viewport.width === 431) await shot(page, 'dashboard-periods-mobile-popup');
      await page.keyboard.press('Escape');
      console.log(`PASS ${viewport.width}px: current, history, budgets, transactions, navigation, refresh, empty period, reset, popup`);
    } finally { await context.close(); }
  }

  // A changed settlement day must not remove old materialized-period records.
  await api('PUT', '/settings', { settlement_day: 26 });
  cleanup(() => api('PUT', '/settings', { settlement_day: 1 }));
  const custom = await api('GET', '/stats/compare?period_key=2025-12');
  assert.equal(custom.start, '2025-11-26');
  assert.equal(custom.end, '2025-12-25');
  assert.equal(Number(custom.current.expense), 60);
  assert.equal((await api('GET', '/transactions?period_key=2025-12')).items.length, 2);
  assert.equal(Number((await api('GET', '/budgets?period_key=2025-12')).total.consumed), 60);
  console.log('PASS custom settlement day keeps historical income/expense, budget and records aligned');
}, 'chromium');
