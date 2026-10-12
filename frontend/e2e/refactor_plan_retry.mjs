import assert from 'node:assert/strict';
import {
  withFixture,
  assertFixture,
  fixtureApi,
  login,
} from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5602';
const backend = process.env.LIABILITY_FIXTURE_API ?? 'http://127.0.0.1:18085';
await assertFixture(base, ['liability-browser']);
await assertFixture(backend, ['liability-browser']);
await withFixture(
  base,
  async (browser, cleanup) => {
    cleanup(async () => {
      const response = await fetch(backend + '/__test__/stop', {
        method: 'POST',
      });
      assert(response.ok, 'dispose isolated plan ledger');
    });
    const api = await fixtureApi(base);
    const forest = await api('GET', '/categories');
    const category = forest
      .flatMap((wrapper) => wrapper.children ?? [])
      .flatMap((root) => (root.children?.length ? root.children : [root]))
      .find((entry) => entry.kind === 'expense');
    const context = await browser.newContext({
      viewport: { width: 1280, height: 900 },
    });
    const page = await context.newPage();
    await login(page, base);
    for (const scenario of ['adjust', 'refund', 'changed']) {
      const card = await api('POST', '/accounts', {
        name: 'retry-' + scenario + '-' + crypto.randomUUID(),
        type: 'liability',
        opening_balance: '0',
        liability_type: 'huabei',
        statement_day: 31,
        repayment_day: 10,
        repayment_month_offset: 1,
      });
      const result = await api('POST', '/liabilities/installments', {
        account_id: card.id,
        amount: '90',
        entry_date: '2026-11-01',
        category_id: category.id,
        term_count: 3,
        first_statement_date: '2026-12-31',
        fee_mode: 'per_term',
        fee_input: '0',
        fee_account_id: card.id,
        fee_category_id: category.id,
        request_key: crypto.randomUUID(),
        reason: '',
      });
      let refund;
      if (scenario === 'refund') {
        const detail = await api('GET', `/accounts/${card.id}/liability`);
        refund = await api('POST', '/transactions', {
          kind: 'refund',
          amount: '9',
          account_id: card.id,
          category_id: category.id,
          related_txn_id: detail.plans[0].source_txn_id,
          entry_date: '2026-11-02',
        });
      }
      await page.goto(base + `/liabilities/${card.id}?plan=${result.plan_id}`);
      await page
        .getByRole('button', {
          name:
            scenario === 'refund' ? '核对退款后的期次' : '调整未来期次 / 费用',
          exact: true,
        })
        .click();
      const dialog = page.getByRole('dialog', {
        name: '调整分期与费用',
        exact: true,
      });
      await dialog.getByLabel('手续费', { exact: true }).first().fill('1');
      await dialog
        .getByLabel('核对 / 操作说明', { exact: true })
        .fill('核对未来费用');
      if (refund) {
        await dialog
          .getByLabel('已到账的本金退款记录', { exact: true })
          .click();
        await page
          .locator(
            '.ant-select-dropdown:visible .ant-select-item-option-content'
          )
          .filter({ hasText: '9' })
          .first()
          .click();
      }
      const endpoint = `/liabilities/installments/${result.plan_id}${refund ? '/refund-adjustments' : ''}`;
      const method = refund ? 'POST' : 'PATCH';
      const pattern = '**' + endpoint;
      const requests = [];
      let committed;
      await page.route(pattern, async (route) => {
        if (route.request().method() !== method) {
          await route.continue();
          return;
        }
        requests.push({
          url: new URL(route.request().url()).pathname,
          method: route.request().method(),
          data: route.request().postDataJSON(),
        });
        if (requests.length === 1) {
          const response = await route.fetch();
          assert.equal(response.status(), 200, await response.text());
          committed = (await response.json()).data;
          await route.fulfill({
            status: 504,
            json: { code: 20000, message: 'repair-response-lost' },
          });
        } else await route.continue();
      });
      const save = () =>
        dialog.getByRole('button', { name: /保\s*存/, exact: true }).click();
      await save();
      await page.getByText('repair-response-lost', { exact: true }).waitFor();
      const afterCommit = await api('GET', `/accounts/${card.id}/liability`);
      const refreshed = page.waitForResponse((response) =>
        response.url().endsWith(`/accounts/${card.id}/liability`)
      );
      await page.evaluate(() => {
        dispatchEvent(new Event('offline'));
        dispatchEvent(new Event('online'));
      });
      await refreshed;
      await page.waitForTimeout(150);
      if (scenario === 'changed') {
        await dialog.getByLabel('手续费', { exact: true }).first().fill('2');
        const response = page.waitForResponse(
          (reply) =>
            reply.request().method() === method &&
            reply.url().endsWith(endpoint)
        );
        await save();
        assert.equal((await response).status(), 409);
        assert.equal(
          requests[1].data.request_key,
          requests[0].data.request_key
        );
        await dialog.getByLabel('手续费', { exact: true }).first().fill('1');
      }
      const replay = page.waitForResponse(
        (reply) =>
          reply.request().method() === method && reply.url().endsWith(endpoint)
      );
      await save();
      const response = await replay;
      assert.equal(response.status(), 200, await response.text());
      assert.deepEqual(
        (await response.json()).data,
        committed,
        'replay returns the already completed operation'
      );
      assert.deepEqual(
        requests.at(-1),
        requests[0],
        'endpoint, body, key and version remain identical'
      );
      await dialog.waitFor({ state: 'hidden' });
      const afterReplay = await api('GET', `/accounts/${card.id}/liability`);
      assert.equal(
        afterReplay.operations.length,
        afterCommit.operations.length
      );
      assert.equal(
        afterReplay.transactions.length,
        afterCommit.transactions.length
      );
      assert.equal(afterReplay.account.balance, afterCommit.account.balance);
      assert.equal(
        afterReplay.summary.total_debt,
        afterCommit.summary.total_debt
      );
      await page.unroute(pattern);
      console.log(
        `PASS ${scenario}: committed response loss, background reload, stable replay and no duplicate operation`
      );
    }
    await context.close();
  },
  'chromium'
);
