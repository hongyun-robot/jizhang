import assert from 'node:assert/strict';
import path from 'node:path';
import { assertFixture, fixtureApi, launchBrowser, login, shot, until } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
const api = await fixtureApi(base);
const browser = await launchBrowser('chromium');
const categories = await api('GET', '/categories');
const leaves = (rows) => rows.flatMap((row) => row.children?.length ? leaves(row.children) : [row]);
const category = leaves(categories).find((row) => row.kind === 'expense');
assert(category);
const clock = async (date) => {
  const response = await fetch(base + '/api/v1/__test__/date', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date }) });
  assert.equal(response.status, 200, await response.text());
};
const label = (name) => new RegExp('^' + [...name].join('\\s*') + '$');
let checks = 0;
const check = (value, reason) => { assert(value, reason); console.log(`PASS ${++checks}: ${reason}`); };
try {
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    await clock('2026-11-01');
    const bank = await api('POST', '/accounts', { name: `提前还款银行-${viewport.width}`, type: 'asset', opening_balance: '5000' });
    const account = await api('POST', '/accounts', { name: `按期还款-${viewport.width}`, type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1 });
    const created = await api('POST', '/liabilities/installments', { request_key: crypto.randomUUID(), reason: '', account_id: account.id, amount: '3000', entry_date: '2026-11-01', category_id: category.id, term_count: 3, first_statement_date: '2026-11-01', fee_mode: 'per_term', fee_input: '20', fee_account_id: account.id, fee_category_id: category.id });
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
    let authorization = '';
    page.on('request', (request) => { authorization = request.headers().authorization || authorization; });
    page.on('pageerror', (error) => errors.push(error.message));
    const modal = (name) => page.getByRole('dialog', { name, exact: true });
    const choose = async (dialog, name, value) => {
      await dialog.getByLabel(name, { exact: true }).click();
      await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({ hasText: value }).first().click();
    };
    const rejected = async (endpoint, input, expected = 409) => {
      const response = await context.request.post(base + '/api/v1' + endpoint, { headers: { authorization }, data: input });
      assert.equal(response.status(), expected, await response.text());
    };
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${account.id}`);
      await page.getByRole('tab', { name: '分期计划', exact: true }).click();
      check(!((await page.locator('body').textContent()).includes('待抵扣')) && !((await page.locator('body').textContent()).includes('溢缴')), `${viewport.width}px: account has no unused-funds feature`);
      await page.getByRole('button', { name: '按期提前还款', exact: true }).click();
      let dialog = modal('按期提前还款');
      await choose(dialog, '还款期数', '还至第 2 期');
      await choose(dialog, '还款账户', bank.name);
      check(await dialog.getByLabel('平台实际剩余本金').count() === 0 && await dialog.getByLabel('实际结清手续费（手填）').count() === 0, `${viewport.width}px: principal and fee cannot be edited separately`);
      await dialog.getByRole('button', { name: '核对还款金额', exact: true }).click();
      await dialog.locator('.liability-preview').waitFor();
      check(/2,040\.00/.test(await dialog.locator('.liability-preview').textContent()), `${viewport.width}px: two future terms include both 20 fees`);
      let detail = await api('GET', `/accounts/${account.id}/liability`);
      check(detail.summary.total_debt === '3000' && detail.transactions.length === 1, `${viewport.width}px: preview never posts future fees`);
      const plan = detail.plans[0];
      const request = { request_key: crypto.randomUUID(), reason: '', version: plan.version, term_ids: [plan.terms[0].id, plan.terms[1].id], from_account_id: bank.id, entry_date: '2026-11-01' };
      const quote = await api('POST', `/liabilities/installments/${created.plan_id}/repayment-preview`, request);
      await rejected(`/liabilities/installments/${created.plan_id}/repay`, { ...request, quote_signature: quote.quote_signature, total_amount: '20' });
      await rejected('/transactions', { kind: 'transfer', amount: '500', account_id: bank.id, transfer_account_id: account.id, entry_date: '2026-11-01' });
      await rejected(`/liabilities/installments/${created.plan_id}/repay`, { ...request, fee_amount: '0' }, 400);
      check((await api('GET', `/accounts/${account.id}/liability`)).transactions.length === 1, `${viewport.width}px: partial, fee-only and obsolete inputs roll back`);
      await dialog.locator('.liability-preview').scrollIntoViewIfNeeded();
      await shot(page, `installment-early-repayment-${viewport.width}`);
      await page.screenshot({ path: path.resolve(`e2e/shots/refactor-frontend/installment-early-repayment-${viewport.width}.png`), fullPage: false });
      const confirmBounds = await dialog.getByRole('button', { name: '确认还款', exact: true }).boundingBox();
      check(confirmBounds && confirmBounds.y >= 0 && confirmBounds.y + confirmBounds.height <= viewport.height, `${viewport.width}px: confirm action stays inside the viewport`);
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${viewport.width}px: repayment modal has no horizontal overflow`);
      let submitted;
      page.on('request', (request) => {
        if (request.method() === 'POST' && request.url().endsWith(`/installments/${created.plan_id}/repay`)) submitted = request.postDataJSON();
      });
      if (viewport.width === 390) {
        let loseResponse = true;
        await page.route(`**/installments/${created.plan_id}/repay`, async (route) => {
          if (!loseResponse) { await route.continue(); return; }
          loseResponse = false;
          const response = await route.fetch();
          assert.equal(response.status(), 200);
          await route.abort('failed');
        });
        await dialog.getByRole('button', { name: '确认还款', exact: true }).click();
        await until(() => api('GET', `/accounts/${account.id}/liability`), (value) => value.plans[0].terms[1].status === 'settled', 'committed response-loss fixture');
        await dialog.getByRole('button', { name: '确认还款', exact: true }).click();
      } else {
        await dialog.getByRole('button', { name: '确认还款', exact: true }).click();
      }
      await dialog.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (value) => value.plans[0].terms[1].status === 'settled', 'two terms paid');
      check(detail.summary.total_debt === '1000' && detail.summary.forecast_fees === '20' && !('pending_credit' in detail.summary) && !('surplus' in detail.summary), `${viewport.width}px: two terms settled; only third term remains`);
      check(detail.transactions.filter((transaction) => transaction.kind === 'expense' && transaction.liability_term_id).length === 2 && detail.transactions.filter((transaction) => transaction.kind === 'transfer').length === 1, `${viewport.width}px: one cash transfer and one expense for each fee`);
      const balances = await api('GET', '/accounts');
      check(balances.find((row) => row.id === bank.id).balance === '2960', `${viewport.width}px: selected bank is charged exactly 2040 once`);
      const replay = await api('POST', `/liabilities/installments/${created.plan_id}/repay`, submitted);
      const operation = detail.operations.find((row) => row.kind === 'installment_repayment');
      check(replay.operation_id === operation.id, `${viewport.width}px: same attempt replays the original result`);
      await page.getByRole('button', { name: '提前结清', exact: true }).click();
      dialog = modal('提前结清');
      await choose(dialog, '还款账户', bank.name);
      await dialog.getByRole('button', { name: '核对还款金额', exact: true }).click();
      await dialog.locator('.liability-preview').waitFor();
      check(/1,020\.00/.test(await dialog.locator('.liability-preview').textContent()), `${viewport.width}px: full payoff includes the last future fee`);
      await dialog.getByRole('button', { name: '确认结清', exact: true }).click();
      await dialog.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (value) => value.plans[0].status === 'settled', 'all terms paid');
      check(detail.summary.total_debt === '0' && detail.transactions.filter((row) => row.kind === 'expense' && row.liability_term_id).length === 3, `${viewport.width}px: all fees are paid instead of cancelled`);
      const settlement = detail.operations.find((row) => row.kind === 'settle');
      await rejected('/transactions', { kind: 'refund', amount: '100', account_id: account.id, related_txn_id: detail.plans[0].source_txn_id, entry_date: '2026-11-01' });
      check((await api('GET', `/accounts/${account.id}/liability`)).summary.total_debt === '0', `${viewport.width}px: refund cannot create unused credit`);
      await page.getByRole('tab', { name: '操作记录', exact: true }).click();
      const row = page.getByRole('row').filter({ hasText: '提前结清' }).first();
      await row.getByRole('button', { name: '撤销并恢复', exact: true }).click();
      dialog = modal('撤销并恢复记录');
      await dialog.getByLabel('核对 / 操作说明', { exact: true }).fill('核对后撤销最后一期结清');
      await dialog.getByRole('button', { name: label('保存'), exact: true }).click();
      await dialog.waitFor({ state: 'hidden' });
      detail = await until(() => api('GET', `/accounts/${account.id}/liability`), (value) => value.operations.find((entry) => entry.id === settlement.id)?.reversed_at, 'settlement reversed');
      await api('POST', `/liabilities/operations/${operation.id}/reverse`, { request_key: crypto.randomUUID(), reason: '逆序撤销前两期' });
      detail = await api('GET', `/accounts/${account.id}/liability`);
      check(detail.summary.total_debt === '3000' && detail.summary.forecast_fees === '60' && detail.plans[0].terms.every((term) => term.fee_txn_id === null && term.status === 'scheduled'), `${viewport.width}px: consecutive operations reverse in order and restore fee plans`);
      check(errors.length === 0, `${viewport.width}px: no browser exceptions`);
    } catch (error) {
      await shot(page, `installment-early-repayment-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally {
      await context.close();
    }
  }
  console.log(`PASS ${checks} installment early repayment checks`);
} finally {
  await browser.close();
}
