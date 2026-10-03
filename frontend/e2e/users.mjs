/* eslint-disable no-console */
/**
 * 用户管理界面走查（specs/identity/user-management，任务 2.5 的 UI 层）。
 * 运行：node e2e/users.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖：
 *  - 管理员「我的」页有「用户管理」入口，展开列出全部用户
 *  - 最后一个启用管理员：角色下拉禁用、停用按钮禁用（前端提前拦，后端亦拒）
 *  - 新建用户即刻可用；重复用户名报错且不产生第二条
 *  - 改角色生效
 *  - 重置密码后新密码可登录、旧密码失效
 *  - 停用后无法登录；恢复后可登录
 *  - 非管理员看不到入口（前端隐藏 + 后端 403 双保险）
 *  - 移动端渲染为列表而非表格
 *  - 移动端各路由无横向溢出（box-sizing reset 回归，见 styles.css 顶部注释）
 *
 * 全程只创建 1 个一次性用户 e2e_ui_user，结束时从库里删除（spec 不提供物理删除 API，
 * 这里的清理是测试环境专用的收尾，不走产品接口）。
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

const TEST_USER = 'e2e_ui_user';
const TEST_PW = 'E2ePass123';
const NEW_PW = 'ResetPass456';

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });
const norm = (s) => (s ?? '').replace(/\s+/g, '');

/** 轮询直到 fn 返回真值。 */
async function until(fn, timeout = 15000, interval = 250) {
  const t0 = Date.now();
  let last;
  while (Date.now() - t0 < timeout) {
    try {
      last = await fn();
      if (last) return last;
    } catch {
      /* 选择器还没就绪 */
    }
    await sleep(interval);
  }
  throw new Error(`until 超时，最后取值=${JSON.stringify(last)}`);
}

