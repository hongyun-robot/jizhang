import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { assertFixture, fixtureApi, launchBrowser, login } from './support/fixture.mjs';

// Write only to the local disposable fixture.
const base = process.env.EXPORT_E2E_BASE ?? 'http://127.0.0.1:5619';
await assertFixture(base);
const browser = await launchBrowser('chromium');
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, acceptDownloads: true });
const page = await context.newPage();
const prefix = 'export-' + Date.now();
const output = path.resolve('e2e/downloads', prefix);
fs.mkdirSync(output, { recursive: true });
const shots = path.resolve('e2e/shots/refactor-frontend');
fs.mkdirSync(shots, { recursive: true });
const created = [], errors = [];
page.on('pageerror', (error) => errors.push(error.message));

// RFC4180, including quoted notes with embedded newlines.
function parseCsv(bytes) {
  assert.deepEqual([...bytes.subarray(0, 3)], [0xef, 0xbb, 0xbf]);
  const text = bytes.toString('utf8').slice(1), rows = [];
  let row = [], value = '', quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === '"') {
      if (quoted && text[i + 1] === '"') { value += '"'; i++; }
      else quoted = !quoted;
    } else if (!quoted && ch === ',') { row.push(value); value = ''; }
    else if (!quoted && ch === '\r' && text[i + 1] === '\n') {
      row.push(value); rows.push(row); row = []; value = ''; i++;
    } else value += ch;
  }
  assert.equal(quoted, false);
  if (value || row.length) rows.push([...row, value]);
  return rows;
}
const button = () => page.getByRole('button', { name: '导出 Excel', exact: true });
async function download(format = 'xlsx') {
  const action = async () => { if (format === 'xlsx') await button().click();
  else {
    const chooser = page.getByRole('button', { name: '选择导出格式', exact: true });
    await chooser.focus(); await chooser.press('Enter');
    await page.getByRole('menuitem', { name: '导出 CSV' }).click();
  } };
  const [result] = await Promise.all([page.waitForEvent('download'), action()]);
  assert(result.suggestedFilename().endsWith('.' + format));
  const file = path.join(output, Date.now() + '-' + result.suggestedFilename());
  await result.saveAs(file);
  assert.equal(await result.failure(), null);
  const bytes = fs.readFileSync(file);
  await page.getByRole('menuitem', { name: '导出 CSV' }).waitFor({ state: 'hidden' });
  if (format === 'xlsx') assert.equal(bytes.subarray(0, 2).toString(), 'PK');
  return bytes;
}
const route = (keyword = prefix, extra = '') => base + '/records?from=2026-10-01&to=2026-10-31&keyword=' + encodeURIComponent(keyword) + extra;
let api;
try {
  await login(page, base);
  api = await fixtureApi(base);
  const accounts = await api('GET', '/accounts');
  const categories = await api('GET', '/categories');
  const account = accounts.find((a) => a.name === '测试现金');
  const flatten = (nodes) => nodes.flatMap((node) => [node, ...flatten(node.children ?? [])]);
  const category = flatten(categories).find((c) => c.id > 0 && c.kind === 'expense' && !c.children?.length);
  const originalBalance = account.balance;
  for (let i = 0; i < 45; i++) {
    const row = await api('POST', '/transactions', { kind: 'expense', amount: i % 2 ? '2.34' : '1.23', account_id: account.id, category_id: category.id, entry_date: '2026-10-10', note: prefix + '-' + i + (i === 0 ? '\r\n早餐,"加蛋"' : '') });
    created.push(row.id);
  }
  const balanceBefore = (await api('GET', '/accounts')).find((a) => a.id === account.id).balance;
  const list = await api('GET', '/transactions?keyword=' + prefix + '&order=custom&page_size=100');
  assert.equal(list.total, 45);
  await page.goto(route(prefix, '&page=2'));
  await button().waitFor();
  await download();
  const csv = parseCsv(await download('csv'));
  assert.deepEqual(csv[0], ['日期', '类型', '分类', '账户', '转入账户', '金额', '备注', '来源', '用途', '券编号', '余额影响', '消费影响', '平台 / 商家']);
  assert.equal(csv.length, 46);
  assert.deepEqual(csv.slice(1).map((r) => r[6]), list.items.map((r) => r.note));
  await page.screenshot({ path: path.join(shots, 'export-desktop.png') });
  await page.goto(route(prefix, '&amount_min=2.00&kind=expense'));
  assert.equal(parseCsv(await download('csv')).length, 23);
  await download();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(route());
  await download();
  assert.equal(parseCsv(await download('csv')).length, 46);
  await page.screenshot({ path: path.join(shots, 'export-mobile.png') });
  await page.goto(route(prefix + '-missing'));
  assert.equal(parseCsv(await download('csv')).length, 1);
  await download();
  await page.goto(route());
  let downloads = 0;
  page.on('download', () => downloads++);
  await page.route('**/transactions/export?**', (r) => r.fulfill({ status: 413, contentType: 'application/json', body: JSON.stringify({ code: 10001, message: 'Excel 最多支持 100,000 条流水，请缩小筛选范围或选择 CSV' }) }));
  await button().click();
  await page.getByText('Excel 最多支持 100,000 条流水，请缩小筛选范围或选择 CSV', { exact: true }).waitFor();
  assert.equal(downloads, 0);
  assert.equal(await button().isEnabled(), true);
  await page.unroute('**/transactions/export?**');
  await download();
  let requests = 0, release;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route('**/transactions/export?**', async (r) => { requests++; await gate; await r.continue(); });
  await button().evaluate((el) => { el.click(); el.click(); });
  await page.waitForFunction(() => document.querySelector('.ant-dropdown-button button')?.disabled);
  const completed = page.waitForEvent('download');
  release(); await completed;
  assert.equal(requests, 1);
  await page.unroute('**/transactions/export?**');
  assert.equal((await api('GET', '/accounts')).find((a) => a.id === account.id).balance, balanceBefore);
  assert.equal((await api('GET', '/transactions?keyword=' + prefix)).total, 45);
  for (const id of created.splice(0)) await api('DELETE', '/transactions/' + id);
  assert.equal((await api('GET', '/accounts')).find((a) => a.id === account.id).balance, originalBalance);
  const viewer = await context.newPage();
  await login(viewer, base, 'voucher-viewer');
  await viewer.goto(route());
  const viewerDownload = viewer.waitForEvent('download');
  await viewer.getByRole('button', { name: '导出 Excel', exact: true }).click();
  assert((await viewerDownload).suggestedFilename().endsWith('.xlsx'));
  assert.equal((await fetch(base + '/api/v1/transactions/export?format=xlsx')).status, 401);
  assert.deepEqual(errors, []);
  console.log('PASS export: desktop/mobile XLSX and CSV, 45 rows, filters, empty, viewer, 413, retry, duplicate prevention, read-only balances');
} catch (error) {
  await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true }).catch(() => {});
  throw error;
} finally {
  if (api) for (const id of created) await api('DELETE', '/transactions/' + id).catch(() => {});
  await browser.close();
}
