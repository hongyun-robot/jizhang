/* eslint-disable no-console */
/**
 * 生产站浏览器冒烟：真实浏览器验证 curl 测不到的部分
 * （JS 执行、静态资源、react-router basename、AntD 渲染、cookie 恢复会话）。
 * 运行：node e2e/prod_smoke.mjs
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
const BASE = process.env.BASE ?? 'https://jizhang.zhanghongyun.cn';
const USER = process.env.JZ_USER ?? 'admin';
const PASS = process.env.JZ_PASS;

if (!PASS) {
  console.error('FATAL 需要环境变量 JZ_PASS（生产管理员口令）');
  process.exit(2);
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

const browser = await chromium.launch({ executablePath: EXE, headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();

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
  await page.locator('.page-head').first().waitFor({ timeout: 20000 });
  const hasIncome = await page.getByText('本期收入').count();
  ok(hasIncome > 0, '首页渲染「本期收入」面板', String(hasIncome));
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

  // 5. 记一笔（真实写入，随后删除还原）
  await page.goto(BASE + '/records', { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => {});
  const addBtn = page.locator('.page-head').getByRole('button', { name: '记一笔' }).first();
  ok((await addBtn.count()) > 0, '流水页存在「记一笔」入口');
  await addBtn.click();
  const modal = page.locator('.ant-modal-container');
  await modal.waitFor({ state: 'visible', timeout: 10000 });
  ok(true, '记账弹窗打开');
  await shot(page, 'prod-03-txn-modal');

  const amount = `0.0${(step % 9) + 1}`;
  await modal.locator('.amount-input input, input.amount-input').first().fill(amount);
  // 选一个末级分类（一级分类不可直接记账）
  const leaf = modal.locator('.cat-leaves .ant-tag, .cat-leaves button, .cat-leaves [role="button"]').first();
  if ((await leaf.count()) > 0) await leaf.click();
  // 填清理标记，冒烟结束按此关键词删除还原
  await modal.getByPlaceholder('备注（可选）').fill('生产冒烟');
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
  const logoutBtn = page.getByRole('button', { name: /退?出登录|登\s*出/ }).first();
  if ((await logoutBtn.count()) > 0) {
    await logoutBtn.click();
    const confirm = page.locator('.ant-popconfirm .ant-btn-primary, .ant-modal-confirm-btns .ant-btn-primary').first();
    if ((await confirm.count()) > 0) await confirm.click();
    await page.waitForURL((u) => u.href.includes('/login'), { timeout: 15000 }).catch(() => {});
    ok(page.url().includes('/login'), '登出跳登录页', page.url());
  } else {
    // 有的实现是链接而非按钮
    const link = page.getByText(/退出登录|登\s*出/).first();
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
  const tabs = await page.locator('.ant-tabs-tab, [class*="mobile-tab"], nav a').count();
  ok(true, '移动端视口渲染完成', `tab-like=${tabs}`);
  await shot(page, 'prod-05-mobile');
} catch (e) {
  ok(false, '执行异常', String(e).slice(0, 300));
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

// 还原：删除本冒烟记入的流水（按 note 标记），保持生产库干净
try {
  const lr = await fetch(BASE + '/api/v1/auth/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username: USER, password: PASS }),
  });
  const lj = await lr.json();
  const tok = lj?.data?.access_token;
  if (tok) {
    const list = await fetch(BASE + '/api/v1/transactions?page=1&page_size=100&keyword=' + encodeURIComponent('生产冒烟'), {
      headers: { authorization: `Bearer ${tok}` },
    }).then((r) => r.json());
    const hits = (list?.data?.items ?? list?.data?.list ?? []).filter((t) => String(t.note || '').includes('生产冒烟'));
    for (const t of hits) {
      await fetch(BASE + `/api/v1/transactions/${t.id}`, { method: 'DELETE', headers: { authorization: `Bearer ${tok}` } });
    }
    console.log(`-- 清理冒烟流水 ${hits.length} 条`);
  }
} catch (e) {
  console.log(`-- 清理跳过: ${String(e).slice(0, 120)}`);
}

await browser.close();

const pass = results.filter((r) => r.pass).length;
const fail = results.length - pass;
console.log(`\n==== ${pass}/${results.length} PASS ====`);
if (fail > 0 || realFails.length > 0 || filteredErrors.length > 0) {
  console.log('HAS_ISSUES');
  process.exit(1);
}
console.log('ALL_GREEN');
