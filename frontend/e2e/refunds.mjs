import { currentAuthToken, latestBearer, trackPageAuth } from './support/auth-tokens.mjs';
/* eslint-disable no-console */
/**
 * add-refund 任务 7.x：退款全流程浏览器走查（UI + 功能整套逻辑）。
 * 运行：node e2e/refunds.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景（tasks.md 7.1–7.8）：
 *  - 基线：API 造数（测试分类 + 100 元支出），跑完恢复基线（幂等可重跑）
 *  - 7.2 主流程：表单第 4 项「退款」→ 记 50 元独立退款 → 列表退款行、余额 +50、
 *    收入统计不变、首页结余与统计页同口径
 *  - 7.3 行内入口：支出行「退款」按钮预填关联/分类/账户/今天 → 保存 → 原支出
 *    显示「已退 ¥30」；关联收入被拒（API 400）
 *  - 7.4 部分退款：两笔（30+70）累计「已退 ¥100」；再记超额 20 → 黄条 + 二次确认提交
 *  - 7.5 账期：跨月退款计入到账月、原月净额不变；封账不阻止开放期新退款；
 *    修改/新增已封账期退款被拒（UI 提示 + API 断言）
 *  - 7.6 删除约束：UI 删带退款支出被拒 → 删光退款 → 删支出成功、余额恢复
 *  - 7.7 统计与预算：「分类仅退款」净额 -50（API 返回负值供图层裁剪）、趋势负支出、
 *    预算显示「本月已退回 ¥50.00」且无进度条（无负宽度）
 *  - 7.8 导出与权限：CSV 含「退款」标记且金额恒正；viewer 无退款入口且 API 403
 * 截图归档至 e2e/shots/refunds-*.png。
 */
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SHOTS = path.join(__dirname, 'shots');
const DL = path.join(__dirname, 'downloads');
fs.mkdirSync(SHOTS, { recursive: true });
fs.rmSync(DL, { recursive: true, force: true });
fs.mkdirSync(DL, { recursive: true });

const EXE =
  process.env.CHROME_EXE ??
  path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe');
const BASE = process.env.BASE ?? 'http://localhost:5600';
const API = process.env.API ?? 'http://127.0.0.1:8080/api/v1';

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });
const pad = (n) => String(n).padStart(2, '0');
const fmtDate = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const TODAY = fmtDate(new Date());
const NOW = new Date();
const CUR_YEAR = NOW.getFullYear();
const PK = TODAY.slice(0, 7); // 当前账期（settlement_day=1 → 等于自然月）
const MONTH_START = `${PK}-01`;
const MONTH_END = `${PK}-${pad(new Date(CUR_YEAR, NOW.getMonth() + 1, 0).getDate())}`;
const AUG = `${CUR_YEAR}-08`; // 封账测试用的历史账期
const NOTE = '退款测试';

async function apiLogin(u, p) {
  const _uiToken = currentAuthToken(BASE, u);
  if (_uiToken) { return _uiToken; }
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ protocol_version: 2, username: u, password: p }),
  });
  const j = await r.json();
  if (j.code !== 0) throw new Error(j.message);
  return j.data.access_token;
}

async function api(method, p, token, body) {
  const r = await fetch(API + p, {
    method,
    headers: { 'content-type': 'application/json', authorization: `Bearer ${latestBearer(BASE, token)}` },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, body: j };
}

async function login(page, username, password) {
  trackPageAuth(page, BASE);
  await page.goto(BASE + '/login');
  await page.locator('input[placeholder="用户名"]').fill(username);
  await page.locator('input[placeholder="密码"]').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.page-head').first().waitFor({ timeout: 15000 });
}

async function expectMsg(page, re, timeout = 6000) {
  try {
    await page.locator('.ant-message', { hasText: re }).first().waitFor({ timeout });
    return true;
  } catch {
    return false;
  }
}

/** dev 环境 SQL（与 budgets.mjs 同一通道）。
 * 优先 WSL docker（历史环境）；WSL/容器不可用时回退本机 MySQL 8.4（3307）。 */
const LOCAL_MYSQL = 'C:\\Program Files\\MySQL\\MySQL Server 8.4\\bin\\mysql.exe';
function sql(q) {
  const cols = `--default-character-set=utf8mb4 jizhang --skip-column-names -e "${q}"`;
  try {
    return execSync(
      `wsl -d Ubuntu-20.04 -u root -- docker exec jizhang-mysql mysql -uroot -pdevpass ${cols}`,
      { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], timeout: 8000 },
    ).trim();
  } catch {
    return execSync(
      `"${LOCAL_MYSQL}" --host=127.0.0.1 --port=3307 --user=root --password=devpass ${cols}`,
      { encoding: 'utf8' },
    ).trim();
  }
}

