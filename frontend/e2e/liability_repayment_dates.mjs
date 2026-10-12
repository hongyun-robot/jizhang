import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
const fixture = process.env.LIABILITY_FIXTURE_API ?? 'http://127.0.0.1:18085';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  cleanup(async () => {
    const response = await fetch(fixture + '/__test__/stop', { method: 'POST' });
    assert(response.ok, 'dispose isolated ledger');
  });
  const today = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date());
  const clock = await fetch(base + '/api/v1/__test__/date', {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ date: today }),
  });
  assert.equal(clock.status, 200);
  const forest = await api('GET', '/categories');
  const category = forest.flatMap((wrapper) => wrapper.children ?? [])
    .flatMap((root) => root.children?.length ? root.children : [root])
    .find((entry) => entry.kind === 'expense');
  assert(category, 'fixture expense category');

  for (const scenario of [
    { name: 'desktop-change', viewport: { width: 1440, height: 1000 }, pending: false },
    { name: 'mobile-existing-setting', viewport: { width: 390, height: 850 }, pending: true },
  ]) {
    const account = await api('POST', '/accounts', {
      name: '还款日回归-' + crypto.randomUUID().slice(0, 8), type: 'liability', opening_balance: '0',
      liability_type: 'huabei', statement_day: 31, repayment_day: 1, repayment_month_offset: 1,
    });
    await api('POST', '/transactions', {
      kind: 'expense', account_id: account.id, amount: '2.30', entry_date: today, category_id: category.id,
    });
    const before = await api('GET', `/accounts/${account.id}/liability`);
    assert.equal(before.statements.length, 1);
    assert.equal(before.statements[0].issued_at, null);
    assert(before.statements[0].due_date.endsWith('-01'));
    const want = before.statements[0].due_date.slice(0, 8) + '08';
    const context = await browser.newContext({ viewport: scenario.viewport });
    cleanup(() => context.close());
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    const submissions = [];
    let showLegacyPending = scenario.pending;
    page.on('request', (request) => {
      if (request.method() === 'PUT' && new URL(request.url()).pathname.endsWith(`/accounts/${account.id}`)) {
        submissions.push(request.postDataJSON());
        showLegacyPending = false;
      }
    });
    if (scenario.pending) {
      // Replay the detail payload produced by the old version: the saved
      // 8th is pending while the existing unissued statement is still due on the 1st.
      await page.route(`**/accounts/${account.id}/liability`, async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        if (showLegacyPending) {
          const effective = new Date(before.statements[0].cycle_end + 'T00:00:00Z');
          effective.setUTCDate(effective.getUTCDate() + 1);
          body.data.rules.push({
            ...body.data.rules.at(-1), id: 999999, version: 2,
            repayment_day: 8, effective_from: effective.toISOString().slice(0, 10),
          });
        }
        await route.fulfill({ response, json: body });
      });
    }
    await login(page, base);
    await page.goto(base + `/liabilities/${account.id}`);
    await page.getByRole('button', { name: '账户设置', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '账户设置', exact: true });
    const repayment = dialog.getByLabel('还款日（1–31）', { exact: true });
    if (scenario.pending) assert.equal(await repayment.inputValue(), '8');
    else await repayment.fill('8');
    await dialog.getByText(/还款日期立即同步至当前及后续未出账账单/).waitFor();
    await dialog.getByRole('button', { name: /保\s*存/, exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    const after = await until(
      () => api('GET', `/accounts/${account.id}/liability`),
      (detail) => detail.statements[0]?.due_date === want,
      scenario.name + ' repayment date synchronized',
    );
    assert.equal(submissions.length, 1);
    assert.equal(submissions[0].statement_day, 31, 'save must apply pending repayment settings');
    assert.equal(submissions[0].repayment_day, 8);
    assert.equal(after.account.repayment_day, 8);
    assert.equal(after.statements.length, 1);
    assert.equal(after.statements[0].id, before.statements[0].id);
    assert.equal(after.statements[0].cycle_start, before.statements[0].cycle_start);
    assert.equal(after.statements[0].cycle_end, before.statements[0].cycle_end);
    assert.equal(after.summary.total_debt, '2.3');
    assert.deepEqual(after.transactions, before.transactions, 'settings do not rewrite the expense');
    await page.getByRole('cell', { name: want, exact: true }).waitFor();
    await page.getByText(/次月\s*8 日还款/).waitFor();
    assert.equal(await page.getByText(/新规则自 .* 生效/).count(), 0);
    if (scenario.pending) {
      await page.locator('.ant-table-content, .ant-table-body').evaluateAll((elements) => {
        for (const element of elements) element.scrollLeft = element.scrollWidth;
      });
    }
    await shot(page, 'repayment-dates-' + scenario.name);
    console.log('PASS ' + scenario.name + ': existing ¥2.30 statement changes from the 1st to the 8th');
  }
}, 'chromium');
