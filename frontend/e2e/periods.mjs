import { currentAuthToken, latestBearer, trackPageAuth } from './support/auth-tokens.mjs';
/* eslint-disable no-console */
/**
 * add-ledger-system 任务 7.4：specs/billing-periods 全场景浏览器走查。
 * 运行：node e2e/periods.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 未配置结算日 = 自然月（默认 1 日）
 *  - 结算日 26 日：本期 2026-09-26~2026-10-25、上期 2026-08-26~2026-09-25
 *  - 修改结算日提示「仅影响今日之后的流水」，不回溯历史
 *  - 封账确认弹窗展示该期收入/支出/结余；封后硬锁（UI 删除被拒）
 *  - 解封恢复可写 → 修改流水 → 重新封账
 *  - 审计日志：封/解封两步留痕（谁、何时、哪一期）
 *  - 非管理员：入口隐藏 + 路由拦截（双重执行）
 * 运行结束自动恢复基线（结算日=1、2026-09 开放、删除走查流水；审计行保留作证据）。
 */
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SHOTS = path.join(__dirname, 'shots');
fs.mkdirSync(SHOTS, { recursive: true });

const EXE =
  process.env.CHROME_EXE ??
  path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe');
const BASE = process.env.BASE ?? 'http://localhost:5600';
const API = process.env.API ?? 'http://127.0.0.1:8080/api/v1';
const NOTE = '账期走查';

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond, extra });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });

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