async function gotoRecords(page) {
  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
}

const rowOf = (page, note) => page.locator('tr.ant-table-row', { hasText: note });

/** VoucherFormModal forceRender 会常驻一个隐藏容器，用 :visible 限定到打开的弹窗。 */
const txnModal = (page) => page.locator('.ant-modal-container:visible');

async function openModal(page) {
  await page.locator('.page-head').getByRole('button', { name: '记一笔' }).first().click();
  const modal = txnModal(page);
  await modal.waitFor({ state: 'visible', timeout: 10000 });
  return modal;
}

async function pickAccount(modal, page, acctName) {
  const sel = modal.locator('.ant-select', { hasText: '账户' }).first();
  await sel.click();
  const opt = page
    .locator('.ant-select-dropdown:visible .ant-select-item-option-content', { hasText: acctName })
    .first();
  await opt.waitFor({ state: 'visible', timeout: 5000 });
  await opt.click();
}

async function submitModal(page) {
  await txnModal(page).locator('.ant-modal-footer .ant-btn-primary').first().click();
}

async function waitModalClosed(page) {
  await txnModal(page)
    .waitFor({ state: 'hidden', timeout: 10000 })
    .catch(() => {});
}

/** 通过行内「退款」入口打开预填好的退款表单。 */
async function openRowRefund(page, note) {
  const row = rowOf(page, note);
  await row.waitFor({ state: 'visible', timeout: 10000 });
  await row.getByRole('button', { name: /^退\s*款$/ }).first().click();
  const modal = txnModal(page);
  await modal.waitFor({ state: 'visible', timeout: 10000 });
  return modal;
}

/** 列表行删除（走 Popconfirm）。 */
async function uiDelete(page, note) {
  const row = rowOf(page, note);
  await row.getByRole('button', { name: /删\s*除/ }).first().click();
  await page.locator('.ant-popconfirm .ant-btn-primary, .ant-modal-confirm-btns .ant-btn-primary').first().click();
}

