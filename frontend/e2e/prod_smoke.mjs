/* eslint-disable no-console */
/**
 * 生产站浏览器冒烟：真实浏览器验证 curl 测不到的部分
 * （JS 执行、静态资源、react-router basename、AntD 渲染、cookie 恢复会话）。
 * 运行：node e2e/prod_smoke.mjs
 */
import { chromium } from 'playwright-core';
import { randomUUID } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SHOTS = path.join(__dirname, 'shots');
fs.mkdirSync(SHOTS, { recursive: true });

const EXE =
  process.env.CHROME_EXE ??
  [
    path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe'),
    'C:/Program Files/Google/Chrome/Application/chrome.exe',
    'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  ].find((candidate) => fs.existsSync(candidate));
const BASE = process.env.BASE ?? 'https://jizhang.zhanghongyun.cn';
const ADMIN_USER = process.env.JZ_USER ?? 'admin';
const ADMIN_PASS = process.env.JZ_PASS;
const USER = `deploy-smoke-${randomUUID()}`;
const PASS = randomUUID();
let smokeUserId;
let adminToken;
const SMOKE_NOTE = `生产冒烟-${randomUUID()}`;
const INITIAL_COMPARE_DELAY = Number(process.env.PROD_SMOKE_COMPARE_DELAY_MS ?? 0);

if (!ADMIN_PASS) {
  console.error('FATAL 需要环境变量 JZ_PASS（生产管理员口令）');
  process.exit(2);
}
if (!Number.isFinite(INITIAL_COMPARE_DELAY) || INITIAL_COMPARE_DELAY < 0 || INITIAL_COMPARE_DELAY > 15000) {
  throw new Error('PROD_SMOKE_COMPARE_DELAY_MS 必须为 0–15000 毫秒');
}

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });

const consoleErrors = [];
const failedRequests = [];

if (!EXE) throw new Error('找不到 Chromium 浏览器，请通过 CHROME_EXE 指定');
const browser = await chromium.launch({ executablePath: EXE, headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
if (INITIAL_COMPARE_DELAY > 0) {
  let delayed = false;
  // Exercise the real loading boundary once; later requests keep normal timing.
  await page.route('**/api/v1/stats/compare*', async (route) => {
    if (!delayed) {
      delayed = true;
      await new Promise((resolve) => setTimeout(resolve, INITIAL_COMPARE_DELAY));
    }
    await route.continue();
  });
}

page.on('console', (m) => {
  if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 200));
});
page.on('requestfailed', (r) => failedRequests.push(`${r.url().slice(0, 120)} ${r.failure()?.errorText}`));
let auth401 = 0; // 未登录时探测 refresh 得到 401 属设计预期，控制台也会报，需按条数抵扣
page.on('response', (r) => {
  if (r.status() === 401 && r.url().includes('/auth/')) {
    auth401 += 1;
    return;
  }
  if (r.status() >= 400) failedRequests.push(`HTTP ${r.status()} ${r.url().slice(0, 120)}`);
});

