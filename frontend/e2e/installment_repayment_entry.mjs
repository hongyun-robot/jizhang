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
      await choose(ordinary, '还款范围', '提前还未出账');
      await choose(ordinary, '还款资金账户', bank.name);
      await ordinary.getByLabel('实际还款日期', { exact: true }).click();
      await page.locator(`.ant-picker-dropdown:visible [title="${repaymentDate}"]`).click();
      await shot(page, `installment-repayment-entry-${viewport.width}`);
      await ordinary.getByRole('button', { name: '按期提前还款', exact: true }).click();
      const repayment = page.getByRole('dialog', { name: '按期提前还款', exact: true });
      await repayment.getByLabel('还款期数', { exact: true }).waitFor();
      check((await repayment.textContent()).includes('还至第 2 期 · 共 1 期'), `${viewport.width}: next unpaid term defaults to the second term`);
      check((await repayment.textContent()).includes(bank.name), `${viewport.width}: selected repayment account is preserved`);
      assert.equal(await repayment.getByLabel('实际还款日期', { exact: true }).inputValue(), repaymentDate);
      const confirm = repayment.getByRole('button', { name: '确认还款', exact: true });
      check(await confirm.isDisabled(), `${viewport.width}: payment requires a server quote`);
      await repayment.getByRole('button', { name: '核对还款金额', exact: true }).click();
      await repayment.locator('.liability-preview').waitFor();
      check(/1,020\.00/.test(await repayment.locator('.liability-preview').textContent()), `${viewport.width}: second-term quote includes its exact principal and fee`);
      detail = await api('GET', `/accounts/${card.id}/liability`);
      check(detail.transactions.length === before, `${viewport.width}: opening and quoting do not move money`);
      await shot(page, `installment-repayment-second-term-${viewport.width}`);
      const bounds = await confirm.boundingBox();
      check(bounds && bounds.y >= 0 && bounds.y + bounds.height <= viewport.height, `${viewport.width}: confirm remains visible`);
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${viewport.width}: no horizontal page overflow`);
      await confirm.click();
      await repayment.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${card.id}/liability`), (value) => value.plans[0].terms[1].status === 'settled', 'second term settled');
      check(detail.plans[0].terms[2].status === 'scheduled' && detail.summary.total_debt === '1008.28', `${viewport.width}: only second term paid; third term and ordinary debt preserved`);
      check(detail.transactions.length === before + 2, `${viewport.width}: exactly one fee expense and one repayment transfer`);
      check(detail.transactions.some((row) => row.kind === 'transfer' && row.entry_date.startsWith(repaymentDate)), `${viewport.width}: actual repayment date is preserved in the saved transfer`);
      const accounts = await api('GET', '/accounts');
      assert.equal(accounts.find((row) => row.id === bank.id).balance, '2960', `${viewport.width}: bank pays first and second terms exactly once`);
      await api('DELETE', `/transactions/${ordinaryExpense.id}`);
      const other = await api('POST', '/liabilities/installments', {
        request_key: crypto.randomUUID(), account_id: card.id, amount: '600', entry_date: sourceDate,
        category_id: category.id, term_count: 2, first_statement_date: date, fee_mode: 'per_term',
        fee_input: '0', fee_account_id: card.id, fee_category_id: category.id, note: '另一笔分期' + suffix,
      });
      await page.reload();
      await page.getByRole('button', { name: label('还款'), exact: true }).click();
      await choose(ordinary, '还款范围', '提前还未出账');
      await ordinary.getByRole('button', { name: '按期提前还款', exact: true }).nth(1).waitFor();
      check(await ordinary.getByRole('button', { name: '按期提前还款', exact: true }).count() === 2, `${viewport.width}: multiple plans are available even without ordinary debt`);
      await ordinary.locator('.liability-toolbar').filter({ hasText: '另一笔分期' + suffix }).getByRole('button', { name: '按期提前还款', exact: true }).click();
      const otherQuote = page.waitForResponse((response) => response.url().endsWith(`/installments/${other.plan_id}/repayment-preview`));
      await repayment.getByRole('button', { name: '核对还款金额', exact: true }).click();
      const otherPreview = (await (await otherQuote).json()).data;
      check(otherPreview.plan_id === other.plan_id && otherPreview.total_amount === '300', `${viewport.width}: selected plan is quoted without mixing other plans`);
      await repayment.getByRole('button', { name: label('取消'), exact: true }).click();
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