function parseCsv(file) {
  const bytes = fs.readFileSync(file);
  const bom = bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  const text = bytes.toString('utf8').replace(/^﻿/, '');
  const lines = text.split('\r\n').filter((l) => l !== '');
  return { bom, header: lines[0] ?? '', dataLines: lines.slice(1), text };
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ acceptDownloads: true, viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();

  const tok = await apiLogin('admin', 'admin123');
  await api('PUT', '/settings', tok, { settlement_day: '1' });

  // ================= 0. 基线清理与造数（幂等，支持重跑） =================
  // 账期断言假设历史 8 月未封（dev 基线）；若上次异常残留则放开
  sql(`DELETE FROM period_closings WHERE period_key='${AUG}'`);
  // 先删仍挂在测试支出/测试分类上的退款（含无备注的孤儿残留），再删测试流水
  sql(
    `DELETE FROM transactions WHERE kind='refund' AND related_txn_id IN ` +
      `(SELECT id FROM (SELECT id FROM transactions WHERE note LIKE '%${NOTE}%') AS x)`,
  );
  sql(
    `DELETE FROM transactions WHERE kind='refund' AND category_id IN ` +
      `(SELECT id FROM (SELECT id FROM categories WHERE name LIKE '${NOTE}%') AS y)`,
  );
  sql(`DELETE FROM transactions WHERE note LIKE '%${NOTE}%'`);
  await api('PUT', `/budgets/${PK}`, tok, {});
  sql(`DELETE FROM categories WHERE name='${NOTE}' OR name='${NOTE}2'`);
  sql(`DELETE FROM notifications WHERE kind='budget_overdue' AND payload LIKE '%${NOTE}%'`);

  const accs = (await api('GET', '/accounts', tok)).body.data ?? [];
  const acct = accs.find((a) => a.status === 'active' && a.type === 'asset') ?? accs[0];
  ok(!!acct, '基线：取得可用资产账户', acct ? `${acct.id} ${acct.name}` : 'none');
  const B0 = Number(acct.balance);

  const catRes = await api('POST', '/categories', tok, { kind: 'expense', name: NOTE });
  ok(catRes.status === 200 && catRes.body.code === 0, '基线：创建测试分类', `id=${catRes.body.data?.id}`);
  const CAT = catRes.body.data.id;

  const cat2Res = await api('POST', '/categories', tok, { kind: 'expense', name: `${NOTE}2` });
  const CAT2 = cat2Res.body.data.id;

  const balNow = async () => {
    const a = (await api('GET', '/accounts', tok)).body.data.find((x) => x.id === acct.id);
    return Number(a.balance);
  };
  const statsCat = async (start = MONTH_START, end = MONTH_END) =>
    (await api('GET', `/stats/category?start_date=${start}&end_date=${end}`, tok)).body.data;
  const periods = async () => {
    const rows = (await api('GET', `/stats/periods?year=${CUR_YEAR}`, tok)).body.data.periods;
    const m = {};
    for (const p of rows) m[p.period_key] = Number(p.expense);
    return m;
  };

  const s0 = await statsCat();
  const s0Income = Number(s0.income.total);

  // 原支出 100（当前账期、备注标记，供行内入口与删除约束用）
  const tx0Res = await api('POST', '/transactions', tok, {
    kind: 'expense',
    amount: '100.00',
    account_id: acct.id,
    category_id: CAT,
    entry_date: TODAY,
    note: `${NOTE}-原支出`,
  });
  ok(tx0Res.body.code === 0, '基线：创建 100 元原支出', `id=${tx0Res.body.data?.id}`);
  const TX0 = tx0Res.body.data.id;

  // 原支出入账后的统计（+100），与后续退款后的净额对照
  const s1 = await statsCat();
  ok(
    Math.abs(Number(s1.expense.total) - (Number(s0.expense.total) + 100)) < 0.001,
    '支出统计 = 基线 + 100（原支出）',
    s1.expense.total,
  );

  await login(page, 'admin', 'admin123');
  await gotoRecords(page);
  ok((await rowOf(page, `${NOTE}-原支出`).count()) > 0, '流水页可见原支出行');
  await shot(page, 'refunds-01-baseline');

  // ================= 1. 7.2 记账主流程 =================
  const modal = await openModal(page);
  const kindCount = await modal.locator('.ant-segmented-item').count();
  ok(kindCount === 4, '表单类型 4 项（含退款）', `count=${kindCount}`);
  ok(
    (await modal.locator('.ant-segmented-item', { hasText: '退款' }).count()) > 0,
    '表单存在「退款」选项',
  );
  await shot(page, 'refunds-02-form-kinds');

  await modal.locator('.ant-segmented-item', { hasText: '退款' }).first().click();
  const dateVal = await modal.locator('.ant-picker input').first().inputValue();
  ok(dateVal === TODAY, '退款日期默认今天', dateVal);
  await modal.locator('input.amount-input').fill('50');
  await pickAccount(modal, page, acct.name);
  await modal.getByPlaceholder('备注（可选）').fill(`${NOTE}-独立`);
  await shot(page, 'refunds-03-form-refund');
  await submitModal(page);
  ok(await expectMsg(page, /已记一笔/), '独立退款保存成功提示');
  await waitModalClosed(page);

  const standaloneRow = rowOf(page, `${NOTE}-独立`);
  await standaloneRow.waitFor({ state: 'visible', timeout: 8000 });
  const srowText = await standaloneRow.innerText();
  ok(srowText.includes('退款'), '列表出现退款行（类型标签=退款）', srowText.replace(/\n/g, ' '));
  ok(srowText.includes('¥50.00') && !srowText.includes('-¥50.00'), '退款金额显示为正 ¥50.00');

  const balAfterRefund = await balNow();
  ok(
    Math.abs(balAfterRefund - (B0 - 100 + 50)) < 0.001,
    '账户余额：原支出 -100 后退款 +50',
    `base=${B0} now=${balAfterRefund}`,
  );

  // 退款入账后的统计（净额口径）
  const s2 = await statsCat();
  ok(
    Math.abs(Number(s2.expense.total) - (Number(s1.expense.total) - 50)) < 0.001,
    '支出统计扣除退款 50（净额口径）',
    s2.expense.total,
  );
  ok(Math.abs(Number(s2.income.total) - s0Income) < 0.001, '收入统计不变（退款不计收入）', s2.income.total);

  // 首页结余与统计页同口径（同 /stats/compare 数据源）
  const cmp = (await api('GET', '/stats/compare', tok)).body.data;
  const fmt2 = (v) =>
    Number(v).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  const heroText = await page.locator('.hero-split').innerText();
  const heroIncome = /\d[\d,]*\.\d{2}/.exec(heroText)?.[0] ?? '';
  const heroNums = heroText.match(/-?[\d,]+\.\d{2}/g) ?? [];
  await shot(page, 'refunds-04-home-hero');
  await page.goto(BASE + '/stats');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(600);
  const cmpText = await page.locator('.cmp-values').innerText();
  const mCmp = /本期\s*([\d,.-]+)\/([\d,.-]+)/.exec(cmpText);
  ok(!!mCmp, '统计页显示本期收支', cmpText);
  ok(
    mCmp && mCmp[1] === fmt2(cmp.current.income) && mCmp[2] === fmt2(cmp.current.expense),
    '统计页收支 = API /stats/compare 口径',
    mCmp ? mCmp.slice(1).join(' / ') : '',
  );
  ok(
    heroNums.length >= 2 && heroNums[heroNums.length - 2] === fmt2(cmp.current.income) &&
      heroNums[heroNums.length - 1] === fmt2(cmp.current.expense),
    '首页结余拆解与统计页同口径',
    heroNums.join(' | '),
  );
  await shot(page, 'refunds-05-stats');

  // ================= 2. 7.3 行内入口与关联断言 =================
  await gotoRecords(page);
  const rModal = await openRowRefund(page, `${NOTE}-原支出`);
  ok(
    (await rModal.locator('.ant-segmented-item-selected', { hasText: '退款' }).count()) > 0,
    '行内入口预填类型=退款',
  );
  ok(
    (await rModal.locator('input.amount-input').inputValue()) === '',
    '金额留空待填',
  );
  const rDate = await rModal.locator('.ant-picker input').first().inputValue();
  ok(rDate === TODAY, '日期默认今天', rDate);
  const related = await rModal.locator('.refund-related').innerText();
  ok(
    related.includes(`#${TX0}`) && /原 ¥100(\.00)?/.test(related) && /已退 ¥0(\.00)?/.test(related),
    '显示关联原支出与金额',
    related.replace(/\n/g, ' '),
  );
  const checkedRoot = await rModal.locator('.cat-roots .ant-tag-checkable-checked').innerText();
  ok(checkedRoot.includes(NOTE), '分类预填原支出分类', checkedRoot);
  const selText = await rModal
    .locator('.ant-select-content-has-value, .ant-select-selection-item')
    .first()
    .innerText()
    .catch(() => '');
  const selTitle = await rModal
    .locator('.ant-select-content-has-value, .ant-select-selection-item')
    .first()
    .getAttribute('title')
    .catch(() => '');
  ok(
    selText.includes(acct.name) || (selTitle ?? '').includes(acct.name),
    '账户预填原支出账户',
    selText || selTitle || 'none',
  );
  await shot(page, 'refunds-06-row-action-prefill');

  await rModal.locator('input.amount-input').fill('30');
  await rModal.getByPlaceholder('备注（可选）').fill(`${NOTE}-部分1`);
  await submitModal(page);
  ok(await expectMsg(page, /已记一笔/), '关联退款保存成功');
  await waitModalClosed(page);

  await sleep(800);
  const tx0Row = rowOf(page, `${NOTE}-原支出`);
  const tag1 = await tx0Row.locator('.refunded-tag').innerText();
  ok(tag1.includes('已退') && tag1.includes('¥30'), '原支出显示「已退 ¥30」', tag1);

  // 定位关联退款（点已退标签 → 只列关联退款）
  await tx0Row.locator('.refunded-tag').click();
  await sleep(800);
  const chip = page.locator('.filter-bar .ant-tag', { hasText: '关联退款' });
  ok((await chip.count()) > 0, '出现「关联退款」筛选标记');
  const rowsAfterLocate = page.locator('tr.ant-table-row');
  const nRows = await rowsAfterLocate.count();
  let allRefund = nRows > 0;
  for (let i = 0; i < nRows; i++) {
    const t = await rowsAfterLocate.nth(i).innerText();
    if (!t.includes('退款')) allRefund = false;
  }
  ok(allRefund, '定位视图只列关联退款', `rows=${nRows}`);
  await shot(page, 'refunds-07-locate-refunds');
  await chip.locator('.anticon-close').click();
  await sleep(500);

  // 关联收入被拒（API 400）
  const incomes = (await api('GET', '/transactions?kind=income&page_size=5', tok)).body.data.items;
  let incomeId = incomes[0]?.id;
  if (!incomeId) {
    const icats = (await api('GET', '/categories', tok)).body.data;
    const iroot = (icats[0]?.children ?? []).find((c) => c.kind === 'income');
    const ileaf = iroot?.children?.[0]?.id ?? iroot?.id;
    const inc = await api('POST', '/transactions', tok, {
      kind: 'income',
      amount: '1.00',
      account_id: acct.id,
      category_id: ileaf,
      entry_date: TODAY,
      note: `${NOTE}-收入`,
    });
    incomeId = inc.body.data?.id;
  }
  const rejIncome = await api('POST', '/transactions', tok, {
    kind: 'refund',
    amount: '5.00',
    account_id: acct.id,
    related_txn_id: incomeId,
    entry_date: TODAY,
    note: `${NOTE}-非法关联`,
  });
  ok(
    rejIncome.status === 400 && /关联/.test(rejIncome.body.message ?? ''),
    '关联收入的退款被拒（400）',
    `status=${rejIncome.status} msg=${rejIncome.body.message}`,
  );

  // ================= 3. 7.4 部分退款与超额提示 =================
  const m2 = await openRowRefund(page, `${NOTE}-原支出`);
  await m2.locator('input.amount-input').fill('70');
  await m2.getByPlaceholder('备注（可选）').fill(`${NOTE}-部分2`);
  await submitModal(page);
  ok(await expectMsg(page, /已记一笔/), '第二笔部分退款 70 保存');
  await waitModalClosed(page);
  await sleep(800);
  const tag2 = await rowOf(page, `${NOTE}-原支出`).locator('.refunded-tag').innerText();
  ok(tag2.includes('已退') && tag2.includes('¥100'), '累计「已退 ¥100」（30+70）', tag2);

  const m3 = await openRowRefund(page, `${NOTE}-原支出`);
  await m3.locator('input.amount-input').fill('20');
  await m3.getByPlaceholder('备注（可选）').fill(`${NOTE}-超额`);
  await sleep(300);
  const overAlert = m3.locator('.refund-over');
  ok((await overAlert.count()) > 0, '超额时出现黄条提示');
  const overText = (await overAlert.innerText().catch(() => '')).replace(/\n/g, ' ');
  ok(overText.includes('超过原支出') && overText.includes('120'), '黄条含累计与原支出金额', overText);
  await shot(page, 'refunds-08-over-alert');
  await submitModal(page);
  ok(await expectMsg(page, /超过原支出/), '首次提交被黄条拦下（二次确认提示）');
  ok(
    (await page.locator('.ant-modal-container .refund-over').count()) > 0,
    '弹窗未关闭等待二次确认',
  );
  await submitModal(page);
  ok(await expectMsg(page, /已记一笔/), '二次确认后提交成功');
  await waitModalClosed(page);
  await sleep(800);
  const tag3 = await rowOf(page, `${NOTE}-原支出`).locator('.refunded-tag').innerText();
  ok(tag3.includes('¥120'), '已退累计 ¥120（超额后）', tag3);
  const listNow = (await api('GET', `/transactions?keyword=${encodeURIComponent(`${NOTE}-原支出`)}`, tok)).body.data;
  const expRow = (listNow.items ?? []).find((t) => t.id === TX0);
  ok(
    Number(expRow?.refunded_amount) === 120,
    'API 聚合 refunded_amount=120',
    String(expRow?.refunded_amount),
  );

  // ================= 4. 7.6 删除约束 =================
  await uiDelete(page, `${NOTE}-原支出`);
  ok(await expectMsg(page, /存在关联退款/), 'UI 删除带退款的支出被拒并提示');
  await sleep(600);
  ok((await rowOf(page, `${NOTE}-原支出`).count()) > 0, '原支出仍在列表');

  // 删光关联/独立退款（API，含超额笔）
  const allTest = [];
  for (let p = 1; p <= 5; p++) {
    const r = await api('GET', `/transactions?keyword=${encodeURIComponent(NOTE)}&page=${p}&page_size=100`, tok);
    allTest.push(...(r.body.data?.items ?? []));
    if ((r.body.data?.items ?? []).length < 100) break;
  }
  const refunds = allTest.filter((t) => t.kind === 'refund');
  ok(refunds.length === 4, '待删退款 4 笔（独立 + 30/70/20）', `count=${refunds.length}`);
  let delOk = true;
  for (const r of refunds) {
    const d = await api('DELETE', `/transactions/${r.id}`, tok);
    if (d.body.code !== 0) delOk = false;
  }
  ok(delOk, '退款全部删除');

  await uiDelete(page, `${NOTE}-原支出`);
  ok(await expectMsg(page, /已删除/), '删光退款后原支出删除成功');
  await waitModalClosed(page);
  await sleep(700);
  ok((await rowOf(page, `${NOTE}-原支出`).count()) === 0, '原支出已从列表移除');
  const balRestored = await balNow();
  ok(
    Math.abs(balRestored - B0) < 0.001,
    '余额恢复基线',
    `base=${B0} now=${balRestored}`,
  );
  await shot(page, 'refunds-09-delete-constraint');

  // ================= 5. 7.5 账期与封账 =================
  const P0 = await periods();

  const tx1Res = await api('POST', '/transactions', tok, {
    kind: 'expense',
    amount: '100.00',
    account_id: acct.id,
    category_id: CAT2,
    entry_date: `${AUG}-15`,
    note: `${NOTE}-8月支出`,
  });
  const TX1 = tx1Res.body.data?.id;
  ok(tx1Res.body.code === 0, '创建 8 月原支出', `id=${TX1}`);
  const augRefundRes = await api('POST', '/transactions', tok, {
    kind: 'refund',
    amount: '60.00',
    account_id: acct.id,
    related_txn_id: TX1,
    entry_date: `${AUG}-16`,
    note: `${NOTE}-8月退款`,
  });
  const AUG_R = augRefundRes.body.data?.id;
  ok(augRefundRes.body.code === 0, '8 月内创建关联退款 60', `id=${AUG_R}`);

  const closeRes = await api('POST', '/closings', tok, { period_key: AUG });
  ok(closeRes.body.code === 0, `封账 ${AUG}`, closeRes.body.message);

  // 封账后：开放期（当前账期）新退款成功，且落在到账月
  const crossRes = await api('POST', '/transactions', tok, {
    kind: 'refund',
    amount: '40.00',
    account_id: acct.id,
    related_txn_id: TX1,
    entry_date: TODAY,
    note: `${NOTE}-跨月退款`,
  });
  ok(crossRes.body.code === 0, '原支出封账不阻止开放期新退款', crossRes.body.message);
  ok(crossRes.body.data?.period_key === PK, '跨月退款归到账月', `period=${crossRes.body.data?.period_key}`);

  // 修改已封账期退款被拒（API）
  const upd = await api('PUT', `/transactions/${AUG_R}`, tok, {
    kind: 'refund',
    amount: '65.00',
    account_id: acct.id,
    related_txn_id: TX1,
    entry_date: `${AUG}-16`,
    note: `${NOTE}-8月退款`,
  });
  ok(
    upd.status >= 400 && /封账/.test(upd.body.message ?? ''),
    '修改已封账期退款被拒',
    `status=${upd.status} msg=${upd.body.message}`,
  );
  // 封账期内新增也被拒（API）
  const addClosed = await api('POST', '/transactions', tok, {
    kind: 'refund',
    amount: '10.00',
    account_id: acct.id,
    related_txn_id: TX1,
    entry_date: `${AUG}-20`,
    note: `${NOTE}-封账新增`,
  });
  ok(addClosed.status >= 400, '已封账期内新增退款被拒', `status=${addClosed.status}`);

  // 账期净额：8 月 = +40（100 支出 - 60 退款）；当前月 = -40（跨月退款）
  const P1 = await periods();
  const augDelta = (P1[AUG] ?? 0) - (P0[AUG] ?? 0);
  const curDelta = (P1[PK] ?? 0) - (P0[PK] ?? 0);
  ok(Math.abs(augDelta - 40) < 0.001, '8 月净额 +40（本期内退款冲减，不受跨月影响）', `delta=${augDelta}`);
  ok(Math.abs(curDelta + 40) < 0.001, '当前月净额 -40（跨月退款计入到账月）', `delta=${curDelta}`);

  // UI：编辑已封账期退款被拒（自定义区间定位 8 月 → 已退标签 → 关联退款 → 编辑）
  await gotoRecords(page);
  await page.locator('.period-picker .ant-segmented-item', { hasText: '自定义' }).click();
  await sleep(300);
  const rp = page.locator('.period-picker .ant-picker');
  const i0 = rp.locator('input').nth(0);
  const i1 = rp.locator('input').nth(1);
  await i0.click();
  await i0.fill(`${AUG}-01`);
  await page.keyboard.press('Enter');
  await i1.fill(`${AUG}-31`);
  await page.keyboard.press('Enter');
  await sleep(900);
  const tx1Row = rowOf(page, `${NOTE}-8月支出`);
  ok((await tx1Row.count()) > 0, '自定义区间定位到 8 月原支出');
  const augTag = await tx1Row.locator('.refunded-tag').innerText();
  ok(augTag.includes('已退'), '8 月原支出带已退标记', augTag);
  await tx1Row.locator('.refunded-tag').click();
  await sleep(800);
  const augRefRow = rowOf(page, `${AUG}-16`);
  ok((await augRefRow.count()) > 0, '关联退款定位到 8 月退款行');
  await augRefRow.getByRole('button', { name: /编\s*辑/ }).click();
  const eModal = txnModal(page);
  await eModal.waitFor({ state: 'visible', timeout: 8000 });
  await eModal.locator('input.amount-input').fill('65');
  await submitModal(page);
  ok(await expectMsg(page, /封账/), 'UI 编辑已封账期退款提示被拒');
  await eModal.getByRole('button', { name: /取\s*消/ }).click();
  await waitModalClosed(page);
  await shot(page, 'refunds-10-closed-period');
  // 恢复筛选：本月 + 清除关联标记
  const chip2 = page.locator('.filter-bar .ant-tag', { hasText: '关联退款' });
  if ((await chip2.count()) > 0) await chip2.locator('.anticon-close').click();
  await page.locator('.period-picker .ant-segmented-item', { hasText: '本月' }).click();
  await sleep(600);

  // ================= 6. 7.7 统计与预算展示 =================
  // 选一个当月无任何流水的日期，保证趋势该日净额就是 -50
  const busy = new Set(
    sql(`SELECT DATE(entry_date) FROM transactions WHERE period_key='${PK}'`)
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean),
  );
  const lastDay = new Date(CUR_YEAR, NOW.getMonth() + 1, 0).getDate();
  let FREE = '';
  for (let d = lastDay; d >= 1; d--) {
    const cand = `${PK}-${pad(d)}`;
    if (!busy.has(cand)) {
      FREE = cand;
      break;
    }
  }
  ok(!!FREE, '选定当月无流水日期用于净额断言', FREE);

  const statEx = await api('POST', '/transactions', tok, {
    kind: 'expense',
    amount: '100.00',
    account_id: acct.id,
    category_id: CAT,
    entry_date: FREE,
    note: `${NOTE}-统计支出`,
  });
  const statRf = await api('POST', '/transactions', tok, {
    kind: 'refund',
    amount: '150.00',
    account_id: acct.id,
    category_id: CAT,
    related_txn_id: statEx.body.data?.id,
    entry_date: FREE,
    note: `${NOTE}-统计退款`,
  });
  ok(statEx.body.code === 0 && statRf.body.code === 0, '构造「分类净额为负」数据（100 支出 / 150 退款）');

  const sCat = await statsCat();
  const negItem = (sCat.expense.items ?? []).find((i) => i.name === NOTE);
  ok(!!negItem, '分类聚合返回负净额条目（不过滤）', negItem ? `${negItem.name}=${negItem.amount}` : 'missing');
  ok(negItem && Number(negItem.amount) === -50, '净额 = -50（100-150）', negItem?.amount);

  const trend = (await api('GET', `/stats/trend?start_date=${MONTH_START}&end_date=${MONTH_END}&granularity=day`, tok))
    .body.data;
  const negPts = (trend.points ?? []).filter((p) => Number(p.expense) < 0);
  ok(negPts.length > 0, '趋势存在负支出日（退款冲减）', JSON.stringify(negPts[0] ?? {}));

  const P2 = await periods();
  ok(
    Math.abs(P2[PK] - P1[PK] + 50) < 0.001,
    '月度汇总净额 -50（100 支出 - 150 退款）',
    `now=${P2[PK]} was=${P1[PK]}`,
  );

  // 预算：配置该分类 300 → 净消耗 -50 → 「本月已退回 ¥50.00」、无进度条
  const setB = await api('PUT', `/budgets/${PK}`, tok, {
    items: [{ category_id: CAT, amount: '300.00' }],
  });
  ok(setB.body.code === 0, '配置分类预算 300');
  await page.goto(BASE + '/budgets');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(700);
  const bRow = page.locator(`.budget-row[data-budget="${CAT}"]`);
  ok((await bRow.count()) > 0, '预算页存在该分类行');
  const refundTxt = await bRow.locator('.budget-refund').innerText().catch(() => '');
  ok(
    refundTxt.includes('本月已退回') && refundTxt.includes('50.00'),
    '显示「本月已退回 ¥50.00」',
    refundTxt.replace(/\n/g, ' '),
  );
  ok(
    (await bRow.locator('.ant-progress').count()) === 0,
    '负消耗无进度条（不出现负宽度）',
  );
  await shot(page, 'refunds-11-budget-payback');

  await page.goto(BASE + '/stats');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(800);
  const statsBudget = page.locator('.ant-card', { hasText: '预算进度' }).first();
  const sbTxt = await statsBudget.innerText();
  ok(sbTxt.includes('本月已退回'), '统计页预算进度显示退回文案');
  await shot(page, 'refunds-12-stats-pie');

  // ================= 7. 7.8 导出与权限 =================
  await gotoRecords(page);
  await page.locator('.filter-bar .ant-segmented-item', { hasText: '退款' }).click();
  await sleep(900);
  const listRows = page.locator('tr.ant-table-row');
  const rn = await listRows.count();
  let kindOk = rn > 0;
  for (let i = 0; i < rn; i++) {
    const cells = await listRows.nth(i).innerText();
    if (!cells.includes('退款')) kindOk = false;
  }
  ok(kindOk, '筛选 kind=退款 列表全部为退款行', `rows=${rn}`);
  await shot(page, 'refunds-13-filter-refund');

  const [dl] = await Promise.all([
    page.waitForEvent('download', { timeout: 20000 }),
    page.getByRole('button', { name: /导\s*出/ }).click(),
  ]);
  const csvFile = path.join(DL, dl.suggestedFilename());
  await dl.saveAs(csvFile);
  const csv = parseCsv(csvFile);
  const refundLines = csv.dataLines.filter((l) => l.includes(',退款,'));
  ok(csv.bom, 'CSV 含 UTF-8 BOM');
  ok(refundLines.length === csv.dataLines.length && refundLines.length > 0,
    'CSV 每行都是退款标记', `${refundLines.length}/${csv.dataLines.length}`);
  ok(
    refundLines.some((l) => l.includes('150.00')) && refundLines.some((l) => l.includes('40.00')),
    'CSV 含退款金额 150.00 与 40.00',
  );
  // 只校验「金额」列恒正：「消费影响」列对退款是负数（后加列），整行匹配会误报
  const amountCellNeg = refundLines.some((l) => {
    const cells = l.split(',');
    return (cells[5] ?? '').trim().startsWith('-');
  });
  ok(!amountCellNeg, 'CSV 退款金额恒正（无负号）');

  // viewer：无退款入口 + API 403
  const vtok = await apiLogin('dazhi', 'member123456');
  const vPost = await api('POST', '/transactions', vtok, {
    kind: 'refund',
    amount: '5.00',
    account_id: acct.id,
    entry_date: TODAY,
    note: `${NOTE}-viewer`,
  });
  ok(vPost.status === 403, 'viewer 创建退款 403', `status=${vPost.status}`);

  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const p2 = await ctx2.newPage();
  await login(p2, 'dazhi', 'member123456');
  await gotoRecords(p2);
  ok((await p2.locator('.row-actions').count()) === 0, 'viewer 行内无操作（无退款入口）');
  const vAdd = p2.locator('.page-head').getByRole('button', { name: '记一笔' }).first();
  if ((await vAdd.count()) > 0) {
    await vAdd.click();
    const vModal = txnModal(p2);
    await vModal.waitFor({ state: 'visible', timeout: 8000 });
    const vKinds = await vModal.locator('.ant-segmented-item').count();
    const hasRefund = (await vModal.locator('.ant-segmented-item', { hasText: '退款' }).count()) > 0;
    ok(vKinds === 3 && !hasRefund, 'viewer 表单只有 3 类型、无退款项', `count=${vKinds}`);
    await shot(p2, 'refunds-14-viewer');
    await vModal.getByRole('button', { name: /取\s*消/ }).click();
  } else {
    ok(false, 'viewer 表单只有 3 类型、无退款项', '记一笔入口缺失');
  }
  await ctx2.close();

  // ================= 8. 恢复基线 =================
  sql(`DELETE FROM period_closings WHERE period_key='${AUG}'`);
  sql(
    `DELETE FROM transactions WHERE kind='refund' AND related_txn_id IN ` +
      `(SELECT id FROM (SELECT id FROM transactions WHERE note LIKE '%${NOTE}%') AS x)`,
  );
  sql(
    `DELETE FROM transactions WHERE kind='refund' AND category_id IN ` +
      `(SELECT id FROM (SELECT id FROM categories WHERE name LIKE '${NOTE}%') AS y)`,
  );
  sql(`DELETE FROM transactions WHERE note LIKE '%${NOTE}%'`);
  await api('PUT', `/budgets/${PK}`, tok, {}); // 预算先清（分类删除受 budgets 外键约束）
  sql(`DELETE FROM categories WHERE name='${NOTE}' OR name='${NOTE}2'`);
  sql(`DELETE FROM notifications WHERE kind='budget_overdue' AND payload LIKE '%${NOTE}%'`);

  const balFinal = await balNow();
  ok(Math.abs(balFinal - B0) < 0.001, '基线还原：余额回到初始值', `base=${B0} now=${balFinal}`);
  const left = sql(
    `SELECT COUNT(*) FROM transactions WHERE note LIKE '%${NOTE}%'`,
  );
  ok(left === '0', '基线还原：无测试流水残留', `left=${left}`);
  const leftCat = sql(`SELECT COUNT(*) FROM categories WHERE name LIKE '${NOTE}%'`);
  ok(leftCat === '0', '基线还原：无测试分类残留', `left=${leftCat}`);
  const bLeft = await api('GET', `/budgets/${PK}`, tok);
  ok(
    (bLeft.body.data?.categories ?? []).every((c) => Number(c.budget ?? 0) === 0) &&
      bLeft.body.data?.total?.budget == null,
    '基线还原：预算清空',
  );
  await shot(page, 'refunds-15-baseline-restored');

  await ctx.close();
  await browser.close();

  const pass = results.filter((r) => r.pass).length;
  console.log(`\n${pass}/${results.length} PASS`);
  if (pass !== results.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error('FATAL', e);
  process.exit(1);
});
