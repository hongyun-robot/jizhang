import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, launchBrowser, login, shot, until } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
const api = await fixtureApi(base);
const browser = await launchBrowser('chromium');
const leaves = (rows) => rows.flatMap((row) => row.children?.length ? leaves(row.children) : [row]);
const category = leaves(await api('GET', '/categories')).find((row) => row.kind === 'expense');
assert(category);
const date = '2026-11-13';
const sourceDate = '2026-11-10';
const repaymentDate = '2026-11-12';
const label = (name) => new RegExp('^' + [...name].join('\\s*') + '$');
const check = (value, message) => { assert(value, message); console.log('PASS ' + message); };

try {
  const clock = await fetch(base + '/api/v1/__test__/date', {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date }),
  });
  assert.equal(clock.status, 200, await clock.text());
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 520, height: 883 }, { width: 390, height: 844 }]) {
    const suffix = viewport.width + '-' + crypto.randomUUID().slice(0, 6);
    const bank = await api('POST', '/accounts', { name: '提前还款资金' + suffix, type: 'asset', opening_balance: '5000' });
    const card = await api('POST', '/accounts', {
      name: '第二期提前还款' + suffix, type: 'liability', opening_balance: '0',
      liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1,
    });
    const created = await api('POST', '/liabilities/installments', {
      request_key: crypto.randomUUID(), account_id: card.id, amount: '3000', entry_date: sourceDate,
      category_id: category.id, term_count: 3, first_statement_date: date, fee_mode: 'per_term',
      fee_input: '20', fee_account_id: card.id, fee_category_id: category.id,
    });
    let detail = await api('GET', `/accounts/${card.id}/liability`);
    const first = {
      request_key: crypto.randomUUID(), version: detail.plans[0].version,
      term_ids: [detail.plans[0].terms[0].id], from_account_id: bank.id, entry_date: sourceDate,
    };
    const quote = await api('POST', `/liabilities/installments/${created.plan_id}/repayment-preview`, first);
    await api('POST', `/liabilities/installments/${created.plan_id}/repay`, {
      ...first, quote_signature: quote.quote_signature, total_amount: quote.total_amount,
    });
    // Keep ordinary debt in the same account, matching the reported ¥8.28 case.
    const ordinaryExpense = await api('POST', '/transactions', {
      kind: 'expense', account_id: card.id, amount: '8.28', entry_date: sourceDate, category_id: category.id,
    });
    detail = await api('GET', `/accounts/${card.id}/liability`);
    check(detail.plans[0].terms[0].status === 'settled' && detail.plans[0].terms[1].status === 'scheduled', `${viewport.width}: first term paid, second term unbilled`);
    const second = await api('POST', `/liabilities/installments/${created.plan_id}/repayment-preview`, {
      version: detail.plans[0].version, term_ids: [detail.plans[0].terms[1].id],
      from_account_id: bank.id, entry_date: date,
    });
    check(second.terms.length === 1 && second.terms[0].number === 2 && second.total_amount === '1020', `${viewport.width}: server allows early repayment of the second term`);
    const before = detail.transactions.length;
    const context = await browser.newContext({ viewport, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    await context.addInitScript(() => {
      const NativeDate = Date;
      globalThis.Date = class extends NativeDate {
        constructor(...args) { super(...(args.length ? args : ['2026-11-13T12:00:00+08:00'])); }
        static now() { return NativeDate.parse('2026-11-13T12:00:00+08:00'); }
      };
    });
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    const choose = async (dialog, field, value) => {
      await dialog.getByLabel(field, { exact: true }).click();
      const search = page.locator('.selection-sheet:visible input');
      if (await search.count()) await search.first().fill(value);
      await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({ hasText: value }).first().click();
    };
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${card.id}`);
      await page.getByRole('button', { name: label('还款'), exact: true }).click();
      const ordinary = page.getByRole('dialog', { name: '记录还款', exact: true });
      check((await ordinary.textContent()).includes('一起还（含未出账和分期）'), `${viewport.width}: one combined payment is the default`);
      await choose(ordinary, '还款资金账户', bank.name);
      await ordinary.getByLabel('实际还款日期', { exact: true }).click();
      await page.locator(`.ant-picker-dropdown:visible [title="${repaymentDate}"]`).click();
      await ordinary.getByLabel('还款金额', { exact: true }).fill('1028.28');
      const confirm = ordinary.getByRole('button', { name: label('保存'), exact: true });
      check(await confirm.isDisabled(), `${viewport.width}: payment requires a server quote`);
      await ordinary.getByRole('button', { name: /核对还款金额/ }).click();
      await ordinary.locator('.combined-repayment-preview').waitFor();
      check(/1,028\.28/.test(await ordinary.locator('.combined-repayment-preview').textContent()) && /8\.28/.test(await ordinary.locator('.combined-repayment-preview').textContent()) && /第 2 期/.test(await ordinary.locator('.combined-repayment-preview').textContent()), `${viewport.width}: a single preview includes ordinary consumption and the second term`);
      await ordinary.getByLabel('还款金额', { exact: true }).fill('1028.29');
      check(await confirm.isDisabled(), `${viewport.width}: changing the total invalidates the old quote`);
      await ordinary.getByLabel('还款金额', { exact: true }).fill('1028.28');
      await ordinary.getByRole('button', { name: /核对还款金额/ }).click();
      await ordinary.locator('.combined-repayment-preview').waitFor();
      detail = await api('GET', `/accounts/${card.id}/liability`);
      check(detail.transactions.length === before, `${viewport.width}: opening and quoting do not move money`);
      await ordinary.locator('.combined-repayment-preview').scrollIntoViewIfNeeded();
      await shot(page, `combined-repayment-preview-${viewport.width}`);
      const bounds = await confirm.boundingBox();
      check(bounds && bounds.y >= 0 && bounds.y + bounds.height <= viewport.height, `${viewport.width}: confirm remains visible`);
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${viewport.width}: no horizontal page overflow`);
      if (viewport.width === 390) {
        let loseResponse = true;
        await page.route('**/liabilities/repayments', async (route) => {
          if (!loseResponse) { await route.continue(); return; }
          loseResponse = false;
          assert.equal((await route.fetch()).status(), 200);
          await route.abort('failed');
        });
        await confirm.click();
        await until(() => api('GET', `/accounts/${card.id}/liability`), (value) => value.plans[0].terms[1].status === 'settled', 'combined payment committed before response loss');
        await confirm.click();
      } else await confirm.click();
      await ordinary.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${card.id}/liability`), (value) => value.plans[0].terms[1].status === 'settled', 'second term settled');
      check(detail.plans[0].terms[2].status === 'scheduled' && detail.summary.total_debt === '1000', `${viewport.width}: ordinary consumption and second term paid together; third term stays scheduled`);
      check(detail.transactions.length === before + 2, `${viewport.width}: exactly one fee expense and one repayment transfer`);
      check(detail.transactions.some((row) => row.kind === 'transfer' && row.entry_date.startsWith(repaymentDate)), `${viewport.width}: actual repayment date is preserved in the saved transfer`);
      const accounts = await api('GET', '/accounts');
      assert.equal(accounts.find((row) => row.id === bank.id).balance, '2951.72', `${viewport.width}: bank pays 1028.28 exactly once for both debts`);
      const combined = detail.operations.find((operation) => JSON.parse(operation.result_json).repayment_scope === 'all');
      assert(combined);
      await page.getByRole('tab', { name: '还款与抵扣', exact: true }).click();
      await page.getByText('普通消费与分期合并还款').waitFor();
      const other = await api('POST', '/liabilities/installments', {
        request_key: crypto.randomUUID(), account_id: card.id, amount: '600', entry_date: sourceDate,
        category_id: category.id, term_count: 2, first_statement_date: date, fee_mode: 'per_term',
        fee_input: '0', fee_account_id: card.id, fee_category_id: category.id, note: '另一笔分期' + suffix,
      });
      await page.reload();
      await page.getByRole('button', { name: label('还款'), exact: true }).click();
      await choose(ordinary, '还款资金账户', bank.name);
      await ordinary.getByRole('button', { name: '全部还清', exact: true }).click();
      assert.equal(await ordinary.getByLabel('还款金额', { exact: true }).inputValue(), '1620.00');
      await ordinary.getByRole('button', { name: /核对还款金额/ }).click();
      await ordinary.locator('.combined-repayment-preview').waitFor();
      check(await ordinary.locator('.liability-term-preview').filter({ hasText: '分期 #' }).count() === 3, `${viewport.width}: all remaining terms across multiple plans share one quote`);
      await ordinary.getByRole('button', { name: label('保存'), exact: true }).click();
      await ordinary.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${card.id}/liability`), (value) => value.summary.total_debt === '0', 'all remaining debts settled');
      check(detail.plans.every((plan) => plan.status === 'settled'), `${viewport.width}: one save clears all remaining plans`);
      const last = detail.operations.filter((operation) => JSON.parse(operation.result_json).repayment_scope === 'all').find((operation) => operation.id !== combined.id);
      assert(last);
      await api('POST', `/liabilities/operations/${last.id}/reverse`, { request_key: crypto.randomUUID(), reason: '撤销全部还清测试' });
      await api('POST', `/liabilities/operations/${combined.id}/reverse`, { request_key: crypto.randomUUID(), reason: '撤销普通消费与第二期合并还款' });
      detail = await api('GET', `/accounts/${card.id}/liability`);
      check(detail.summary.total_debt === '2608.28' && detail.summary.forecast_fees === '40', `${viewport.width}: reversal restores ordinary debt, unpaid terms and planned fees together`);
      await api('DELETE', `/transactions/${ordinaryExpense.id}`);
      await page.reload();
      await page.getByRole('button', { name: label('还款'), exact: true }).click();
      await ordinary.getByRole('button', { name: '全部还清', exact: true }).click();
      assert.equal(await ordinary.getByLabel('还款金额', { exact: true }).inputValue(), '2640.00');
      await ordinary.getByRole('button', { name: label('取消'), exact: true }).click();
      check(errors.length === 0, `${viewport.width}: no browser exceptions`);
    } catch (error) {
      await shot(page, `installment-repayment-entry-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally {
      await context.close();
    }
  }
} finally {
  await browser.close();
}
