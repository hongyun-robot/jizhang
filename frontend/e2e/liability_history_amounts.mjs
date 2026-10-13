import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
await withFixture(base, async (browser) => {
  const api = await fixtureApi(base);
  const forest = await api('GET', '/categories');
  const category = forest.flatMap((wrapper) => wrapper.children ?? []).flatMap((root) => root.children?.length ? root.children : [root]).find((entry) => entry.kind === 'expense');
  const clock = async (date) => {
    const response = await fetch(base + '/api/v1/__test__/date', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date }) });
    assert(response.ok);
  };
  const currency = (value) => '¥' + Number(value).toFixed(2);
  const primary = (scope) => scope.locator('.liability-term-amount > .money').allTextContents();
  const settledAccounts = [];
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 532, height: 884 }, { width: 390, height: 844 }]) {
    await clock('2026-11-14');
    const account = await api('POST', '/accounts', { name: `历史金额-${viewport.width}`, type: 'liability', opening_balance: '0', liability_type: 'jd_baitiao', statement_day: 13, repayment_day: 22, repayment_month_offset: 0, principal_remainder: 'first' });
    const bank = await api('POST', '/accounts', { name: `历史还款-${viewport.width}`, type: 'asset', opening_balance: '2000' });
    const created = await api('POST', '/liabilities/installments', { request_key: crypto.randomUUID(), reason: '', account_id: account.id, amount: '998.33', entry_date: '2026-11-01', category_id: category.id, term_count: 3, first_statement_date: '2026-11-01', fee_mode: 'per_term', fee_input: '0.08', fee_account_id: account.id, fee_category_id: category.id });
    await clock('2026-11-14');
    let detail = await api('GET', `/accounts/${account.id}/liability`);
    const fee = detail.transactions.find((txn) => txn.liability_term_id === detail.plans[0].terms[0].id);
    assert(fee);
    assert.equal(detail.summary.total_debt, '998.41');
    const originalPlan = JSON.stringify(detail.plans[0]);
    const bill = detail.statements.find((entry) => entry.id === detail.plans[0].terms[0].statement_id);
    const context = await browser.newContext({ viewport, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    await context.addInitScript(() => {
      const NativeDate = Date;
      globalThis.Date = class extends NativeDate {
        constructor(...args) { super(...(args.length ? args : ['2026-11-14T12:00:00+08:00'])); }
        static now() { return NativeDate.parse('2026-11-14T12:00:00+08:00'); }
      };
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    const dialog = (name) => page.getByRole('dialog', { name, exact: true });
    const billRow = () => page.getByRole('tabpanel').locator(`tr[data-row-key="${bill.id}"]`).first();
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${account.id}`);
      await billRow().locator('.ant-table-row-expand-icon').click();
      let entries = page.locator('.liability-bill-items:visible');
      assert.equal(await entries.locator('tr.ant-table-row').count(), 1, 'one term is one bill row');
      assert.deepEqual(await primary(entries), ['¥332.87', '¥332.87']);
      assert.equal(await entries.getByRole('button', { name: '查看流水', exact: true }).count(), 2, 'principal and fee sources remain reachable');
      await entries.getByRole('button', { name: '查看分期', exact: true }).click();
      let modal = dialog('分期明细');
      assert.deepEqual(await primary(modal.locator('.liability-plan-table')), ['¥332.87', '¥332.87', '¥332.85', '¥332.77', '¥332.85', '¥332.77']);
      assert(await modal.getByText('已发生', { exact: false }).first().isVisible());
      await modal.getByRole('button', { name: '关闭', exact: true }).click();
      assert.equal(JSON.stringify((await api('GET', `/accounts/${account.id}/liability`)).plans[0]), originalPlan, 'reading history never rewrites amounts');
      await api('POST', '/liabilities/repayments', { request_key: crypto.randomUUID(), reason: '', account_id: account.id, from_account_id: bank.id, amount: '332.87', entry_date: '2026-11-14', mode: 'auto', targets: [] });
      detail = await api('GET', `/accounts/${account.id}/liability`);
      const activeFee = detail.items.find((item) => item.source_id === fee.id && item.kind === 'fee');
      assert.equal(detail.statements.find((entry) => entry.id === bill.id).remaining, '0');
      await page.reload();
      await page.getByRole('tab', { name: '分期计划', exact: true }).click();
      const history = page.locator('.liability-plan-table').first();
      assert.deepEqual(await primary(history), ['¥332.87', '¥0.00', '¥332.85', '¥332.77', '¥332.85', '¥332.77']);
      assert((await history.locator('.small').allTextContents()).includes(`未还手续费 ${currency(activeFee.remaining)}`));
      await history.scrollIntoViewIfNeeded();
      await shot(page, `liability-history-${viewport.width}`);
      await page.getByRole('button', { name: '调整未来期次 / 费用', exact: true }).first().click();
      modal = dialog('调整分期与费用');
      await modal.getByLabel('手续费', { exact: true }).nth(1).fill('0.09');
      await until(() => primary(modal.locator('.liability-term-preview')), (values) => values.join('|') === '¥332.87|¥332.86|¥332.85', 'draft totals reflect fee edits without reapplying paid principal');
      await modal.getByRole('button', { name: /取\s*消/, exact: true }).click();
      assert.equal((await api('GET', `/accounts/${account.id}/liability`)).plans[0].terms[1].fee_amount, '0.08', 'cancelled draft does not change history');
      await page.getByRole('tab', { name: '还款与抵扣', exact: true }).click();
      const feePaid = detail.allocations.filter((allocation) => allocation.debt_item_id === activeFee.id).reduce((sum, allocation) => sum + Number(allocation.amount), 0).toFixed(2);
      await page.getByText(`已抵扣手续费 ${currency(feePaid)}`, { exact: true }).waitFor();
      await page.getByRole('tab', { name: '账单', exact: true }).click();
      await billRow().locator('.ant-table-row-expand-icon').click();
      entries = page.locator('.liability-bill-items:visible');
      assert.deepEqual(await primary(entries), ['¥332.87', '¥0.00']);
      await shot(page, `liability-bill-${viewport.width}`);
      await page.goto(base + '/');
      const recentFee = page.locator('.recent-row').filter({ hasText: fee.note });
      await recentFee.getByText('分期手续费', { exact: true }).waitFor();
      assert.equal(await recentFee.getByText('分期手续费', { exact: true }).count(), 1);
      await page.goto(base + '/records');
      const recordFee = viewport.width < 768 ? page.locator('.txn-row').filter({ hasText: fee.note }) : page.getByRole('row').filter({ hasText: fee.note });
      await recordFee.getByText('分期手续费', { exact: true }).waitFor();
      assert((await recordFee.textContent()).includes('-¥0.08'), 'real fee expense stays visible after repayment');
      const assetAccount = await api('POST', '/accounts', { name: `历史资产费用-${viewport.width}`, type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 13, repayment_day: 22, repayment_month_offset: 0 });
      await api('POST', '/liabilities/installments', { request_key: crypto.randomUUID(), reason: '', account_id: assetAccount.id, amount: '300', entry_date: '2026-11-01', category_id: category.id, term_count: 3, first_statement_date: '2026-11-01', fee_mode: 'per_term', fee_input: '1', fee_account_id: bank.id, fee_category_id: category.id });
      const assetDetail = await api('GET', `/accounts/${assetAccount.id}/liability`);
      assert.equal(assetDetail.summary.total_debt, '300');
      await page.goto(base + `/liabilities/${assetAccount.id}?plan=${assetDetail.plans[0].id}`);
      const assetHistory = page.locator('.liability-plan-table').first();
      await until(() => primary(assetHistory), (values) => values.join('|') === '¥101.00|¥100.00|¥101.00|¥100.00|¥101.00|¥100.00', 'asset-paid historic fees are excluded from debt');
      assert.equal((await api('GET', `/accounts/${assetAccount.id}/liability`)).summary.total_debt, '300');
      const current = await api('GET', `/accounts/${account.id}/liability`);
      const payoff = { request_key: crypto.randomUUID(), reason: '验证历史显示保护', version: current.plans[0].version, all_remaining: true, from_account_id: bank.id, entry_date: '2026-11-14' };
      const payoffQuote = await api('POST', `/liabilities/installments/${created.plan_id}/settlement-preview`, payoff);
      await api('POST', `/liabilities/installments/${created.plan_id}/settle`, { ...payoff, quote_signature: payoffQuote.quote_signature, total_amount: payoffQuote.total_amount });
      detail = await api('GET', `/accounts/${account.id}/liability`);
      assert.equal(detail.summary.total_debt, '0');
      assert.equal(detail.summary.forecast_fees, '0');
      assert.deepEqual(detail.plans[0].terms.map((term) => Number(term.fee_amount)), [0.08, 0.08, 0.08], 'cancelled fee plans keep historical values');
      assert.equal(detail.transactions.filter((txn) => txn.kind === 'expense' && txn.liability_term_id).length, 3, 'each future fee is posted exactly once during payoff');
      await page.goto(base + `/liabilities/${account.id}?plan=${created.plan_id}`);
      const settled = page.locator('.liability-plan-table').first();
      const planned = detail.plans[0].terms.map((term) => currency(Number(term.principal) + Number(term.fee_amount)));
      await until(() => primary(settled), (values) => values.join('|') === planned.flatMap((total) => [total, '¥0.00']).join('|'), 'settled history keeps plans and clears real unpaid amounts');
      assert.equal(await settled.getByText('已结清', { exact: true }).count(), 3);
      assert.equal(detail.plans[0].terms.filter((term) => term.fee_status === 'posted').length, 3);
      settledAccounts.push(account.id);
      assert.deepEqual(errors, []);
      console.log(`PASS ${viewport.width}px: bill/source links, whole-term repayment, draft cancellation, fee expense visibility and settled history without rewriting money`);
    } catch (error) {
      await shot(page, `liability-history-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally {
      await context.close();
    }
  }
  await clock('2027-01-15');
  for (const id of settledAccounts) {
    const detail = await api('GET', `/accounts/${id}/liability`);
    assert.equal(detail.summary.total_debt, '0');
    assert.equal(detail.summary.overdue, '0');
    assert(!('pending_credit' in detail.summary));
    assert.equal(detail.transactions.filter((txn) => txn.kind === 'expense' && txn.liability_term_id).length, 3, 'early-paid fees are not posted again at their original dates');
  }
}, 'chromium');
