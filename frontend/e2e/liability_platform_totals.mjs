import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
await withFixture(base, async (browser) => {
  const api = await fixtureApi(base);
  const clock = await fetch(base + '/api/v1/__test__/date', {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ date: '2026-11-01' }),
  });
  assert(clock.ok);
  const prefix = `分期平台-${crypto.randomUUID().slice(0, 6)}`;
  const existing = await api('POST', '/voucher-platforms', { name: prefix });
  const forest = await api('GET', '/categories');
  const category = forest.flatMap((wrapper) => wrapper.children ?? []).flatMap((root) => root.children?.length ? root.children : [root]).find((entry) => entry.kind === 'expense');
  const assets = (await api('GET', '/accounts')).filter((account) => account.type === 'asset' && account.status === 'active');
  const saveButton = (modal) => modal.getByRole('button', { name: /保\s*存/, exact: true });
  const choose = async (page, modal, label, text) => {
    await modal.getByLabel(label, { exact: true }).click();
    await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({ hasText: text }).first().click();
  };

  for (const viewport of [{ width: 1440, height: 1000 }, { width: 532, height: 884 }, { width: 390, height: 844 }]) {
    const account = await api('POST', '/accounts', { name: `分期合计-${viewport.width}`, type: 'liability', opening_balance: '0', liability_type: 'jd_baitiao', statement_day: 13, repayment_day: 22, repayment_month_offset: 0, principal_remainder: 'first' });
    await api('POST', '/transactions', { account_id: account.id, kind: 'expense', amount: '5', category_id: category.id, entry_date: '2026-11-01', note: '可转分期的旧消费' });
    const context = await browser.newContext({ viewport, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    await context.addInitScript(() => {
      const NativeDate = Date;
      globalThis.Date = class extends NativeDate {
        constructor(...args) { super(...(args.length ? args : ['2026-11-01T12:00:00+08:00'])); }
        static now() { return NativeDate.parse('2026-11-01T12:00:00+08:00'); }
      };
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    const dialog = (name) => page.getByRole('dialog', { name, exact: true });
    const platformPopup = () => viewport.width < 768 ? dialog('选择平台 / 商家') : page.locator('.ant-select-dropdown:visible');
    const openPlatform = async (modal) => {
      await modal.getByLabel('平台 / 商家', { exact: true }).click();
      await platformPopup().waitFor();
      if (viewport.width < 768) assert(await page.evaluate(() => !['INPUT', 'TEXTAREA'].includes(document.activeElement.tagName)), 'opening platform choices must not summon the keyboard');
      return platformPopup();
    };
    const totals = async (modal, values) => until(
      () => modal.locator('.liability-term-amount > .money').allTextContents(),
      (actual) => actual.join('|') === values.join('|'), 'exact principal plus fee totals'
    );
    let releaseCreation;
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${account.id}`);
      await page.getByRole('button', { name: /记分期/ }).click();
      let modal = dialog('记录固定分期');
      await modal.getByLabel('消费本金', { exact: true }).fill('998.33');
      await modal.getByLabel('每期手续费', { exact: true }).fill('0.08');
      await totals(modal, ['¥332.87', '¥332.85', '¥332.85']);
      assert.equal(await modal.getByRole('columnheader').count(), 4);
      assert.equal(await modal.getByRole('columnheader', { name: '本期合计', exact: true }).count(), 1);
      assert.equal(await modal.getByRole('columnheader', { name: '手续费', exact: true }).count(), 0);
      assert.deepEqual(await modal.locator('.liability-term-amount .small').allTextContents(), Array(3).fill('手续费 ¥0.08'));
      const styles = await modal.locator('.liability-term-amount').first().evaluate((cell) => ({ major: getComputedStyle(cell.firstElementChild).fontSize, minor: getComputedStyle(cell.lastElementChild).fontSize, majorColor: getComputedStyle(cell.firstElementChild).color, minorColor: getComputedStyle(cell.lastElementChild).color }));
      assert(parseFloat(styles.minor) < parseFloat(styles.major));
      assert.notEqual(styles.majorColor, styles.minorColor);
      await choose(page, modal, '费用支付账户', assets[0].name);
      await modal.getByText('由此资产账户单独支付，不计入本负债账户的应还金额。', { exact: true }).waitFor({ state: 'visible' });
      await choose(page, modal, '费用支付账户', account.name);
      await modal.getByText('计入本负债账户的对应账单，与本金一起还款。', { exact: true }).waitFor({ state: 'visible' });
      let popup = await openPlatform(modal);
      if (viewport.width < 768) await popup.getByRole('button', { name: existing.name, exact: true }).click();
      else await popup.locator('.ant-select-item-option-content').getByText(existing.name, { exact: true }).click();
      popup = await openPlatform(modal);
      await shot(page, `liability-platform-${viewport.width}`);
      const merchant = `${prefix}-新商家-${viewport.width}`;
      if (viewport.width < 768) await popup.getByPlaceholder('搜索，或输入新名称创建').fill(merchant);
      else await modal.getByLabel('平台 / 商家', { exact: true }).fill(merchant);
      const gate = new Promise((resolve) => { releaseCreation = resolve; });
      await page.route('**/api/v1/voucher-platforms', async (route) => {
        if (route.request().method() === 'POST') await gate;
        await route.continue();
      });
      if (viewport.width < 768) await popup.getByRole('button', { name: `创建「${merchant}」`, exact: true }).click();
      else await popup.locator('.ant-select-item-option-content').getByText(`创建「${merchant}」`, { exact: true }).click();
      await until(() => saveButton(modal).isDisabled(), (disabled) => disabled, 'platform creation blocks saving a plan');
      assert.equal((await api('GET', `/accounts/${account.id}/liability`)).plans.length, 0);
      releaseCreation();
      await until(() => saveButton(modal).isEnabled(), (enabled) => enabled, 'platform creation completes');
      await page.unroute('**/api/v1/voucher-platforms');
      await modal.locator('.liability-term-amount').last().scrollIntoViewIfNeeded();
      await shot(page, `liability-totals-${viewport.width}`);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
      await saveButton(modal).click();
      await modal.waitFor({ state: 'hidden' });
      const detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (data) => data.plans.length === 1, 'plan saved');
      assert.equal(detail.plans[0].fee_account_id, account.id);
      assert.deepEqual(detail.plans[0].terms.map((term) => Number(term.fee_amount)), [0.08, 0.08, 0.08]);
      assert.equal(detail.transactions.find((txn) => txn.id === detail.plans[0].source_txn_id).platform, merchant);
      // Unmounting the optional platform field must release the parent's guard.
      await page.getByRole('button', { name: /记分期/ }).click();
      modal = dialog('记录固定分期');
      popup = await openPlatform(modal);
      const lateMerchant = `${merchant}-迟到`;
      if (viewport.width < 768) await popup.getByPlaceholder('搜索，或输入新名称创建').fill(lateMerchant);
      else await modal.getByLabel('平台 / 商家', { exact: true }).fill(lateMerchant);
      const lateGate = new Promise((resolve) => { releaseCreation = resolve; });
      await page.route('**/api/v1/voucher-platforms', async (route) => { if (route.request().method() === 'POST') await lateGate; await route.continue(); });
      if (viewport.width < 768) await popup.getByRole('button', { name: `创建「${lateMerchant}」`, exact: true }).click();
      else await popup.locator('.ant-select-item-option-content').getByText(`创建「${lateMerchant}」`, { exact: true }).click();
      await until(() => saveButton(modal).isDisabled(), (disabled) => disabled, 'second creation pending');
      if (viewport.width < 768) await popup.locator('.ant-drawer-close').click();
      await choose(page, modal, '消费来源', '可转分期的旧消费');
      await until(() => saveButton(modal).isEnabled(), (enabled) => enabled, 'unmounted platform releases save guard');
      releaseCreation();
      await until(() => api('GET', '/voucher-platforms'), (data) => data.some((entry) => entry.name === lateMerchant), 'late creation completed');
      await page.unroute('**/api/v1/voucher-platforms');
      assert.equal(await modal.getByLabel('平台 / 商家', { exact: true }).count(), 0);
      await modal.getByRole('button', { name: /取\s*消/, exact: true }).click();
      assert.equal((await api('GET', `/accounts/${account.id}/liability`)).plans.length, 1);
      assert.deepEqual(errors, []);
      console.log(`PASS ${viewport.width}px: shared platform picker and creation guards; one amount column with exact totals and muted fees; credit fee attribution`);
    } catch (error) {
      await shot(page, `liability-totals-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally {
      releaseCreation?.();
      await context.close();
    }
  }
}, 'chromium');