async function api(method, path_, token, body) {
  const r = await fetch(API + path_, {
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

/** 等待 antd message 出现匹配文本。 */
async function expectMsg(page, re, timeout = 6000) {
  try {
    await page.locator('.ant-message', { hasText: re }).first().waitFor({ timeout });
    return true;
  } catch {
    return false;
  }
}

/** 账期设置页 → 封账管理卡片。 */
function closingCard(page) {
  return page.locator('.ant-card', { hasText: '封账管理' }).first();
}

/** 选择账期月份（输入 YYYY-MM 回车；可选等待预览出现 waitText，否则回退面板点击）。 */
async function pickMonth(page, key, waitText) {
  const card = closingCard(page);
  const input = card.locator('.ant-picker input');
  await input.click();
  await input.fill(key);
  await input.press('Enter');
  if (waitText) {
    try {
      await card.getByText(waitText).first().waitFor({ timeout: 4000 });
      return;
    } catch {
      /* 输入未生效 → 面板回退 */
    }
  } else {
    await sleep(600);
    if ((await card.getByText(/账期区间/).innerText()).includes(key === '2026-09' ? '2026-09-01' : key)) {
      return;
    }
  }
  // 回退：打开面板点击月份格
  await input.click();
  const monthLabel = `${Number(key.slice(5))}月`;
  const cell = page
    .locator('.ant-picker-dropdown:not(.ant-picker-dropdown-hidden) .ant-picker-cell-in-view')
    .filter({ hasText: monthLabel });
  if ((await cell.count()) > 0) {
    await cell.first().click();
    await page.keyboard.press('Enter').catch(() => {});
  }
  await sleep(500);
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();

  // ================= 基线准备（API，幂等） =================
  const tok = await apiLogin('admin', 'admin123');
  await api('PUT', '/settings', tok, { settlement_day: 1 });
  await api('DELETE', '/closings/2026-09', tok); // 未封则 409，忽略
  const pre = await api(
    'GET',
    '/transactions?start_date=2026-09-01&end_date=2026-09-30&page=1&page_size=50',
    tok,
  );
  for (const t of pre.body?.data?.items ?? []) {
    if (t.note === NOTE) await api('DELETE', `/transactions/${t.id}`, tok);
  }
  await api('POST', '/transactions', tok, {
    kind: 'expense',
    amount: '88.00',
    account_id: 1,
    category_id: 24, // 购物/日用（末级分类；根分类不可直接记账）
    entry_date: '2026-09-15',
    note: NOTE,
  });

  // ================= 未配置 = 自然月 + 改日提示 =================
  await login(page, 'admin', 'admin123');
  await page.goto(BASE + '/settings');
  await page.getByText('账期设置').first().waitFor();
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(400);

  const dayInput = page
    .locator('.ant-card', { hasText: '结算日' })
    .first()
    .locator('.ant-input-number-input');
  ok((await dayInput.inputValue()) === '1', '未配置结算日默认 1（自然月）', await dayInput.inputValue());
  ok(
    (await page.getByText(/仅影响今日之后的流水/).count()) > 0,
    '改日提示「仅影响今日之后的流水」可见',
  );
  const natRange = await closingCard(page).getByText(/账期区间/).innerText();
  ok(natRange.includes('2026-09-01 ~ 2026-09-30'), '自然月口径：账期 2026-09 = 09-01~09-30', natRange);
  await shot(page, 'periods-01-settings-natural');

  // ================= 结算日 26 日（spec 本期/上期） =================
  await dayInput.fill('26');
  await page
    .locator('.ant-card', { hasText: '结算日' })
    .first()
    .getByRole('button', { name: /保\s*存/ })
    .click();
  ok(await expectMsg(page, /仅影响今日之后的流水/), '修改结算日成功提示（仅影响今日之后的流水）');
  await sleep(700);
  const s26 = await closingCard(page).getByText(/账期区间/).innerText();
  ok(
    s26.includes('2026-08-26 ~ 2026-09-25'),
    '结算日 26：上期 2026-08-26 ~ 2026-09-25（spec）',
    s26,
  );
  await shot(page, 'periods-02-settlement26-prev');

  await pickMonth(page, '2026-10', '2026-09-26');
  const cur = await closingCard(page).getByText(/账期区间/).innerText();
  ok(
    cur.includes('2026-09-26 ~ 2026-10-25'),
    '结算日 26：本期 2026-09-26 ~ 2026-10-25（spec）',
    cur,
  );
  await shot(page, 'periods-03-settlement26-current');

  // 恢复自然月（后续封账演示用）
  await dayInput.fill('1');
  await page
    .locator('.ant-card', { hasText: '结算日' })
    .first()
    .getByRole('button', { name: /保\s*存/ })
    .click();
  await expectMsg(page, /仅影响今日之后的流水/);
  await pickMonth(page, '2026-09');
  await sleep(700);
  ok(
    (await closingCard(page).getByText(/账期区间/).innerText()).includes('2026-09-01 ~ 2026-09-30'),
    '恢复结算日 1：账期区间回到自然月',
  );

  // ================= 封账确认（展示该期收支结余） =================
  const card = closingCard(page);
  ok(await card.getByText('已结束').first().isVisible(), '2026-09 状态为「已结束」');
  ok(
    (await card.getByText(/88\.00/).count()) > 0,
    '预览展示该期收支（支出 88.00）',
  );
  await card.getByRole('button', { name: /封\s*账/ }).click();
  const modal = page.locator('.ant-modal-confirm, .ant-modal-content').first();
  await modal.waitFor();
  const modalText = await modal.innerText();
  ok(
    modalText.includes('收入') && modalText.includes('支出') && modalText.includes('结余'),
    '封账确认弹窗展示收入/支出/结余',
  );
  ok(modalText.includes('88.00'), '确认弹窗展示该期金额 88.00', modalText.replace(/\n/g, ' | '));
  await shot(page, 'periods-04-close-confirm');
  await modal.getByRole('button', { name: /确\s*认\s*封\s*账/ }).click();
  ok(await expectMsg(page, /已封账/), '封账成功提示');
  await sleep(600);
  ok(await closingCard(page).getByText('已封账').first().isVisible(), '状态变为「已封账」');
  await shot(page, 'periods-05-closed');

  // ================= 封账期内修改流水被拒（UI） =================
  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.ant-segmented-item', { hasText: '上月' }).first().click();
  await sleep(700);
  const row = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE }).first();
  ok(await row.isVisible(), '流水列表可见「账期走查」（2026-09）');
  await row.getByRole('button', { name: /删\s*除/ }).click();
  const pop = page.locator('.ant-popconfirm').first();
  await pop.waitFor();
  await pop.locator('.ant-btn-primary').click();
  ok(await expectMsg(page, /已封账/), '封账期删除被拒：提示「该账期已封账」');
  await sleep(500);
  ok(
    (await page.locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE }).count()) > 0,
    '被拒后流水仍在（未删除）',
  );
  await shot(page, 'periods-06-write-rejected');

  // ================= 解封 → 修改 → 重新封账 =================
  await page.goto(BASE + '/settings');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  const card2 = closingCard(page);
  await card2.getByRole('button', { name: /解\s*封/ }).click();
  const pop2 = page.locator('.ant-popconfirm').first();
  await pop2.waitFor();
  await pop2.locator('.ant-btn-primary').click();
  ok(await expectMsg(page, /已解封/), '解封成功提示');
  await sleep(600);
  ok(await closingCard(page).getByText('已结束').first().isVisible(), '解封后状态回到「已结束」（可写）');

  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.ant-segmented-item', { hasText: '上月' }).first().click();
  await sleep(700);
  const row2 = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE }).first();
  await row2.getByRole('button', { name: /编\s*辑/ }).click();
  const editModal = page.locator('.ant-modal-container:visible');
  await editModal.waitFor({ state: 'visible', timeout: 10000 });
  await editModal.locator('input[inputmode="decimal"]').fill('99');
  await editModal.locator('.ant-modal-footer .ant-btn-primary').click();
  ok(await expectMsg(page, /已保存修改/), '解封后修改流水成功（88 → 99）');
  await sleep(600);

  await page.goto(BASE + '/settings');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  const card3 = closingCard(page);
  ok(
    (await card3.getByText(/99\.00/).count()) > 0,
    '重新封账前预览已更新为 99.00',
  );
  await card3.getByRole('button', { name: /封\s*账/ }).click();
  const modal3 = page.locator('.ant-modal-confirm, .ant-modal-content').first();
  await modal3.waitFor();
  ok((await modal3.innerText()).includes('99.00'), '重新封账弹窗展示更新后的结余');
  await modal3.getByRole('button', { name: /确\s*认\s*封\s*账/ }).click();
  ok(await expectMsg(page, /已封账/), '重新封账成功');
  await sleep(500);

  // ================= 审计日志（谁、何时、哪一期） =================
  await page.goto(BASE + '/audit');
  await page.getByText('审计日志').first().waitFor();
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(400);
  const auditRows = page.locator('.ant-table-tbody tr.ant-table-row');
  const first3 = [];
  for (let i = 0; i < Math.min(3, await auditRows.count()); i++) {
    first3.push((await auditRows.nth(i).innerText()).replace(/\n/g, '/'));
  }
  ok(
    first3.length >= 3 &&
      first3[0].includes('封账') &&
      first3[1].includes('解封') &&
      first3[2].includes('封账'),
    '审计最新三条 = 封账 → 解封 → 封账（留痕）',
    first3.join('  ||  '),
  );
  ok(
    first3.every((r) => r.includes('2026-09') && r.includes('admin')),
    '审计含账期 2026-09 与操作人 admin',
  );
  ok(
    /\d{4}-\d{2}-\d{2}/.test(first3[0] ?? ''),
    '审计含时间',
    first3[0],
  );
  await shot(page, 'periods-07-audit');

  // ================= 非管理员：入口隐藏 + 路由拦截 =================
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p2 = await ctx2.newPage();
  await login(p2, 'xiaowang', 'member123456');
  await p2.goto(BASE + '/mine');
  await sleep(500);
  ok(
    (await p2.getByRole('button', { name: /账期设置/ }).count()) === 0,
    'member 我的页无「账期设置」入口',
  );
  await p2.goto(BASE + '/settings');
  await p2.waitForURL((u) => u.pathname === '/', { timeout: 8000 }).catch(() => {});
  ok(p2.url().endsWith('/'), 'member 直访 /settings 被拦截回首页', p2.url());

  const ctx3 = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p3 = await ctx3.newPage();
  await login(p3, 'dazhi', 'member123456');
  await p3.goto(BASE + '/audit');
  await p3.waitForURL((u) => u.pathname === '/', { timeout: 8000 }).catch(() => {});
  ok(p3.url().endsWith('/'), 'viewer 直访 /audit 被拦截回首页', p3.url());

  // ================= 恢复基线（审计行保留作证据） =================
  await api('PUT', '/settings', tok, { settlement_day: 1 });
  await api('DELETE', '/closings/2026-09', tok);
  const post = await api(
    'GET',
    '/transactions?start_date=2026-09-01&end_date=2026-09-30&page=1&page_size=50',
    tok,
  );
  for (const t of post.body?.data?.items ?? []) {
    if (t.note === NOTE) await api('DELETE', `/transactions/${t.id}`, tok);
  }
  const dayBack = await api('GET', '/settings', tok);
  const sepLeft = await api(
    'GET',
    '/transactions?start_date=2026-09-01&end_date=2026-09-30&page=1&page_size=50',
    tok,
  );
  ok(
    dayBack.body?.data?.settlement_day === 1 && (sepLeft.body?.data?.total ?? 0) === 0,
    '基线恢复：结算日=1、走查流水已清理',
    `day=${dayBack.body?.data?.settlement_day} sep=${sepLeft.body?.data?.total}`,
  );

  await ctx3.close();
  await ctx2.close();
  await ctx.close();
  await browser.close();

  const pass = results.filter((r) => r.pass).length;
  console.log(`\n${pass}/${results.length} PASS`);
  if (pass !== results.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error('FATAL', e);
  process.exit(1); // 失败也要退出（否则 browser 进程挂住脚本）
});
