import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, launchBrowser, login, shot, until } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
await assertFixture(base, ['liability-browser']);
const api = await fixtureApi(base);
const browser = await launchBrowser('chromium');
const leaves = (rows) => rows.flatMap((r) => r.children?.length ? leaves(r.children) : [r]);
const category = leaves(await api('GET', '/categories')).find((c) => c.kind === 'expense');
assert(category);
const clock = async (date) => {
  const response = await fetch(base + '/api/v1/__test__/date', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ date }) });
  assert.equal(response.status, 200, await response.text());
};
const createCard = (name) => api('POST', '/accounts', { name: name + crypto.randomUUID().slice(0, 6), type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1 });
const expense = (account, amount, date = '2026-10-15') => api('POST', '/transactions', { kind: 'expense', account_id: account, amount, entry_date: date, category_id: category.id });
const label = (name) => new RegExp('^' + [...name].join('\\s*') + '$');
let checks = 0;
const check = (value, name) => { assert(value, name); console.log(`PASS ${++checks}: ${name}`); };
try {
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 474, height: 884 }, { width: 390, height: 844 }]) {
    await clock('2026-10-15');
    const card = await createCard('普通提前还款');
    const bank = await api('POST', '/accounts', { name: '扣款资产' + viewport.width + crypto.randomUUID().slice(0, 6), type: 'asset', opening_balance: '5000' });
    const source = await expense(card.id, '1000');
    const context = await browser.newContext({ viewport, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
    await context.addInitScript(() => {
      const NativeDate = Date;
      globalThis.Date = class extends NativeDate {
        constructor(...args) { super(...(args.length ? args : ['2026-10-15T12:00:00+08:00'])); }
        static now() { return NativeDate.parse('2026-10-15T12:00:00+08:00'); }
      };
    });
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    let authorization = '';
    const errors = [];
    page.on('request', (r) => { authorization = r.headers().authorization || authorization; });
    page.on('pageerror', (e) => errors.push(e.message));
    const choose = async (dialog, name, value) => {
      await dialog.getByLabel(name, { exact: true }).click();
      if (name === '还款资金账户') {
        const search = page.locator('.selection-sheet:visible input');
        if (await search.count()) await search.first().fill(value);
        else if (await dialog.getByLabel(name, { exact: true }).isEditable()) await dialog.getByLabel(name, { exact: true }).fill(value);
      }
      await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({ hasText: value }).first().click();
    };
    const reject = async (endpoint, input, expected = 409) => {
      const response = await context.request.post(base + '/api/v1' + endpoint, { headers: { authorization }, data: input });
      assert.equal(response.status(), expected, await response.text());
    };
    try {
      await login(page, base);
      await page.goto(base + `/liabilities/${card.id}`);
      await page.getByRole('button', { name: label('还款'), exact: true }).click();
      let dialog = page.getByRole('dialog', { name: '记录还款', exact: true });
      check((await dialog.textContent()).includes('还已出账'), `${viewport.width}: defaults to issued scope`);
      await choose(dialog, '还款范围', '提前还未出账');
      await choose(dialog, '还款资金账户', bank.name);
      await dialog.getByLabel('还款金额', { exact: true }).fill('300');
      const save = dialog.getByRole('button', { name: /保\s*存/ });
      check(await save.isDisabled(), `${viewport.width}: confirmation requires a server quote`);
      await dialog.getByRole('button', { name: /核对还款金额/ }).click();
      await dialog.locator('.ordinary-repayment-confirmed').waitFor();
      check((await dialog.textContent()).includes('1,000.00'), `${viewport.width}: ordinary capacity excludes the issuance restriction`);
      await dialog.getByLabel('还款金额', { exact: true }).fill('301');
      check(await save.isDisabled(), `${viewport.width}: editing the amount invalidates confirmation`);
      if (viewport.width === 1440) {
        let release;
        const pending = new Promise((resolve) => { release = resolve; });
        let captured;
        const arrived = new Promise((resolve) => { captured = resolve; });
        await page.route('**/liabilities/repayments/preview', async (route) => { captured(); await pending; await route.continue(); });
        await dialog.getByRole('button', { name: /核对还款金额/ }).click();
        await arrived;
        await dialog.getByLabel('还款金额', { exact: true }).fill('300');
        release();
        await page.waitForResponse((r) => r.url().endsWith('/repayments/preview'));
        await page.unroute('**/liabilities/repayments/preview');
        check(await save.isDisabled() && await dialog.locator('.ordinary-repayment-confirmed').count() === 0, 'late response cannot confirm an edited draft');
        const input = { account_id: card.id, from_account_id: bank.id, amount: '300', entry_date: '2026-10-15', request_key: crypto.randomUUID(), mode: 'auto', targets: [], scope: 'unbilled_ordinary' };
        await reject('/liabilities/repayments/preview', { ...input, obsolete: true }, 400);
        await reject('/liabilities/repayments/preview', { ...input, scope: 'other' }, 400);
        await reject('/liabilities/repayments/preview', { ...input, entry_date: '2026-10-16' }, 400);
        await reject('/liabilities/repayments/preview', { ...input, amount: '1001' });
        await reject('/liabilities/repayments', { ...input, scope: 'issued' });
        check(true, 'HTTP rejects unknown fields/scope, future date, excess and legacy unissued payment');
      }
      await dialog.getByLabel('还款金额', { exact: true }).fill('300');
      await dialog.getByRole('button', { name: /核对还款金额/ }).click();
      await dialog.locator('.ordinary-repayment-confirmed').waitFor();
      await dialog.locator('.ordinary-repayment-confirmed').scrollIntoViewIfNeeded();
      await shot(page, `unbilled-ordinary-repayment-${viewport.width}`);
      await dialog.locator('.ant-modal-body').evaluate((el) => { el.scrollTop = 0; });
      await shot(page, `unbilled-ordinary-repayment-scope-${viewport.width}`);
      const bounds = await save.boundingBox();
      check(bounds && bounds.y >= 0 && bounds.y + bounds.height <= viewport.height, `${viewport.width}: save remains reachable in the viewport`);
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${viewport.width}: no page horizontal overflow`);
      if (viewport.width === 474) {
        let lose = true;
        let notifyLost;
        const responseLost = new Promise((resolve) => { notifyLost = resolve; });
        await page.route('**/liabilities/repayments', async (route) => {
          if (!lose) { await route.continue(); return; }
          lose = false;
          const response = await route.fetch();
          assert.equal(response.status(), 200, await response.text());
          await route.abort('failed');
          notifyLost();
        });
        await save.click();
        await responseLost;
        await page.getByText('Network Error', { exact: true }).waitFor();
        await until(() => api('GET', `/accounts/${card.id}/liability`), (d) => d.summary.unbilled === '700', 'response-loss commit');
        await save.click();
      } else await save.click();
      await dialog.waitFor({ state: 'hidden' });
      let d = await until(() => api('GET', `/accounts/${card.id}/liability`), (v) => v.summary.unbilled === '700', 'partial ordinary advance');
      check(d.credits.length === 1 && d.credits[0].mode === 'ordinary_advance' && d.transactions.filter((t) => t.kind === 'transfer').length === 1, `${viewport.width}: one fixed payment, including response-loss retry`);
      const balances = await api('GET', '/accounts');
      check(balances.find((a) => a.id === bank.id).balance === '4700', `${viewport.width}: selected bank charged 300 exactly once`);
      const operation = d.operations.find((o) => o.kind === 'repayment');
      check(JSON.parse(operation.result_json).repayment_targets[0].source_id === source.id, `${viewport.width}: actual purchase remains traceable`);
      await expense(card.id, '20', '2026-10-14');
      d = await api('GET', `/accounts/${card.id}/liability`);
      check(d.allocations.every((a) => d.items.find((i) => i.id === a.debt_item_id).source_id === source.id), `${viewport.width}: later backfill does not displace existing payment`);
      await clock('2026-11-01');
      d = await api('GET', `/accounts/${card.id}/liability`);
      check(d.summary.current_due === '720' && d.credits.length === 1, `${viewport.width}: normal issuance bills only unpaid 720`);
      await page.reload();
      await page.getByRole('tab', { name: '还款与抵扣', exact: true }).click();
      const row = page.getByRole('row').filter({ hasText: '未出账普通消费提前还款' }).first();
      await row.getByRole('button', { name: '撤销并恢复', exact: true }).click();
      dialog = page.getByRole('dialog', { name: '撤销并恢复记录', exact: true });
      await dialog.getByLabel('核对 / 操作说明', { exact: true }).fill('核对后撤销提前还款');
      await dialog.getByRole('button', { name: /保\s*存/ }).click();
      await dialog.waitFor({ state: 'hidden' });
      d = await until(() => api('GET', `/accounts/${card.id}/liability`), (v) => v.summary.current_due === '1020', 'advance reversal');
      check(d.credits.length === 0 && d.operations.find((o) => o.id === operation.id).reversed_at, `${viewport.width}: reversal restores the original debt after issuance`);
      check(errors.length === 0, `${viewport.width}: no browser exceptions`);
    } catch (error) {
      await shot(page, `unbilled-ordinary-repayment-failure-${viewport.width}`).catch(() => {});
      throw error;
    } finally { await context.close(); }
  }
  // Mixed bill and two real unissued monthly bills, using API to verify server authority.
  await clock('2026-10-15');
  const mixed = await createCard('混合提前还款');
  const banks = await api('GET', '/accounts');
  const bank = banks.find((a) => a.type === 'asset' && a.status === 'active' && a.asset_class !== 'fund');
  const firstOrdinary = await expense(mixed.id, '300');
  await expense(mixed.id, '500', '2026-11-15');
  await expense(mixed.id, '50', '2026-12-15');
  await api('POST', '/liabilities/installments', { account_id: mixed.id, amount: '1000', entry_date: '2026-10-15', term_count: 3, first_statement_date: '2026-10-15', category_id: category.id, fee_mode: 'per_term', fee_input: '20', fee_account_id: mixed.id, fee_category_id: category.id, request_key: crypto.randomUUID(), reason: '' });
  await clock('2026-11-15');
  let d = await api('GET', `/accounts/${mixed.id}/liability`);
  const november = d.statements.find((b) => b.cycle_end === '2026-11-30');
  const december = d.statements.find((b) => b.cycle_end === '2026-12-31');
  const firstItem = d.items.find((i) => i.source_id === firstOrdinary.id);
  await api('POST', `/statements/${firstItem.statement_id}/reconcile`, { request_key: crypto.randomUUID(), reason: '实际归入后期账单', complete: false, moves: [{ item_id: firstItem.id, statement_id: december.id }] });
  const input = { account_id: mixed.id, from_account_id: bank.id, amount: '200', entry_date: '2026-11-15', scope: 'unbilled_ordinary', mode: 'manual', targets: [{ statement_id: november.id, amount: '100' }, { statement_id: december.id, amount: '100' }], request_key: crypto.randomUUID(), reason: '' };
  const quote = await api('POST', '/liabilities/repayments/preview', input);
  check(quote.available_amount === '800', 'mixed installment bills expose only ordinary dated capacity, including moved consumption');
  await api('POST', '/liabilities/repayments', { ...input, quote_signature: quote.quote_signature });
  const after = await api('GET', `/accounts/${mixed.id}/liability`);
  check(after.plans[0].terms.every((t) => t.status !== 'settled') && after.credits.filter((c) => c.mode === 'ordinary_advance').length === 1, 'manual multi-bill advance leaves every installment untouched');
  const authResponse = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username: 'voucher-admin', password: 'voucher-test-only' }) });
  const token = (await authResponse.json()).data.access_token;
  const pending = { ...input, amount: '100', mode: 'auto', targets: [], request_key: crypto.randomUUID() };
  const pendingQuote = await api('POST', '/liabilities/repayments/preview', pending);
  const other = { ...pending, amount: '10', request_key: crypto.randomUUID() };
  const otherQuote = await api('POST', '/liabilities/repayments/preview', other);
  await api('POST', '/liabilities/repayments', { ...other, quote_signature: otherQuote.quote_signature });
  const stale = await fetch(base + '/api/v1/liabilities/repayments', { method: 'POST', headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' }, body: JSON.stringify({ ...pending, quote_signature: pendingQuote.quote_signature }) });
  check(stale.status === 409, 'a changed allocation invalidates the old quote even when the requested amount still fits');
  for (const role of ['member', 'recorder']) {
    const ownApi = await fixtureApi(base, 'voucher-' + role);
    const ownCard = await ownApi('POST', '/accounts', { name: '本人提前还款' + role, type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1 });
    const ownBank = (await ownApi('GET', '/accounts')).find((a) => a.type === 'asset' && a.status === 'active' && a.asset_class !== 'fund');
    const ownCategory = leaves(await ownApi('GET', '/categories')).find((c) => c.kind === 'expense');
    await ownApi('POST', '/transactions', { kind: 'expense', account_id: ownCard.id, category_id: ownCategory.id, amount: '50', entry_date: '2026-11-15' });
    const ownInput = { ...pending, account_id: ownCard.id, from_account_id: ownBank.id, amount: '20', request_key: crypto.randomUUID() };
    const ownQuote = await ownApi('POST', '/liabilities/repayments/preview', ownInput);
    await ownApi('POST', '/liabilities/repayments', { ...ownInput, quote_signature: ownQuote.quote_signature });
    check((await ownApi('GET', `/accounts/${ownCard.id}/liability`)).summary.unbilled === '30', role + ' can record only own ordinary advance');
  }
  const viewerAuth = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username: 'voucher-viewer', password: 'voucher-test-only' }) });
  const viewerToken = (await viewerAuth.json()).data.access_token;
  const forbidden = await fetch(base + '/api/v1/liabilities/repayments/preview', { method: 'POST', headers: { authorization: 'Bearer ' + viewerToken, 'content-type': 'application/json' }, body: JSON.stringify(pending) });
  check(forbidden.status === 403, 'viewer cannot use ordinary advance API');
  console.log(`PASS ${checks} ordinary advance checks`);
} finally {
  await browser.close();
}
