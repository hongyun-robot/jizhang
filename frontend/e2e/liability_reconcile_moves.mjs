import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
await withFixture(base, async (browser) => {
  const api = await fixtureApi(base);
  const clock = async (date) => {
    const response = await fetch(base + '/api/v1/__test__/date', {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date }),
    });
    assert(response.ok, await response.text());
  };
  const forest = await api('GET', '/categories');
  const category = forest.flatMap((wrapper) => wrapper.children ?? [])
    .flatMap((root) => root.children?.length ? root.children : [root]).find((entry) => entry.kind === 'expense');
  assert(category);
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    await clock('2026-10-13');
    const account = await api('POST', '/accounts', {
      name: `归属核对-${viewport.width}-${crypto.randomUUID()}`, type: 'liability', opening_balance: '0',
      liability_type: 'huabei',
    });
    await api('PUT', `/accounts/${account.id}/liability-settings`, {
      liability_type: 'huabei', statement_day: 31, repayment_day: 8, repayment_month_offset: 1,
    });
    await api('PUT', `/accounts/${account.id}/liability-settings`, {
      liability_type: 'huabei', statement_day: 1, repayment_day: 8, repayment_month_offset: 0,
    });
    await clock('2026-11-05');
    const source = await api('POST', '/transactions', {
      kind: 'expense', amount: '3', account_id: account.id, category_id: category.id,
      entry_date: '2026-11-01', note: '补电动车披风',
    });
    const before = await api('GET', `/accounts/${account.id}/liability`);
    const item = before.items.find((entry) => entry.source_id === source.id);
    const original = before.statements.find((entry) => entry.id === item.statement_id);
    const target = before.statements.find((entry) => entry.cycle_end === '2026-12-01');
    assert.equal(original.due_date, '2026-11-08');
    assert.equal(target?.due_date, '2026-12-08');
    const context = await browser.newContext({ viewport, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    const page = await context.newPage();
    const errors = [];
    let submissions = 0;
    page.on('pageerror', (error) => errors.push(error.message));
    page.on('request', (request) => {
      if (request.url().endsWith(`/statements/${original.id}/reconcile`) && request.method() === 'POST') submissions++;
    });
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${account.id}`);
      await page.locator(`tr[data-row-key="${original.id}"]`).getByRole('button', { name: '核对 / 调整', exact: true }).click();
      const modal = page.getByRole('dialog', { name: '核对账单', exact: true });
      const reference = modal.getByLabel('平台账单参考金额（仅用于核对）', { exact: true });
      await reference.fill('3');
      await modal.getByRole('button', { name: '调整消费归属', exact: true }).click();
      const choose = async (index, text) => {
        await modal.locator('.liability-form-row').locator('.ant-select, .selection-trigger').nth(index).click();
        await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item')
          .filter({ hasText: text }).first().click();
      };
      await choose(0, '补电动车披风');
      await choose(1, '2026-12-08');
      await modal.getByLabel('核对 / 操作说明').fill('11月消费调整至12月还款');
      await until(() => modal.locator('p').filter({ hasText: '调整后系统账单' }).textContent(),
        (text) => text.includes('¥0.00'), 'preview uses the adjusted original bill');
      assert((await modal.locator('p').filter({ hasText: '与调整后系统差异' }).textContent()).includes('¥3.00'));
      await modal.getByRole('button', { name: /保\s*存/, exact: true }).click();
      await modal.locator('.ant-form-item-explain-error').filter({ hasText: '调整后本账单金额为 ¥0.00' }).waitFor();
      assert.equal(submissions, 0, 'mismatched completion is caught before sending a request');
      assert.equal((await api('GET', `/accounts/${account.id}/liability`)).items.find((entry) => entry.id === item.id).statement_id, original.id);
      await shot(page, `liability-reconcile-move-${viewport.width}`);
      if (viewport.width < 768) {
        await modal.getByRole('checkbox', { name: '差异已解决，标记核对完成' }).uncheck();
      } else {
        await reference.fill('0');
      }
      await modal.getByRole('button', { name: /保\s*存/, exact: true }).click();
      await modal.waitFor({ state: 'hidden' });
      const after = await api('GET', `/accounts/${account.id}/liability`);
      const bill = after.statements.find((entry) => entry.id === original.id);
      const expense = after.transactions.find((entry) => entry.id === source.id);
      assert.equal(after.items.find((entry) => entry.id === item.id).statement_id, target.id);
      assert.equal(after.summary.total_debt, before.summary.total_debt);
      assert.equal(after.summary.current_due, '0');
      assert.equal(after.summary.unbilled, '3');
      assert.equal(bill.amount, '0');
      assert.equal(bill.snapshot_amount, original.snapshot_amount);
      assert.equal(!!bill.reconciled_at, viewport.width >= 768);
      assert.equal(expense.entry_date.slice(0, 10), '2026-11-01');
      assert.equal(expense.period_key, source.period_key);
      assert.equal(expense.amount, '3');
      assert.equal(submissions, 1);
      assert.deepEqual(errors, []);
      console.log(`PASS ${viewport.width}: correct preview, local validation, ${viewport.width < 768 ? 'move without completing' : 'complete with zero reference'}, intact expense`);
    } finally {
      await context.close();
    }
  }
}, 'chromium');
