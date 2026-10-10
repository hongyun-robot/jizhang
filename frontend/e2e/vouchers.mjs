import { firefox } from 'playwright-core';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const base = process.env.VOUCHER_E2E_BASE ?? 'http://127.0.0.1:5601';
assert(
  ['127.0.0.1', 'localhost'].includes(new URL(base).hostname),
  'browser test must use the isolated local fixture',
);
const executablePath =
  process.env.FIREFOX_EXE ??
  path.join(
    process.env.LOCALAPPDATA,
    'ms-playwright',
    'firefox-1543',
    'firefox',
    'firefox.exe',
  );
const shots = path.resolve('e2e/shots');
const downloads = path.resolve('e2e/downloads');
fs.mkdirSync(shots, { recursive: true });
fs.mkdirSync(downloads, { recursive: true });
const browser = await firefox.launch({ executablePath, headless: true });
const errors = [];
const today = new Intl.DateTimeFormat('sv-SE', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
}).format(new Date());
const relativeDay = (n) => {
  const d = new Date(`${today}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
};
const wait = async (fn, label) => {
  for (let i = 0; i < 50; i++) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(`Timed out: ${label}`);
};
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
  acceptDownloads: true,
});
const page = await context.newPage();
let activePage = page;
let authorization = '';
page.on('request', (r) => {
  if (r.url().startsWith(base + '/api/v1/'))
    authorization = r.headers().authorization ?? authorization;
});
page.on('console', (m) => {
  if (m.type() === 'error' && !m.text().includes('401')) errors.push(m.text());
});
page.on('pageerror', (e) => errors.push(e.message));
const api = async (method, endpoint, input) => {
  const response = await context.request.fetch(base + '/api/v1' + endpoint, {
    method,
    headers: { Authorization: authorization },
    data: input,
  });
  const body = await response.json();
  assert.equal(response.status(), 200, JSON.stringify(body));
  assert.equal(body.code, 0);
  return body.data;
};
const login = async (p, user) => {
  await p.goto(base + '/login');
  await p.getByPlaceholder('用户名').fill(user);
  await p.getByPlaceholder('密码').fill('voucher-test-only');
  await p.getByRole('button', { name: /登\s*录/ }).click();
  await p.waitForURL(base + '/');
  await p.locator('.page-head').waitFor();
};
const dialog = (p, name) => p.getByRole('dialog', { name, exact: true });
const buttonName = (name) => new RegExp(`^${[...name].join('\\s*')}$`);
const setDate = async (locator, value) => {
  await locator.fill(value);
  await locator.press('Enter');
  await locator.press('Tab');
};
const choose = async (p, modal, label, value) => {
  if (label === '消费分类') {
    await modal.locator('.cat-leaves').getByRole('checkbox', { name: value, exact: true }).click();
    return;
  }
  await modal.getByLabel(label, { exact: true }).click();
  await p
    .locator('.ant-select-dropdown:visible .ant-select-item-option-content')
    .filter({ hasText: value })
    .first()
    .click();
};
const buy = async (p, name, amount, units, fromHome = false) => {
  if (fromHome) {
    await p.getByRole('button', { name: '快捷记账' }).click();
    await p.getByRole('button', { name: '购券：先付款，使用时计消费' }).click();
  } else {
    await p
      .locator('.page-head')
      .getByRole('button', { name: '登记购券', exact: true })
      .click();
  }
  const modal = dialog(p, '登记购券');
  await modal.getByLabel('券名称', { exact: true }).fill(name);
  await modal.getByLabel('实付金额', { exact: true }).fill(amount);
  await modal.getByLabel('可用次数', { exact: true }).fill(String(units));
  await choose(p, modal, '支付账户', '测试现金');
  await choose(p, modal, '消费分类', '测试餐饮');
  await setDate(
    modal.getByLabel('有效期至（可选）', { exact: true }),
    relativeDay(30),
  );
  assert.equal(
    await modal.getByRole('switch').getAttribute('aria-checked'),
    'true',
  );
  await modal.getByRole('button', { name: '登记购券', exact: true }).click();
  await modal.waitFor({ state: 'hidden' });
};
const openVoucher = async (p, name) => {
  await p.locator('.voucher-row').filter({ hasText: name }).click();
  await p.locator('.voucher-detail').waitFor();
};
const act = async (p, name, units) => {
  await p
    .locator('.voucher-actions')
    .getByRole('button', { name: buttonName(name) })
    .click();
  const modal = dialog(p, name);
  if (units !== undefined)
    await modal.getByRole('spinbutton').fill(String(units));
  await modal.getByRole('button', { name: '确认记录' }).click();
  await modal.waitFor({ state: 'hidden' });
};
const confirm = async (p) => {
  await p
    .locator('.ant-popover:visible')
    .getByRole('button', { name: /^(确\s*定|OK)$/ })
    .click();
};
const undoLast = async (p) => {
  await p
    .locator('.voucher-events')
    .getByRole('button', { name: buttonName('撤销') })
    .click();
  await confirm(p);
};
const basicName = `浏览器-餐券-${Date.now()}`;
const mobileName = `浏览器-多次券-${Date.now()}`;
try {
  const probe = await context.request.get(base + '/api/v1/__test__/info');
  assert.equal((await probe.json()).fixture, 'voucher-browser');
  await login(page, 'voucher-admin');
  await wait(() => !!authorization, 'authorization');
  const accounts = await api('GET', '/accounts');
  const account = accounts[0];
  const startBalance = Number(account.balance);
  const startExpense = Number(
    (await api('GET', '/stats/compare')).current.expense,
  );
  await buy(page, basicName, '100', 5, true);
  assert.equal(
    Number((await api('GET', '/accounts'))[0].balance),
    startBalance - 100,
  );
  assert.equal(
    Number((await api('GET', '/stats/compare')).current.expense),
    startExpense,
  );
  await page.goto(base + '/mine');
  await page.getByRole('button', { name: '我的券', exact: true }).click();
  await openVoucher(page, basicName);
  await act(page, '核销', 2);
  assert.equal(
    Number((await api('GET', '/accounts'))[0].balance),
    startBalance - 100,
  );
  assert.equal(
    Number((await api('GET', '/stats/compare')).current.expense),
    startExpense + 40,
  );
  await act(page, '退未用部分');
  assert.equal(
    Number((await api('GET', '/accounts'))[0].balance),
    startBalance - 40,
  );
  await page.screenshot({
    path: path.join(shots, 'voucher-desktop.png'),
    fullPage: true,
  });
  await undoLast(page);
  await wait(
    async () =>
      Number((await api('GET', '/accounts'))[0].balance) === startBalance - 100,
    'refund undo',
  );
  await act(page, '作废');
  assert.equal(
    Number((await api('GET', '/stats/compare')).current.expense),
    startExpense + 100,
  );
  await undoLast(page);
  await wait(
    async () =>
      Number((await api('GET', '/stats/compare')).current.expense) ===
      startExpense + 40,
    'writeoff undo',
  );
  await undoLast(page);
  await wait(
    async () =>
      Number((await api('GET', '/stats/compare')).current.expense) ===
      startExpense,
    'redemption undo',
  );
  await page
    .locator('.voucher-actions')
    .getByRole('button', { name: '删除券', exact: true })
    .click();
  await confirm(page);
  await wait(
    async () =>
      Number((await api('GET', '/accounts'))[0].balance) === startBalance,
    'purchase undo',
  );

  const auto = await api('POST', '/vouchers', {
    name: '浏览器-到期自动退款',
    paid_amount: '20',
    purchase_date: relativeDay(-2),
    account_id: account.id,
    category_id: 1,
    total_units: 2,
    expires_on: relativeDay(-1),
    request_key: crypto.randomUUID(),
  });
  await api('POST', '/vouchers/ensure');
  await page.goto(base + `/vouchers?id=${auto.id}`);
  await page.locator('.voucher-events').getByText('到期自动登记').waitFor();
  await undoLast(page);
  await wait(
    async () =>
      (await api('GET', `/vouchers/${auto.id}`)).auto_refund_suspended,
    'automatic refund paused',
  );
  await page.reload();
  await page
    .locator('.voucher-detail')
    .getByRole('alert')
    .getByText('自动退款已暂停', { exact: true })
    .waitFor();
  assert.equal((await api('GET', `/vouchers/${auto.id}`)).remaining_units, 2);
  await page.screenshot({
    path: path.join(shots, 'voucher-auto-paused.png'),
    fullPage: true,
  });

  const mobileContext = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  const mobile = await mobileContext.newPage();
  activePage = mobile;
  await login(mobile, 'voucher-admin');
  await mobile.goto(base + '/vouchers');
  await buy(mobile, mobileName, '10', 3);
  await openVoucher(mobile, mobileName);
  await act(mobile, '核销', 1);
  await wait(async () => {
    const rows = await api('GET', '/vouchers');
    return rows.items.find((v) => v.name === mobileName)?.used_units === 1;
  }, 'mobile redemption');
  let mv = (await api('GET', '/vouchers')).items.find(
    (v) => v.name === mobileName,
  );
  assert.equal(Number(mv.used_amount), 3.33);
  assert.equal(Number(mv.remaining_amount), 6.67);
  await mobile.screenshot({
    path: path.join(shots, 'voucher-mobile.png'),
    fullPage: true,
  });
  assert(
    await mobile.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
    'mobile horizontal overflow',
  );
  await act(mobile, '核销', 1);
  await wait(
    async () => (await api('GET', `/vouchers/${mv.id}`)).used_units === 2,
    'second redemption',
  );
  await act(mobile, '核销', 1);
  await wait(
    async () => (await api('GET', `/vouchers/${mv.id}`)).used_units === 3,
    'last redemption',
  );
  mv = await api('GET', `/vouchers/${mv.id}`);
  assert.equal(Number(mv.used_amount), 10);
  assert.equal(Number(mv.events.at(-1).amount), 3.34);

  await page.goto(base + '/records');
  const downloadPromise = page.waitForEvent('download');
  await page
    .locator('.page-head')
    .getByRole('button', { name: '导出', exact: true })
    .click();
  const download = await downloadPromise;
  const csvPath = path.join(downloads, 'vouchers.csv');
  await download.saveAs(csvPath);
  const csv = fs.readFileSync(csvPath, 'utf8');
  assert(csv.includes('余额影响,消费影响'));
  assert(csv.includes('券核销'));
  assert(csv.includes('3.34'));
  assert.equal(
    (await page.getByRole('button', { name: '查看券', exact: true }).count()) >
      0,
    true,
  );

  const readonlyContext = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  const readonly = await readonlyContext.newPage();
  let readonlyEnsure = 0;
  activePage = readonly;
  readonly.on('request', (r) => {
    if (r.url().includes('/vouchers/ensure')) readonlyEnsure++;
  });
  await login(readonly, 'voucher-viewer');
  await readonly.goto(base + '/mine');
  await readonly.getByRole('button', { name: '我的券', exact: true }).click();
  await readonly.locator('.voucher-list').waitFor();
  assert.equal(
    await readonly
      .getByRole('button', { name: '登记购券', exact: true })
      .count(),
    0,
  );
  await openVoucher(readonly, mobileName);
  assert.equal(await readonly.locator('.voucher-actions button').count(), 0);
  assert.equal(
    await readonly.getByRole('button', { name: buttonName('撤销') }).count(),
    0,
  );
  assert.equal(readonlyEnsure, 0);
  assert.deepEqual(errors, []);
  console.log(
    'PASS desktop/mobile: purchase, redemption, refund, writeoff, undo, automatic pause, CSV, readonly; no page errors',
  );
} catch (err) {
  await activePage.screenshot({
    path: path.join(shots, 'voucher-failure.png'),
    fullPage: true,
  });
  throw err;
} finally {
  await browser.close();
}