try {
  // Each user owns an independent ledger. Keep smoke writes out of real users' data.
  const adminLogin = await fetch(BASE + '/api/v1/auth/login', {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ protocol_version: 2, username: ADMIN_USER, password: ADMIN_PASS }),
  }).then((response) => response.json());
  adminToken = adminLogin?.data?.access_token;
  if (adminLogin?.code !== 0 || !adminToken) throw new Error('冒烟准备：管理员认证失败');
  const createdUser = await fetch(BASE + '/api/v1/users', {
    method: 'POST', headers: { authorization: `Bearer ${adminToken}`, 'content-type': 'application/json' },
    body: JSON.stringify({ username: USER, password: PASS, role: 'admin' }),
  }).then((response) => response.json());
  if (createdUser?.code !== 0 || !createdUser?.data?.id) throw new Error('冒烟准备：临时账本账号创建失败');
  smokeUserId = createdUser.data.id;
  // 1. 首屏（未登录应被 RequireAuth 异步跳到 /login，故等 URL 变化）
  const resp = await page.goto(BASE + '/', { waitUntil: 'domcontentloaded', timeout: 30000 });
  ok(resp?.status() === 200, '首页 HTTP 200', String(resp?.status()));
  await page.waitForURL((u) => u.href.includes('/login'), { timeout: 15000 }).catch(() => {});
  ok(page.url().includes('/login'), '未登录 → 跳登录页', page.url());

  // 2. 登录
  await page.locator('input[placeholder="用户名"]').fill(USER);
  await page.locator('input[placeholder="密码"]').fill(PASS);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.includes('/login'), { timeout: 20000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  ok(!page.url().includes('/login'), '登录成功跳首页', page.url());
  ok(page.url() === BASE + '/' || page.url() === BASE, '首页地址正确（无多余前缀）', page.url());

  // 3. 首页数据面板渲染
  const hasIncome = await page.locator('.hero-split').getByText('本期收入')
    .waitFor({ state: 'visible', timeout: 20000 }).then(() => true).catch(() => false);
  ok(hasIncome, '首页渲染「本期收入」面板', hasIncome ? 'ready' : 'not ready');
  await shot(page, 'prod-01-home');

  // 4. 各路由可达（SPA 路由 + 深链刷新）
  for (const [path, marker] of [
    ['/records', '流水'],
    ['/stats', '统计'],
    ['/budgets', '预算'],
    ['/accounts', '账户'],
    ['/mine', '我的'],
  ]) {
    const r = await page.goto(BASE + path, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForLoadState('networkidle').catch(() => {});
    const hasMarker = await page.getByText(marker, { exact: false }).first().count();
    ok(r?.status() === 200 && hasMarker > 0, `深链 ${path} 可刷新直达`, `http=${r?.status()} marker=${hasMarker}`);
  }
  await shot(page, 'prod-02-stats');

  // Mine navigation and CRUD use only this run's private platform directory.
  await page.getByRole('button', { name: '平台 / 商家管理', exact: true }).click();
  await page.waitForURL(BASE + '/platforms');
  await page.getByRole('heading', { name: '平台 / 商家管理', exact: true }).waitFor();
  ok(true, '我的 → 平台 / 商家管理可达');
  await page.getByRole('button', { name: '新建平台 / 商家', exact: true }).click();
  let platformEditor = page.getByRole('dialog', { name: '新建平台 / 商家', exact: true });
  await platformEditor.getByLabel('名称', { exact: true }).fill(SMOKE_NOTE);
  await platformEditor.getByRole('button', { name: /确\s*定/ }).click();
  await platformEditor.waitFor({ state: 'hidden' });
  let platformRow = page.locator('.platform-row').filter({ hasText: SMOKE_NOTE });
  await platformRow.waitFor();
  ok(true, '本人平台 / 商家创建成功');
  const adminDirectory = await fetch(BASE + '/api/v1/voucher-platforms', {
    headers: { authorization: `Bearer ${adminToken}` },
  }).then((response) => response.json());
  ok(adminDirectory.code === 0 && !adminDirectory.data.some((row) => row.name === SMOKE_NOTE), '平台 / 商家跨账号不共享');
  await platformRow.getByRole('button', { name: `重命名 ${SMOKE_NOTE}`, exact: true }).click();
  platformEditor = page.getByRole('dialog', { name: '重命名平台 / 商家', exact: true });
  await platformEditor.getByLabel('名称', { exact: true }).fill(SMOKE_NOTE + '-改名');
  await platformEditor.getByRole('button', { name: /确\s*定/ }).click();
  await platformEditor.waitFor({ state: 'hidden' });
  platformRow = page.locator('.platform-row').filter({ hasText: SMOKE_NOTE + '-改名' });
  await platformRow.waitFor();
  ok(true, '本人平台 / 商家重命名成功');
  await platformRow.getByRole('button', { name: `删除 ${SMOKE_NOTE}-改名`, exact: true }).click();
  await page.locator('.ant-popconfirm:visible').getByRole('button', { name: /确\s*定/ }).click();
  await platformRow.waitFor({ state: 'hidden' });
  ok(true, '本人平台 / 商家删除成功');
  await shot(page, 'prod-02-platforms');

  // 5. 记一笔（真实写入，随后删除还原）
  await page.goto(BASE + '/records', { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => {});
  const addBtn = page.locator('.page-head').getByRole('button', { name: '记一笔' }).first();
  ok((await addBtn.count()) > 0, '流水页存在「记一笔」入口');
  await addBtn.click();
  // 记账弹窗内还挂了个 forceRender 的购券弹窗（隐藏、0×0），必须只取可见的那个容器
  const modal = page.locator('.ant-modal-container:visible');
  await modal.waitFor({ state: 'visible', timeout: 10000 });
  ok(true, '记账弹窗打开');
  await shot(page, 'prod-03-txn-modal');

  const amount = `0.0${(step % 9) + 1}`;
  await modal.locator('.amount-input input, input.amount-input').first().fill(amount);
  // 选一个末级分类（一级分类不可直接记账）
  const leaf = modal.locator('.cat-leaves .ant-tag, .cat-leaves button, .cat-leaves [role="button"]').first();
  if ((await leaf.count()) > 0) await leaf.click();
  // 唯一标记只匹配本次运行，不清理其他冒烟或用户已有流水。
  await modal.getByPlaceholder('备注（可选）').fill(SMOKE_NOTE);
  // 账户必填（不选会被表单校验拦下）；AntD Select 的 placeholder 是独立节点
  const acctSelect = modal.locator('.ant-select', { hasText: '账户' }).first();
  await acctSelect.click();
  const acctOpt = page.locator('.ant-select-dropdown:visible .ant-select-item').first();
  await acctOpt.waitFor({ state: 'visible', timeout: 5000 });
  await acctOpt.click();
  await page.waitForTimeout(300);
  const submit = modal.locator('.ant-modal-footer .ant-btn-primary').first();
  await submit.click();
  const toastOk = await page.getByText('已记一笔').first().waitFor({ timeout: 10000 }).then(() => true).catch(() => false);
  ok(toastOk, '记一笔提交成功（UI 提示）');
  await shot(page, 'prod-03b-txn-created');

  // 6. 刷新恢复会话（refresh cookie 是否生效）
  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => {});
  ok(!page.url().includes('/login'), '刷新后会话保持（refresh cookie 生效）', page.url());

  // 7. 登出
  await page.goto(BASE + '/mine', { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => {});
  const logoutBtn = page.getByRole('button', { name: /退出所有设备|退?出登录|登\s*出/ }).first();
  if ((await logoutBtn.count()) > 0) {
    await logoutBtn.click();
    const confirm = page.locator('.ant-popconfirm .ant-btn-primary, .ant-modal-confirm-btns .ant-btn-primary').first();
    if ((await confirm.count()) > 0) await confirm.click();
    await page.waitForURL((u) => u.href.includes('/login'), { timeout: 15000 }).catch(() => {});
    ok(page.url().includes('/login'), '登出跳登录页', page.url());
  } else {
    // 有的实现是链接而非按钮
    const link = page.getByText(/退出所有设备|登\s*出/).first();
    ok((await link.count()) > 0, '登出入口存在', '未找到按钮，文本入口也未命中');
  }
  await shot(page, 'prod-04-logout');

  // 8. 登出后访问受保护页应回登录页
  await page.goto(BASE + '/stats', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(1500);
  ok(page.url().includes('/login'), '登出后访问 /stats 被拦截', page.url());

  // 9. 移动端底栏（ui-shell）
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' });
  await page.locator('input[placeholder="用户名"]').fill(USER);
  await page.locator('input[placeholder="密码"]').fill(PASS);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.includes('/login'), { timeout: 20000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.bottom-tabs').waitFor({ state: 'visible', timeout: 10000 });
  const tabs = await page.locator('.bottom-tabs .tab').count();
  ok(tabs === 4, '移动端四项导航可见', `tabs=${tabs}`);
  await shot(page, 'prod-05-mobile');
} catch (e) {
  ok(false, '执行异常', String(e).replaceAll(PASS, '<REDACTED>').replaceAll(ADMIN_PASS, '<REDACTED>').slice(0, 300));
  await shot(page, 'prod-99-error').catch(() => {});
}

// 控制台错误只作信息输出：第三方 CDN/图标 404 之类不该判失败，但要能看见。
// 未登录时探测 /auth/refresh 得到的 401 是设计预期，按条数抵扣。
const benign401 = auth401;
const realErrors = consoleErrors.filter((t) => !/favicon|ResizeObserver|Download the React DevTools/i.test(t));
let seen401 = 0;
const filteredErrors = realErrors.filter((t) => {
  if (/status of 401/.test(t)) {
    seen401 += 1;
    return seen401 > benign401; // 超出预期条数才算真问题
  }
  return true;
});
const realFails = failedRequests.filter((u) => !/favicon/i.test(u));
console.log(`\n-- 控制台错误 ${filteredErrors.length} 条（已抵扣预期 401: ${benign401}）`);
filteredErrors.slice(0, 8).forEach((t) => console.log(`   ${t}`));
console.log(`-- 失败请求 ${realFails.length} 条`);
realFails.slice(0, 8).forEach((t) => console.log(`   ${t}`));

// 还原：只删除本次运行的唯一标记流水，验证清理确实成功。
let cleanupFailed = false;
try {
  const lr = await fetch(BASE + '/api/v1/auth/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ protocol_version: 2, username: USER, password: PASS }),
  });
  const lj = await lr.json();
  const tok = lj?.data?.access_token;
  if (lj?.code !== 0 || !tok) throw new Error('清理登录失败');
  if (tok) {
    const listUrl = BASE + '/api/v1/transactions?page=1&page_size=100&keyword=' + encodeURIComponent(SMOKE_NOTE);
    const list = await fetch(listUrl, {
      headers: { authorization: `Bearer ${tok}` },
    }).then((r) => r.json());
    if (list?.code !== 0) throw new Error('清理流水查询失败');
    const hits = (list?.data?.items ?? list?.data?.list ?? []).filter((t) => t.note === SMOKE_NOTE);
    for (const t of hits) {
      const response = await fetch(BASE + `/api/v1/transactions/${t.id}`, { method: 'DELETE', headers: { authorization: `Bearer ${tok}` } });
      const result = await response.json();
      if (!response.ok || result?.code !== 0) throw new Error(`清理流水 ${t.id} 失败`);
    }
    const remaining = await fetch(listUrl, { headers: { authorization: `Bearer ${tok}` } }).then((r) => r.json());
    if (remaining?.code !== 0 || (remaining?.data?.items ?? remaining?.data?.list ?? []).some((t) => t.note === SMOKE_NOTE)) {
      throw new Error('清理后仍有本次冒烟流水或查询失败');
    }
    console.log(`-- 清理冒烟流水 ${hits.length} 条`);
    const catalog = await fetch(BASE + '/api/v1/voucher-platforms', {
      headers: { authorization: `Bearer ${tok}` },
    }).then((response) => response.json());
    if (catalog?.code !== 0 || !Array.isArray(catalog.data)) throw new Error('清理商家查询失败');
    for (const row of catalog.data.filter((row) => [SMOKE_NOTE, SMOKE_NOTE + '-改名'].includes(row.name))) {
      const response = await fetch(BASE + `/api/v1/voucher-platforms/${row.id}`, {
        method: 'DELETE', headers: { authorization: `Bearer ${tok}` },
      });
      const body = await response.json();
      if (!response.ok || body.code !== 0) throw new Error('清理商家失败');
    }
  }
} catch (e) {
  cleanupFailed = true;
  console.log(`-- 清理失败: ${String(e).replaceAll(PASS, '<REDACTED>').replaceAll(ADMIN_PASS, '<REDACTED>').slice(0, 120)}`);
}

if (smokeUserId && adminToken) {
  try {
    const response = await fetch(BASE + `/api/v1/users/${smokeUserId}/status`, {
      method: 'PATCH', headers: { authorization: `Bearer ${adminToken}`, 'content-type': 'application/json' },
      body: JSON.stringify({ status: 'disabled' }),
    });
    const body = await response.json();
    if (!response.ok || body?.code !== 0) throw new Error('临时冒烟账号停用失败');
    console.log('-- 临时账本账号已停用，会话已撤销');
  } catch (error) {
    cleanupFailed = true;
    console.log(`-- 临时账号清理失败: ${String(error).slice(0, 120)}`);
  }
}

await browser.close();

const pass = results.filter((r) => r.pass).length;
const fail = results.length - pass;
console.log(`\n==== ${pass}/${results.length} PASS ====`);
if (fail > 0 || realFails.length > 0 || filteredErrors.length > 0 || cleanupFailed) {
  console.log('HAS_ISSUES');
  process.exit(1);
}
console.log('ALL_GREEN');
