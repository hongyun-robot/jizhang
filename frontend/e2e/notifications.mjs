/* eslint-disable no-console */
/**
 * add-ledger-system 任务 10.3：specs/notifications 全场景浏览器走查。
 * 运行：node e2e/notifications.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 基线：铃铛纯图标、无红点（0 未读）
 *  - 首次超支生成 2 条预算提醒（总 + 餐饮）→ 铃铛红点显示数字 2
 *  - 点击铃铛打开提醒列表：类型标签/分类名/超出金额/账期可读
 *  - 单条已读 → 未读 2→1，行保留；全部已读 → 红点消失、历史仍可见
 *  - 还款提醒正文渲染（账户名/欠款/还款日/天数，SQL 种入真实 payload）
 *  - 周期补齐提醒真实链路：建模板 → 打开应用懒补齐 → 生成提醒
 *  - 移动端底部 Tab 铃铛形态与跳转；viewer（四档）可读可标已读
 * 运行结束恢复基线（预算清空、模板与生成流水清空、提醒清空、支出 922.66）。
 *
 * 提醒行存在性/正文断言走页面 DOM；还款提醒无法等 08:00 cron，故 SQL 种入
 * （生成逻辑由 notification_service 单测覆盖）。
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

let pass = 0,
  fail = 0;
function ok(cond, label, extra = '') {
  if (cond) {
    pass++;
    console.log(`  ✓ ${label}`);
  } else {
    fail++;
    console.log(`  ✗ FAIL: ${label}${extra ? ` — ${extra}` : ''}`);
  }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function shot(page, name) {
  await page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: false }).catch(() => {});
}

async function api(method, p, token, body) {
  const r = await fetch(API + p, {
    method,
    headers: { 'content-type': 'application/json', authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, body: j };
}
async function apiLogin(username, password) {
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const j = await r.json();
  if (!j.data?.access_token) throw new Error('login failed');
  return j.data.access_token;
}

/** dev 环境 SQL：走 stdin 传入（CJK/JSON 经 stdin 往返已验证）。
 * 优先 WSL docker（历史环境）；WSL/容器不可用时回退本机 MySQL 8.4（3307）。 */
const LOCAL_MYSQL = 'C:\\Program Files\\MySQL\\MySQL Server 8.4\\bin\\mysql.exe';
function sqlIn(q) {
  const args = `--default-character-set=utf8mb4 jizhang --skip-column-names`;
  try {
    return execSync(
      `wsl -d Ubuntu-20.04 -u root -- docker exec -i jizhang-mysql mysql -uroot -pdevpass ${args}`,
      { input: q, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], timeout: 8000 },
    ).trim();
  } catch {
    return execSync(
      `"${LOCAL_MYSQL}" --host=127.0.0.1 --port=3307 --user=root --password=devpass ${args}`,
      { input: q, encoding: 'utf8' },
    ).trim();
  }
}
const sqlN = (q) => Number(sqlIn(q) || '0');

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

/** 铃铛可见文本：无红点 = ''，红点数字 = '2'。 */
async function bellText(page, sel) {
  const loc = page.locator(sel);
  if ((await loc.count()) === 0) return '«no-bell»';
  return (await loc.first().innerText()).replace(/\s+/g, '');
}
async function waitBell(page, sel, want, timeout = 15000) {
  const t0 = Date.now();
  let last = '«none»';
  while (Date.now() - t0 < timeout) {
    last = await bellText(page, sel);
    if (last === want) return true;
    await sleep(300);
  }
  console.log(`    (waitBell 超时: want="${want}" got="${last}")`);
  return false;
}

const DESKTOP_BELL = '.brand .notif-bell';
const MOBILE_BELL = '.bottom-tabs .notif-bell';

