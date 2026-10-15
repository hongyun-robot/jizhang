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
  const suffix = crypto.randomUUID().slice(0, 6);
  const root = await api('POST', '/categories', { kind: 'expense', name: `餐饮-${suffix}` });
  const leaf = await api('POST', '/categories', { kind: 'expense', name: '早餐', parent_id: root.id });
  const otherRoot = await api('POST', '/categories', { kind: 'expense', name: `费用-${suffix}` });
  const otherLeaf = await api('POST', '/categories', { kind: 'expense', name: '服务费', parent_id: otherRoot.id });
  const dialog = (page, name) => page.getByRole('dialog', { name, exact: true });
  const save = async (modal) => {
    await modal.getByRole('button', { name: /保\s*存/, exact: true }).click();
    await modal.waitFor({ state: 'hidden' });
  };
  const pick = async (modal, label, parent, child) => {
    const picker = modal.getByRole('group', { name: label, exact: true });
    await picker.locator('.cat-roots').getByRole('checkbox', { name: parent.name, exact: true }).click();
    await picker.locator('.cat-leaves').getByRole('checkbox', { name: child.name, exact: true }).click();
    await until(
      () => picker.locator('.cat-leaves').getByRole('checkbox', { name: child.name, exact: true }).getAttribute('aria-checked'),
      (checked) => checked === 'true',
      'selected leaf is reflected'
    );
  };
  const noPopup = async (page) => {
    assert.equal(await page.locator('.selection-sheet:visible, .ant-select-dropdown:visible, .ant-cascader-dropdown:visible').count(), 0);
  };

  for (const viewport of [{ width: 1440, height: 1000 }, { width: 532, height: 884 }, { width: 390, height: 844 }]) {
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
    try {
      await login(page, base);
      // Existing accounts retain the legacy last-term default.
      const account = await api('POST', '/accounts', {
        name: `尾差验收-${viewport.width}-${crypto.randomUUID().slice(0, 6)}`,
        type: 'liability', opening_balance: '0', liability_type: 'jd_baitiao',
        statement_day: 13, repayment_day: 22, repayment_month_offset: 0,
      });
      assert.equal(account.principal_remainder, 'last', 'existing behavior is the default');
      await page.goto(base + `/liabilities/${account.id}`);
      await page.getByRole('button', { name: '账户设置', exact: true }).click();
      let modal = dialog(page, '账户设置');
      assert(await modal.getByRole('radio', { name: '放在最后一期', exact: true }).isChecked());
      await modal.locator('.ant-segmented-item').filter({ hasText: '放在首期' }).click();
      await save(modal);
      await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.account.principal_remainder === 'first', 'first-term preference persisted');
      await page.getByRole('button', { name: '账户设置', exact: true }).click();
      modal = dialog(page, '账户设置');
      assert(await modal.getByRole('radio', { name: '放在首期', exact: true }).isChecked(), 'preference restores on reopen');
      await modal.locator('.ant-segmented').scrollIntoViewIfNeeded();
      await shot(page, `liability-settings-${viewport.width}`);
      await modal.getByRole('button', { name: /取\s*消/, exact: true }).click();

      await page.getByRole('button', { name: /记分期/ }).click();
      modal = dialog(page, '记录固定分期');
      await modal.getByLabel('消费本金', { exact: true }).fill('998.33');
      await pick(modal, '消费分类', root, leaf);
      const feePicker = modal.getByRole('group', { name: '手续费支出分类', exact: true });
      await feePicker.locator('.cat-roots').getByRole('checkbox', { name: otherRoot.name, exact: true }).click();
      assert.equal(await feePicker.locator('.cat-leaves [aria-checked="true"]').count(), 0, 'switching the parent clears the old leaf');
      await modal.getByRole('button', { name: /保\s*存/, exact: true }).click();
      await modal.locator('.ant-form-item-explain-error').first().waitFor();
      assert.equal((await api('GET', `/accounts/${account.id}/liability`)).plans.length, 0, 'missing leaf cannot create a plan');
      await pick(modal, '手续费支出分类', otherRoot, otherLeaf);
      await noPopup(page);
      const principalPreview = await modal.locator('.liability-term-amount > .money').allTextContents();
      assert.deepEqual(principalPreview, ['¥332.79', '¥332.77', '¥332.77']);
      assert(await modal.getByText('本金尾差按账户设置放在首期，可在账户设置中修改。', { exact: true }).isVisible());
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'no page overflow');
      await shot(page, `liability-categories-${viewport.width}`);
      await save(modal);
      let detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.plans.length === 1, 'first plan saved');
      assert.deepEqual(detail.plans[0].terms.map((term) => Number(term.principal)), [332.79, 332.77, 332.77]);
      assert.equal(detail.plans[0].fee_category_id, otherLeaf.id);
      assert.equal(detail.transactions.find((txn) => txn.id === detail.plans[0].source_txn_id).category_id, leaf.id);
      const firstPlanId = detail.plans[0].id;
      const historicalTerms = detail.plans[0].terms;

      await page.getByRole('button', { name: '账户设置', exact: true }).click();
      modal = dialog(page, '账户设置');
      await modal.locator('.ant-segmented-item').filter({ hasText: '放在最后一期' }).click();
      await save(modal);
      detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.account.principal_remainder === 'last', 'last-term preference persisted');
      assert.deepEqual(detail.plans[0].terms, historicalTerms, 'setting preserves historical installments');
      await page.getByRole('button', { name: /记分期/ }).click();
      modal = dialog(page, '记录固定分期');
      await modal.getByLabel('消费本金', { exact: true }).fill('998.33');
      assert.deepEqual(await modal.locator('.liability-term-amount > .money').allTextContents(), ['¥332.77', '¥332.77', '¥332.79']);
      await save(modal);
      detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.plans.length === 2, 'last-term plan saved');
      assert.deepEqual(detail.plans.find((plan) => plan.id !== firstPlanId).terms.map((term) => Number(term.principal)), [332.77, 332.77, 332.79]);

      await page.getByRole('tab', { name: '分期计划', exact: true }).click();
      await page.getByRole('button', { name: '调整未来期次 / 费用', exact: true }).first().click();
      modal = dialog(page, '调整分期与费用');
      await pick(modal, '后续手续费分类', root, leaf);
      await noPopup(page);
      await modal.getByLabel('核对 / 操作说明').fill('分类选择验收');
      await save(modal);
      await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.plans[0].fee_category_id === leaf.id, 'adjustment saves leaf ID');
      await page.getByRole('button', { name: '提前结清', exact: true }).first().click();
      modal = dialog(page, '提前结清');
      await pick(modal, '手续费支出分类', otherRoot, otherLeaf);
      await noPopup(page);
      await modal.getByRole('button', { name: /取\s*消/, exact: true }).click();

      await page.goto(base + '/records');
      if (viewport.width < 768) await page.getByRole('button', { name: /筛选/ }).first().click();
      const filter = viewport.width < 768 ? dialog(page, '筛选') : page.locator('.filter-bar');
      await pick(filter, '分类', root, leaf);
      await page.waitForURL((url) => url.searchParams.get('category_id') === String(leaf.id));
      await noPopup(page);
      await filter.getByRole('group', { name: '分类', exact: true }).locator('.cat-roots').getByRole('checkbox', { name: '全部', exact: true }).click();
      await page.waitForURL((url) => !url.searchParams.has('category_id'));
      assert(await filter.isVisible(), 'filter stays open while choosing categories');
      await page.goto(base + '/accounts');
      if (viewport.width < 768) {
        await page.locator('.acct-row').filter({ hasText: account.name }).locator('.acct-name').click();
      } else {
        await page.getByRole('row').filter({ hasText: account.name }).getByRole('button', { name: '编辑', exact: true }).click();
      }
      modal = dialog(page, '编辑账户');
      assert(await modal.getByRole('radio', { name: '放在最后一期', exact: true }).isChecked());
      await modal.locator('.ant-segmented-item').filter({ hasText: '放在首期' }).click();
      await save(modal);
      await until(() => api('GET', `/accounts/${account.id}/liability`), (d) => d.account.principal_remainder === 'first', 'general account editor saves the preference');
      assert.deepEqual(errors, [], 'no browser exceptions');
      console.log(`PASS ${viewport.width}px: inline categories in create/adjust/settle/filter; saved first/last setting; exact previews and historical protection`);
    } catch (error) {
      await shot(page, `liability-categories-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally {
      await context.close();
    }
  }
}, 'chromium');
