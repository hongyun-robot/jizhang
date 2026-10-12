import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5602';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  cleanup(async () => { const response = await fetch('http://127.0.0.1:18085/__test__/stop', { method: 'POST' }); assert(response.ok, 'dispose isolated ledger'); });
  const account = await api('POST', '/accounts', { name: 'preview-' + crypto.randomUUID(), type: 'liability', opening_balance: '0', liability_type: 'huabei', statement_day: 31, repayment_day: 10, repayment_month_offset: 1 });
  const forest = await api('GET', '/categories');
  const category = forest.flatMap((wrapper) => wrapper.children ?? []).flatMap((root) => root.children?.length ? root.children : [root]).find((entry) => entry.kind === 'expense');
  const result = await api('POST', '/liabilities/installments', { request_key: crypto.randomUUID(), reason: '', account_id: account.id, amount: '90', entry_date: '2027-01-01', category_id: category.id, term_count: 3, first_statement_date: '2027-02-28', fee_mode: 'per_term', fee_input: '0', fee_account_id: account.id, fee_category_id: category.id });
  const context = await browser.newContext({ viewport: { width: 390, height: 850 } });
  const page = await context.newPage();
  await login(page, base);
  const requests = [];
  await page.route(`**/liabilities/installments/${result.plan_id}/settle`, async (route) => { requests.push(route.request().postDataJSON()); await new Promise((resolve) => setTimeout(resolve, 300)); await route.fulfill({ status: 500, json: { code: 20000, message: 'test write failed' } }); });
  try {
  await page.goto(base + `/liabilities/${account.id}?plan=${result.plan_id}`);
  await page.getByRole('button', { name: '提前结清', exact: true }).last().click();
  const dialog = page.getByRole('dialog', { name: '提前结清', exact: true });
  await dialog.getByLabel('实际结清日期', { exact: true }).fill('2027-01-07');
  await dialog.getByLabel('实际结清日期', { exact: true }).press('Enter');
  await dialog.getByRole('button', { name: '核对结清预览', exact: true }).click();
  await dialog.locator('.liability-preview').waitFor();
  await dialog.getByLabel('实际结清手续费（手填）', { exact: true }).fill('1');
  assert.equal(await dialog.locator('.liability-preview').count(), 0, 'field edit invalidates preview');
  await dialog.getByRole('button', { name: '确认结清', exact: true }).click();
  await page.getByText('请先核对结清预览', { exact: true }).waitFor();
  assert.equal(requests.length, 0);
  await dialog.getByRole('button', { name: '核对结清预览', exact: true }).click();
  await dialog.locator('.liability-preview').waitFor();
  await dialog.getByRole('button', { name: '确认结清', exact: true }).dblclick();
  await until(() => requests.length, (count) => count === 1, 'single failed write');
  assert.equal(await dialog.getByLabel('实际结清手续费（手填）', { exact: true }).inputValue(), '1');
  await page.getByText('test write failed', { exact: true }).first().waitFor();
  await dialog.getByRole('button', { name: '确认结清', exact: true }).click();
  await until(() => requests.length, (count) => count === 2, 'retry');
  assert.equal(requests[0].request_key, requests[1].request_key);
  assert.equal(requests[0].version, requests[1].version);
  await context.close();
  console.log('PASS edited settlement preview, validation-time submit guard, failed draft and stable retry key/version');
  } catch (error) { console.error('SETTLEMENT PAGE', page.url(), await page.locator('body').innerText()); await shot(page, 'settlement-failure'); throw error; }
}, 'chromium');
