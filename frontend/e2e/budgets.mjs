/* eslint-disable no-console */
/**
 * add-ledger-system 任务 8.3：specs/budgets 全场景浏览器走查。
 * 运行：node e2e/budgets.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 配置本期预算（总 8000 / 餐饮 3000）→ 首页与统计页显示对应预算进度
 *  - 预算消耗按账期支出计算（分类含子分类）：进度 922.66/3000
 *  - 超支标红并显示超出金额（餐饮降到 500 → 已超支 422.66）
 *  - 首次超支生成一条站内提醒（分类名 + 超出金额 + 去重键），重复保存不重复
 *  - 预算配置仅管理员（viewer 无输入框/无保存按钮），查看对四档开放
 * 运行结束恢复基线（预算清空、提醒清空）。
 *
 * 提醒行的存在性用 dev 环境 SQL 断言（提醒读取接口在任务 10.2 才提供）。
 */
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';
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
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username: u, password: p }),
  });
  const j = await r.json();
  if (j.code !== 0) throw new Error(j.message);
  return j.data.access_token;
}

async function api(method, path_, token, body) {
  const r = await fetch(API + path_, {
    method,
    headers: { 'content-type': 'application/json', authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, body: j };
}

async function login(page, username, password) {
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

/** dev 环境 SQL（提醒读取接口 10.2 才有，此处直接断言表内容）。
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
const notifCount = () => Number(sql('SELECT COUNT(*) FROM notifications'));

/** 预算页配置卡片。 */
const budgetCard = (page) => page.locator('.ant-card', { hasText: '预算配置' }).first();
const row = (page, scope) => page.locator(`.budget-row[data-budget="${scope}"]`);

async function metaText(page, scope) {
  return (await row(page, scope).locator('.budget-meta').innerText().catch(() => '')).replace(/\n/g, ' ');
}

async function save(page) {
  await budgetCard(page).getByRole('button', { name: /保\s*存/ }).click();
  const got = await expectMsg(page, /预算已保存/);
  await sleep(700);
  return got;
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();

  // ================= 基线准备（API + SQL，幂等） =================
  const tok = await apiLogin('admin', 'admin123');
  await api('PUT', '/settings', tok, { settlement_day: 1 });
  await api('PUT', '/budgets/2026-10', tok, {});
  sql('DELETE FROM notifications');

  await login(page, 'admin', 'admin123');
  await page.goto(BASE + '/budgets');
  await page.getByText('预算').first().waitFor();
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);

  // ================= 0. 基线：未配置 =================
  ok(
    (await budgetCard(page).locator('.ant-picker input').inputValue()) === '2026-10',
    '账期选择器默认当前账期 2026-10（后端返回，客户端不重算）',
  );
  ok((await page.locator('.ant-tag', { hasText: '可配置' }).count()) > 0, '管理员标记「可配置」');
  ok((await page.locator('.budget-row').count()) === 9, '总预算 + 8 个一级支出分类行（配置回填）');
  ok(
    (await row(page, 'total').innerText()).includes('未设置'),
    '基线：总预算未设置',
  );
  ok(
    (await budgetCard(page).getByRole('button', { name: /保\s*存/ }).count()) > 0,
    '管理员可见保存按钮',
  );
  await shot(page, 'budgets-01-baseline');

  // ================= 1. 配置本期预算（spec 场景 1） =================
  await row(page, 'total').locator('input').fill('8000');
  await row(page, '1').locator('input').fill('3000');
  ok(await save(page), '保存预算成功提示');
  const totalMeta = await metaText(page, 'total');
  ok(
    totalMeta.includes('922.66') && totalMeta.includes('8000.00'),
    '总预算进度按本期支出 922.66 / 8000.00',
    totalMeta,
  );
  const mealMeta = await metaText(page, '1');
  ok(
    mealMeta.includes('922.66') && mealMeta.includes('3000.00'),
    '餐饮（含子分类）进度 922.66 / 3000.00',
    mealMeta,
  );
  ok((await page.locator('.budget-over').count()) === 0, '未超支不标红');
  await shot(page, 'budgets-02-configured');

  // ================= 2. 首页显示对应预算进度（spec 场景 1 THEN） =================
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(400);
  const dashCard = page.locator('.ant-card', { hasText: '预算进度' }).first();
  const dashText = await dashCard.innerText();
  ok(
    dashText.includes('账期 2026-10') && dashText.includes('总预算') && dashText.includes('餐饮'),
    '首页预算进度卡：账期 + 总预算 + 餐饮',
    dashText.replace(/\n/g, ' | ').slice(0, 120),
  );
  ok(dashText.includes('8000.00'), '首页展示配置的总预算 8000.00');
  await shot(page, 'budgets-03-home');

  // ================= 3. 统计页显示对应预算进度 =================
  await page.goto(BASE + '/stats');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(400);
  const statsCard = page.locator('.ant-card', { hasText: '预算进度' }).first();
  const statsText = await statsCard.innerText();
  ok(await statsCard.isVisible(), '统计页存在预算进度卡');
  ok(
    statsText.includes('8000.00') && statsText.includes('餐饮'),
    '统计页展示同一预算进度（8000.00 / 餐饮）',
  );
  await shot(page, 'budgets-04-stats');

  // ================= 4. 超支标红 + 超出金额（spec 场景 2/3） =================
  await page.goto(BASE + '/budgets');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  await row(page, '1').locator('input').fill('500');
  ok(await save(page), '餐饮预算下调 3000 → 500');
  ok(
    (await row(page, '1').locator('.ant-progress-status-exception').count()) > 0,
    '餐饮进度条标红（exception）',
  );
  const over = await row(page, '1').locator('.budget-over').innerText();
  ok(
    over.includes('已超支') && over.includes('422.66'),
    '显示超出金额 已超支 ¥422.66（922.66 - 500）',
    over,
  );
  ok(
    (await row(page, 'total').locator('.ant-progress-status-exception').count()) === 0,
    '总预算未超支不标红',
  );
  await shot(page, 'budgets-05-over-budget');

  // ================= 5. 首次超支生成一条提醒（spec 场景 3 THEN） =================
  let n = notifCount();
  ok(n === 1, '首次超支生成 1 条站内提醒', `count=${n}`);
  const payload = sql("SELECT payload FROM notifications WHERE kind='budget_overdue'");
  ok(
    payload.includes('餐饮') && payload.includes('422.66') && payload.includes('budget_overdue:2026-10:cat:1'),
    '提醒含分类名与超出金额（+ 去重键）',
    payload,
  );

  // ================= 6. 同账期同预算不重复提醒 =================
  ok(await save(page), '重复保存同配置');
  n = notifCount();
  ok(n === 1, '重复保存不重复提醒', `count=${n}`);

  // ================= 7. 查看开放 / 配置仅管理员（viewer） =================
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p2 = await ctx2.newPage();
  await login(p2, 'dazhi', 'member123456');
  await p2.goto(BASE + '/budgets');
  await p2.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  ok((await p2.locator('.ant-tag', { hasText: '仅查看' }).count()) > 0, 'viewer 标记「仅查看」');
  ok((await p2.locator('.budget-input').count()) === 0, 'viewer 无预算输入框（配置仅管理员）');
  ok(
    (await budgetCard(p2).getByRole('button', { name: /保\s*存/ }).count()) === 0,
    'viewer 无保存按钮',
  );
  const vText = await p2.locator('.budget-row[data-budget="1"]').innerText();
  ok(vText.includes('已超支') && vText.includes('422.66'), 'viewer 可查看进度与超支（四档开放）', vText.replace(/\n/g, ' '));
  await shot(p2, 'budgets-06-viewer-readonly');
  await ctx2.close();

  // ================= 8. 恢复基线 =================
  await row(page, 'total').locator('input').fill('0');
  await row(page, '1').locator('input').fill('0');
  ok(await save(page), '清空预算（0 = 不设）保存');
  ok(
    (await row(page, 'total').innerText()).includes('未设置') &&
      (await row(page, '1').innerText()).includes('未设置'),
    '预算页回到全部未设置',
  );
  sql('DELETE FROM notifications');
  n = notifCount();
  ok(n === 0, '提醒清空', `count=${n}`);

  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(400);
  const homeEmpty = await page.locator('.ant-card', { hasText: '预算进度' }).first().innerText();
  ok(
    homeEmpty.includes('预算尚未配置') && homeEmpty.includes('去配置'),
    '首页回到空态（预算尚未配置 · 去配置）',
    homeEmpty.replace(/\n/g, ' | '),
  );
  await shot(page, 'budgets-07-baseline-restored');

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