async function main() {
  console.log('== 组10 提醒中心 e2e（specs/notifications） ==');
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();

  // ================= 0. 基线准备 =================
  console.log('— 0. 基线准备 —');
  const tok = await apiLogin('admin', 'admin123');
  const put0 = await api('PUT', '/settings', tok, { settlement_day: 1 });
  ok(put0.status === 200, '结算日 = 1');
  const clearB = await api('PUT', '/budgets/2026-10', tok, {});
  ok(clearB.status === 200, '预算清空');
  sqlIn('DELETE FROM transactions WHERE rule_id IS NOT NULL');
  sqlIn('DELETE FROM recurring_rules');
  sqlIn('DELETE FROM notifications');
  ok(sqlN('SELECT COUNT(*) FROM notifications') === 0, '提醒基线 0 条');
  ok(sqlN('SELECT COUNT(*) FROM recurring_rules') === 0, '周期模板基线 0 个');

  await login(page, 'admin', 'admin123');
  ok((await page.locator(DESKTOP_BELL).count()) === 1, '桌面侧栏 brand 行存在提醒铃铛');
  ok((await waitBell(page, DESKTOP_BELL, '')), '基线 0 未读：铃铛无红点', await bellText(page, DESKTOP_BELL));
  await shot(page, 'notif-01-bell-baseline');

  // ============ 1. 首次超支生成 2 条 → 红点数字 2 ============
  console.log('— 1. 预算超支真实生成 2 条未读 —');
  await page.goto(BASE + '/budgets');
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.budget-row[data-budget="total"]').first().waitFor({ timeout: 15000 });
  await page.locator('.budget-row[data-budget="total"] input').fill('1');
  await page.locator('.budget-row[data-budget="1"] input').fill('1');
  const saveBtn = page
    .locator('.ant-card', { hasText: '预算配置' })
    .getByRole('button', { name: /保\s*存/ })
    .first();
  await saveBtn.click();
  ok(await expectMsg(page, /预算已保存/), '保存预算（总 1 / 餐饮 1）成功提示');
  ok(await waitBell(page, DESKTOP_BELL, '2'), '首次超支 2 条 → 铃铛红点数字 2', await bellText(page, DESKTOP_BELL));
  ok(sqlN('SELECT COUNT(*) FROM notifications') === 2, 'DB 生成 2 条预算提醒');
  ok(sqlN(`SELECT COUNT(*) FROM notifications WHERE kind='budget_overdue'`) === 2, 'kind = budget_overdue ×2');
  await shot(page, 'notif-02-badge-2');

  // ============ 2. 点击铃铛打开提醒列表 ============
  console.log('— 2. 点击铃铛打开提醒列表 —');
  await page.locator(DESKTOP_BELL).click();
  await page.waitForURL('**/notifications', { timeout: 10000 });
  await page.locator('.notif-list').waitFor({ timeout: 10000 });
  ok(true, '点击铃铛跳转 /notifications');
  ok((await page.locator('.notif-row').count()) === 2, '列表 2 条提醒');
  ok((await page.locator('.notif-row.unread').count()) === 2, '2 条均为未读态');
  const listText = await page.locator('.notif-list').innerText();
  ok(
    listText.includes('预算超支') && listText.includes('总预算') && listText.includes('餐饮'),
    '类型标签 + 分类名（总预算/餐饮）',
    listText.replace(/\n/g, ' ').slice(0, 160),
  );
  ok(listText.includes('921.66') && listText.includes('2026-10'), '超出金额 921.66 + 账期 2026-10');
  ok((await page.locator('.notif-row', { hasText: '预算超支' }).count()) === 2, '预算超支标签 ×2');
  await shot(page, 'notif-03-list-unread');

  // ============ 3. 单条已读 ============
  console.log('— 3. 单条已读 —');
  await page
    .locator('.notif-row.unread')
    .first()
    .getByRole('button', { name: /标\s*为\s*已\s*读/ })
    .click();
  ok(await waitBell(page, DESKTOP_BELL, '1'), '单条已读 → 红点 2→1', await bellText(page, DESKTOP_BELL));
  ok((await page.locator('.notif-row').count()) === 2, '已读行保留在列表中');
  ok((await page.locator('.notif-row.unread').count()) === 1, '未读行剩 1 条');
  await shot(page, 'notif-04-single-read');

  // ============ 4. 全部已读 ============
  console.log('— 4. 全部已读 —');
  const allBtn = page.getByRole('button', { name: /全\s*部\s*已\s*读/ });
  await allBtn.click();
  ok(await waitBell(page, DESKTOP_BELL, ''), '全部已读 → 红点消失', await bellText(page, DESKTOP_BELL));
  ok((await page.locator('.notif-row').count()) === 2, '历史提醒仍可见（2 条）');
  ok((await page.locator('.notif-row.unread').count()) === 0, '无未读行');
  ok(await page.getByRole('button', { name: /全\s*部\s*已\s*读/ }).isDisabled(), '未读为 0 时「全部已读」禁用');
  ok((await page.locator('.notif-row', { hasText: '已读' }).count()) >= 2, '已读行显示「已读」标签');
  await shot(page, 'notif-05-all-read');

  // ============ 5. 还款提醒渲染（SQL 种入真实 payload） ============
  console.log('— 5. 还款提醒正文渲染 —');
  sqlIn(`
INSERT INTO notifications (kind, payload, read_at, created_at, owner_id) VALUES
('repayment_due', '{"dedup_key":"repayment_due:5:2026-10","account_id":5,"account_name":"信用卡","debt":"100.00","repayment_date":"2026-10-10","days":8,"overdue":false}', NULL, NOW(), 1);
`);
  ok(sqlN(`SELECT COUNT(*) FROM notifications WHERE kind='repayment_due'`) === 1, '种入 1 条还款提醒');
  await page.reload();
  await page.waitForLoadState('networkidle').catch(() => {});
  ok(await waitBell(page, DESKTOP_BELL, '1'), '还款提醒 → 红点 1', await bellText(page, DESKTOP_BELL));
  const repayRow = page.locator('.notif-row', { hasText: '还款提醒' }).first();
  await repayRow.waitFor({ timeout: 10000 });
  const rText = await repayRow.innerText();
  ok(
    rText.includes('还款提醒') && rText.includes('信用卡') && rText.includes('100.00') &&
      rText.includes('2026-10-10') && rText.includes('距还款日 8 天'),
    '还款提醒含账户名/欠款/还款日/天数',
    rText.replace(/\n/g, ' '),
  );
  await shot(page, 'notif-06-repayment');

  // ============ 6. 周期补齐提醒（真实链路：建模板 → 懒补齐） ============
  console.log('— 6. 周期补齐提醒真实链路 —');
  const mk = await api('POST', '/recurring', tok, {
    amount: '88.88',
    freq: 'monthly',
    account_id: 1,
    category_id: 17, // 午餐（末级分类；根分类不可直接记账）
    start_date: '2026-10-01',
  });
  ok(mk.status === 200 && mk.body.code === 0, '创建周期模板（月/88.88/午餐/2026-10-01）');
  // 直接调懒补齐接口：页面 reload 时 AppShell 会并发触发 liabilities/ensure，
  // 其 BeforeEnsure 走的是非懒路径（不落提醒），与懒补齐竞态导致生成被抢跑。
  const ens = await api('POST', '/recurring/ensure', tok);
  ok(ens.status === 200 && ens.body.data?.generated === 1, '懒补齐生成 1 笔', JSON.stringify(ens.body));
  await page.reload(); // 拉取提醒列表（补齐已由接口完成，reload 只为刷新展示）
  await page.waitForLoadState('networkidle').catch(() => {});
  ok(await waitBell(page, DESKTOP_BELL, '2'), '懒补齐生成 → 红点 2（还款+补记）', await bellText(page, DESKTOP_BELL));
  ok(sqlN(`SELECT COUNT(*) FROM notifications WHERE kind='recurring_patched'`) === 1, 'DB 生成 1 条补齐提醒');
  ok(sqlN('SELECT COUNT(*) FROM transactions WHERE rule_id IS NOT NULL') === 1, '模板补记 1 笔流水');
  const recurRow = page.locator('.notif-row', { hasText: '周期补记' }).first();
  await recurRow.waitFor({ timeout: 10000 });
  const rcText = await recurRow.innerText();
  ok(rcText.includes('周期补记') && rcText.includes('已补记 1 笔'), '补齐提醒含笔数', rcText.replace(/\n/g, ' '));
  ok((await page.locator('.notif-row').count()) === 4, '列表累计 4 条（含已读历史）');
  await shot(page, 'notif-07-recurring');

  // ============ 7. 移动端底部 Tab 铃铛 ============
  console.log('— 7. 移动端形态 —');
  await page.setViewportSize({ width: 390, height: 844 });
  await sleep(800);
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.bottom-tabs').waitFor({ timeout: 10000 });
  ok((await page.locator(MOBILE_BELL).count()) === 1, '移动端底部 Tab 栏右端铃铛');
  ok((await bellText(page, MOBILE_BELL)) === '2', '移动端红点显示 2', await bellText(page, MOBILE_BELL));
  await shot(page, 'notif-08-mobile-dash');
  await page.locator(MOBILE_BELL).click();
  await page.waitForURL('**/notifications', { timeout: 10000 });
  await page.locator('.notif-list').waitFor({ timeout: 10000 });
  ok((await page.locator('.notif-row').count()) === 4, '移动端打开列表 4 条');
  await shot(page, 'notif-09-mobile-list');
  await page.setViewportSize({ width: 1280, height: 800 });
  await sleep(800);

  // ============ 8. viewer（四档开放） ============
  console.log('— 8. viewer 可读可标已读 —');
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p2 = await ctx2.newPage();
  await login(p2, 'dazhi', 'member123456');
  await p2.goto(BASE + '/notifications');
  await p2.waitForLoadState('networkidle').catch(() => {});
  await p2.locator('.notif-list').waitFor({ timeout: 15000 });
  ok((await p2.locator('.notif-row').count()) === 3, 'viewer 可读 3 条提醒（还款提醒按 owner 隔离，看不到 admin 的）');
  ok((await p2.locator('.notif-row.unread').count()) === 1, 'viewer 看到 1 条未读（补齐提醒）');
  ok(await waitBell(p2, DESKTOP_BELL, '1'), 'viewer 铃铛红点 1', await bellText(p2, DESKTOP_BELL));
  await p2
    .locator('.notif-row.unread')
    .first()
    .getByRole('button', { name: /标\s*为\s*已\s*读/ })
    .click();
  ok(await waitBell(p2, DESKTOP_BELL, ''), 'viewer 标已读生效 → 未读归零', await bellText(p2, DESKTOP_BELL));
  await shot(p2, 'notif-10-viewer');
  await ctx2.close();

  // ============ 9. 恢复基线 ============
  console.log('— 9. 恢复基线 —');
  const clr = await api('PUT', '/budgets/2026-10', tok, {});
  ok(clr.status === 200, '预算清空');
  sqlIn('DELETE FROM transactions WHERE rule_id IS NOT NULL');
  sqlIn('DELETE FROM recurring_rules');
  sqlIn('DELETE FROM notifications');
  const nb = await api('GET', '/notifications', tok);
  ok(nb.status === 200 && nb.body.data.list.length === 0 && nb.body.data.unread === 0, '提醒清空 unread=0');
  ok(sqlN('SELECT COUNT(*) FROM recurring_rules') === 0, '模板清空');
  ok(sqlN('SELECT COUNT(*) FROM budgets') === 0, '预算表清空');
  ok(
    sqlN(`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE kind='expense' AND period_key='2026-10'`) === 922.66,
    '支出基线恢复 922.66',
    String(sqlN(`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE kind='expense' AND period_key='2026-10'`)),
  );
  await page.goto(BASE + '/notifications');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(800);
  ok((await page.locator('.ant-empty').count()) > 0, '清空后空态「暂无提醒」');
  ok((await bellText(page, DESKTOP_BELL)) === '', '红点消失', await bellText(page, DESKTOP_BELL));
  await shot(page, 'notif-11-empty');

  await browser.close();
  console.log(`\n组10 提醒 e2e: ${pass} 通过, ${fail} 失败`);
  if (fail > 0) process.exit(1);
}

main().catch((e) => {
  console.error('FATAL:', e);
  process.exit(1);
});