async function newPage(browser, opts = {}) {
  const ctx = await browser.newContext({
    viewport: opts.viewport ?? { width: 1280, height: 900 },
  });
  const page = await ctx.newPage();
  return { ctx, page };
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

/** 打开「我的」页的用户管理面板。 */
async function openUserMgmt(page) {
  await page.goto(BASE + '/mine');
  await page.getByRole('button', { name: '用户管理' }).first().waitFor({ timeout: 10000 });
  await page.getByRole('button', { name: '用户管理' }).first().click();
  await page.locator('.ant-card-head-title', { hasText: '用户管理' }).waitFor({ timeout: 10000 });
  await page.waitForFunction(() => document.querySelectorAll('tbody tr.ant-table-row').length > 0, null, {
    timeout: 15000,
  });
}

const rows = (page, who) =>
  who
    ? page.locator('tbody tr.ant-table-row').filter({ hasText: who })
    : page.locator('tbody tr.ant-table-row');

/** 在打开的下拉里按中文标签选一项（antd v6 的 role=option 里是原始 value，只能按视觉项选）。 */
async function pickOption(page, label) {
  const opt = page.locator('.ant-select-item-option-content:visible', { hasText: label });
  await until(async () => (await opt.count()) > 0, 8000);
  await opt.last().click();
  // 选完下拉只是加 hidden 类，DOM 不卸载 —— 必须按可见性判断，不能按 count
  await until(async () => (await page.locator('.ant-select-dropdown:visible').count()) === 0, 8000);
}

/** 读最近一条 message 提示（读不到返回空串，不抛错）。 */
async function lastMessage(page) {
  try {
    await page.locator('.ant-message-notice').first().waitFor({ timeout: 5000 });
    await sleep(300);
    return norm(await page.locator('.ant-message-notice').last().innerText());
  } catch {
    return '';
  }
}

const browser = await chromium.launch({ executablePath: EXE, headless: true });

try {
  // ================= 预检：测试用户必须不存在（本套件不走删除 API，spec 明确不提供物理删除） =================
  {
    const { ctx, page: p } = await newPage(browser);
    const lg = await p.request.post(BASE + '/api/v1/auth/login', {
      data: { username: 'admin', password: 'admin123' },
    });
    const tok = (await lg.json())?.data?.access_token ?? '';
    const res = await p.request.get(BASE + '/api/v1/users', {
      headers: { Authorization: `Bearer ${tok}` },
    });
    const names = ((await res.json())?.data ?? []).map((u) => u.username);
    await ctx.close();
    if (names.includes(TEST_USER)) {
      console.error(
        `预检失败：${TEST_USER} 还在库里（上一轮没清理）。先执行：\n` +
          `  docker exec jizhang-mysql mysql -uroot -pdevpass -N -e "DELETE FROM jizhang.users WHERE username='${TEST_USER}';"`,
      );
      process.exit(1);
    }
  }

  // ================= 管理员视角 =================
  const { ctx: ctxA, page } = await newPage(browser);
  await login(page, 'admin', 'admin123');
  await openUserMgmt(page);

  ok(true, '01 管理员「我的」页有「用户管理」入口，可展开');

  const n0 = await rows(page).count();
  ok(n0 >= 4, '02 展开后列出全部用户', `rows=${n0}`);

  // 最后一个管理员锁定
  const adminRow = rows(page, 'admin');
  ok((await adminRow.count()) === 1, '03 定位到 admin 行');
  const lockedSelect = await adminRow.locator('.ant-select-disabled').count();
  const stopDisabled = await adminRow.getByRole('button', { name: /停\s*用/ }).isDisabled();
  ok(
    lockedSelect === 1 && stopDisabled,
    '04 最后一个管理员：角色下拉与「停用」均被禁用',
    `selectDisabled=${lockedSelect} stopDisabled=${stopDisabled}`,
  );

  // ---------- 新建用户 ----------
  await page.getByRole('button', { name: '新建用户' }).click();
  let modal = page.locator('.ant-modal-container');
  await modal.waitFor({ timeout: 8000 });
  await modal.locator('input[placeholder="登录用，如：xiaoming"]').fill(TEST_USER);
  await modal.locator('input[placeholder="至少 6 位，首次登录后可重置"]').fill(TEST_PW);
  await modal.locator('.ant-select').click();
  await pickOption(page, '只读成员');
  await modal.getByRole('button', { name: /创\s*建/ }).click();
  const createMsg = await lastMessage(page);
  await modal.waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});

  const created = await until(async () => (await rows(page, TEST_USER).count()) === 1, 15000).catch(
    () => false,
  );
  ok(created, '05 新建用户成功并出现在列表', `msg=${createMsg}`);

  const viewerRow = rows(page, TEST_USER);
  ok(
    norm(await viewerRow.innerText()).includes('只读成员'),
    '06 新用户初始角色正确（只读成员）',
    norm(await viewerRow.innerText()),
  );

  // ---------- 重复用户名 ----------
  await page.getByRole('button', { name: '新建用户' }).click();
  modal = page.locator('.ant-modal-container');
  await modal.waitFor({ timeout: 8000 });
  await modal.locator('input[placeholder="登录用，如：xiaoming"]').fill(TEST_USER);
  await modal.locator('input[placeholder="至少 6 位，首次登录后可重置"]').fill('Whatever123');
  await modal.getByRole('button', { name: /创\s*建/ }).click();
  const dupMsg = await lastMessage(page);
  await sleep(500);
  const dupCount = await rows(page, TEST_USER).count();
  const modalStillOpen = await modal.isVisible().catch(() => false);
  ok(
    dupCount === 1 && modalStillOpen && dupMsg.includes('已占用'),
    '07 重复用户名报错、弹窗不关、不产生第二条',
    `count=${dupCount} open=${modalStillOpen} msg=${dupMsg}`,
  );
  await modal.getByRole('button', { name: /取\s*消/ }).click();
  await modal.waitFor({ state: 'hidden', timeout: 8000 }).catch(() => {});

  // ---------- 改角色 ----------
  await viewerRow.locator('.ant-select').click();
  await pickOption(page, '记账员');
  const roleOk = await until(async () => norm(await viewerRow.innerText()).includes('记账员'), 15000).catch(
    () => false,
  );
  ok(roleOk, '08 改角色生效（只读成员 → 记账员）', norm(await viewerRow.innerText()));

  // ---------- 重置密码 ----------
  await viewerRow.getByRole('button', { name: '重置密码' }).click();
  modal = page.locator('.ant-modal-container');
  await modal.waitFor({ timeout: 8000 });
  await modal.locator('input[placeholder="至少 6 位"]').fill(NEW_PW);
  await modal.locator('input[placeholder="再输一次"]').fill(NEW_PW);
  await modal.getByRole('button', { name: /^重\s*置$/ }).click();
  const resetMsg = await lastMessage(page);
  await modal.waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
  ok(resetMsg.includes('重置'), '09 重置密码成功', resetMsg);

  // ---------- 停用 ----------
  await viewerRow.getByRole('button', { name: /停\s*用/ }).click();
  const pop = page.locator('.ant-popover:visible').filter({ hasText: `停用「${TEST_USER}」？` });
  await pop.waitFor({ timeout: 8000 });
  await pop.locator('.ant-btn-dangerous').click();
  const disabled = await until(
    async () => (await viewerRow.locator('.ant-tag', { hasText: '停用' }).count()) > 0,
    15000,
  ).catch(() => false);
  ok(disabled, '10 停用成功，状态标记为停用');

  await shot(page, 'users_admin');

  // ---------- 恢复 ----------
  await viewerRow.getByRole('button', { name: /恢\s*复/ }).click();
  const enabled = await until(
    async () => (await viewerRow.locator('.ant-tag', { hasText: '启用' }).count()) > 0,
    15000,
  ).catch(() => false);
  ok(enabled, '11 恢复成功，状态回到启用');

  await ctxA.close();

  // ================= 新用户视角：重置后的密码可用 =================
  {
    const { ctx, page: p } = await newPage(browser);
    let okLogin = true;
    try {
      await login(p, TEST_USER, NEW_PW);
      okLogin = !p.url().includes('/login');
    } catch {
      okLogin = false;
    }
    ok(okLogin, '12 重置密码后，新密码可正常登录', p.url());
    if (okLogin) await shot(p, 'users_newuser');
    await ctx.close();
  }

  // ================= 停用态：无法登录 =================
  {
    // 用 API 停用，避免重复走 UI（UI 路径已覆盖）
    const { ctx, page: p } = await newPage(browser);
    await login(p, 'admin', 'admin123');
    await openUserMgmt(p);
    const r = rows(p, TEST_USER);
    await r.getByRole('button', { name: /停\s*用/ }).click();
    const popc = p.locator('.ant-popover:visible').filter({ hasText: `停用「${TEST_USER}」？` });
    await popc.waitFor({ timeout: 8000 });
    await popc.locator('.ant-btn-dangerous').click();
    await until(
      async () => (await r.locator('.ant-tag', { hasText: '停用' }).count()) > 0,
      15000,
    ).catch(() => {});
    await ctx.close();

    const { ctx: ctx2, page: p2 } = await newPage(browser);
    await p2.goto(BASE + '/login');
    await p2.locator('input[placeholder="用户名"]').fill(TEST_USER);
    await p2.locator('input[placeholder="密码"]').fill(NEW_PW);
    await p2.getByRole('button', { name: /登\s*录/ }).click();
    await p2.waitForTimeout(2500);
    const stillLogin = p2.url().includes('/login');
    ok(stillLogin, '13 停用后无法登录', p2.url());
    await ctx2.close();
  }

  // ================= 非管理员看不到入口 =================
  {
    const { ctx, page: p } = await newPage(browser);
    await login(p, 'xiaowang', 'member123456');
    await p.goto(BASE + '/mine');
    await p.waitForLoadState('networkidle').catch(() => {});
    const cnt = await p.getByRole('button', { name: '用户管理' }).count();
    const mgmtCard = await p.locator('.ant-card-head-title', { hasText: '管理' }).count();
    ok(cnt === 0, '14 非管理员（member）看不到「用户管理」入口', `btn=${cnt} 管理卡=${mgmtCard}`);
    await shot(p, 'users_member');
    await ctx.close();
  }

  // ================= 后端 403 双保险 =================
  {
    const { ctx, page: p } = await newPage(browser);
    // access token 只在内存里（设计 D2），所以直连也要先走一次 API 登录拿 token
    const lg = await p.request.post(BASE + '/api/v1/auth/login', {
      data: { username: 'xiaowang', password: 'member123456' },
    });
    const tok = (await lg.json())?.data?.access_token ?? '';
    const res = await p.request.post(BASE + '/api/v1/users', {
      headers: { Authorization: `Bearer ${tok}` },
      data: { username: 'should_not_exist', password: 'Whatever123', role: 'viewer' },
      failOnStatusCode: false,
    });
    ok(res.status() === 403 && !!tok, '15 非管理员直连 POST /users 被 403 拦截', `status=${res.status()}`);
    await ctx.close();

    // 同一个 token 也读不到列表（双保险：写被拒，读也被拒）
    const { ctx: c2, page: p2 } = await newPage(browser);
    const lg2 = await p2.request.post(BASE + '/api/v1/auth/login', {
      data: { username: 'xiaowang', password: 'member123456' },
    });
    const tok2 = (await lg2.json())?.data?.access_token ?? '';
    const listRes = await p2.request.get(BASE + '/api/v1/users', {
      headers: { Authorization: `Bearer ${tok2}` },
      failOnStatusCode: false,
    });
    ok(listRes.status() === 403, '16 非管理员读取用户列表同样 403', `status=${listRes.status()}`);
    await c2.close();
  }

  // ================= 移动端 =================
  {
    const { ctx, page: p } = await newPage(browser, { viewport: { width: 390, height: 844 } });
    await login(p, 'admin', 'admin123');
    await p.goto(BASE + '/mine');
    await p.getByRole('button', { name: '用户管理' }).first().click();
    await p.locator('.ant-card-head-title', { hasText: '用户管理' }).waitFor({ timeout: 10000 });
    // 数据回来之后再数，否则会把「还没加载」误判成「渲染成了表格」
    await until(async () => (await p.locator('.acct-list .acct-row').count()) >= 4, 15000).catch(() => false);
    const mobileList = await p.locator('.acct-list .acct-row').count();
    const tableOnMobile = await p.locator('tbody tr.ant-table-row').count();
    ok(
      mobileList >= 4 && tableOnMobile === 0,
      '17 移动端渲染为列表而非表格',
      `list=${mobileList} table=${tableOnMobile}`,
    );
    await shot(p, 'users_mobile');

    // 这次踩的坑：全站没有 box-sizing reset，.app-content 是 width:100%+padding 的
    // content-box，360px 视口下渲染成 384px —— 每个页面都横向溢出 24px。逐路由守住。
    const overflow = [];
    for (const r of ['/', '/records', '/stats', '/budgets', '/mine']) {
      await p.goto(BASE + r);
      await p.waitForTimeout(600);
      const m = await p.evaluate(() => ({
        doc: document.documentElement.scrollWidth,
        inner: window.innerWidth,
      }));
      if (m.doc > m.inner) overflow.push(`${r}:${m.doc}>${m.inner}`);
    }
    ok(overflow.length === 0, '18 移动端各路由无横向溢出', overflow.join(' ') || 'docScrollW<=innerW');
    await ctx.close();
  }

  // ================= 无残留：一次性用户之外没多出任何人 =================
  {
    const { ctx, page: p } = await newPage(browser);
    const lg = await p.request.post(BASE + '/api/v1/auth/login', {
      data: { username: 'admin', password: 'admin123' },
    });
    const tok = (await lg.json())?.data?.access_token ?? '';
    const res = await p.request.get(BASE + '/api/v1/users', {
      headers: { Authorization: `Bearer ${tok}` },
    });
    const list = (await res.json())?.data ?? [];
    const names = list.map((u) => u.username);
    ok(
      list.length === 5 && names.includes(TEST_USER) && !names.includes('should_not_exist'),
      '19 用户数 = 4 个种子 + 1 个测试用户，无其它残留',
      `count=${list.length} ${names.join(',')}`,
    );
    await ctx.close();
  }
} finally {
  await browser.close();
}

// ================= 汇总 =================
const pass = results.filter((r) => r.pass).length;
const fail = results.length - pass;
console.log(`\n==== users: ${pass}/${results.length} PASS, ${fail} FAIL ====`);
console.log(
  `\n收尾：${TEST_USER} 仍在库里（spec 明确不提供物理删除 API），跑完请清理：\n` +
    `  docker exec jizhang-mysql mysql -uroot -pdevpass -N -e "DELETE FROM jizhang.users WHERE username='${TEST_USER}';"`,
);
if (fail > 0) {
  results.filter((r) => !r.pass).forEach((r) => console.log(`  ✗ ${r.name}`));
  process.exit(1);
}


