import { currentAuthToken, latestBearer, trackPageAuth } from './support/auth-tokens.mjs';
/* eslint-disable no-console */
/**
 * improve-mobile-web-interaction 阶段 2 走查（QueryState 三态 / 删除防重 /
 * 内联校验 / 弹层挂载 / 滚动复位 / 语义化键盘可达 / hero 不裁切 / 登录消息）。
 * 运行：node e2e/interaction_feedback.mjs   （需 dev server :5600 与后端 :8080 已启动）
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

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond, extra });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });

/** 弹层是否贴合触发器：在触发器下方（顶边距触发器底 ≤ tol）或上方翻转（底边距触发器顶 ≤ tol）。 */
function adjacent(pop, anchor, tol) {
  if (!pop || !anchor) return false;
  const below = Math.abs(pop.y - (anchor.y + anchor.height)) <= tol;
  const above = Math.abs(anchor.y - (pop.y + pop.height)) <= tol;
  return below || above;
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

let TOKEN = '';
async function apiLogin() {
  const _uiToken = currentAuthToken(BASE, 'admin');
  if (_uiToken) { TOKEN = _uiToken; return _uiToken; }
  if (TOKEN) return TOKEN;
  const r = await fetch(BASE + '/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ protocol_version: 2, username: 'admin', password: 'admin123' }),
  });
  const j = await r.json();
  TOKEN = j.data?.access_token ?? '';
  if (!TOKEN) throw new Error('API login failed: ' + JSON.stringify(j));
  return TOKEN;
}
const api = (p, init = {}) =>
  fetch(BASE + '/api/v1' + p, {
    ...init,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${latestBearer(BASE, TOKEN)}`, ...(init.headers ?? {}) },
  }).then(async (r) => ({ status: r.status, json: await r.json().catch(() => null) }));

function localToday() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

let SEED_CAT = 0;
let SEED_ACCT = 0;
async function seedBase() {
  await apiLogin();
  if (SEED_CAT && SEED_ACCT) return;
  const cats = await api('/categories');
  const roots = (cats.json?.data ?? []).flatMap((w) => w.children ?? []);
  const expRoot = roots.find((r) => r.kind === 'expense');
  SEED_CAT = expRoot?.children?.[0]?.id ?? expRoot?.id ?? 0;
  const accts = await api('/accounts');
  SEED_ACCT = accts.json?.data?.[0]?.id ?? 0;
  if (!SEED_CAT || !SEED_ACCT) throw new Error('seed: no category/account');
}

/** 造一笔流水，返回其 id。 */
async function seedTxn(note) {
  await seedBase();
  const { status, json } = await api('/transactions', {
    method: 'POST',
    body: JSON.stringify({
      kind: 'expense',
      amount: '12.34',
      account_id: SEED_ACCT,
      category_id: SEED_CAT,
      entry_date: localToday(),
      note,
    }),
  });
  if (status >= 400) throw new Error('seed txn failed: ' + status);
  return json?.data?.id ?? 0;
}

/** 取真实流水列表响应体（空态测试用真实结构覆盖 items）。 */
async function realTxnList() {
  const { json } = await api('/transactions?page=1&page_size=20');
  return json;
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  await apiLogin();
  await seedTxn(`fb-seed-${Date.now()}`);

  // ================= 2.2 统计页错误态 + 重试 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    let fail = true;
    await page.route('**/api/v1/stats/category*', async (route) => {
      if (fail) {
        await route.fulfill({ status: 500, contentType: 'application/json', body: '{"code":500,"message":"e2e 模拟失败"}' });
      } else {
        await route.continue();
      }
    });
    await page.goto(BASE + '/stats');
    const card = page.locator('.panel').filter({ hasText: '分类占比' }).first();
    await card.locator('.query-state .ant-alert-error').waitFor({ timeout: 20000 });
    ok(true, '2.2 统计接口 500 → 分类占比卡出现错误态');
    const retry = card.locator('.query-state .ant-btn', { hasText: /重\s*试/ });
    ok((await retry.count()) === 1, '2.2 错误态带「重试」按钮');
    const emptyShown = await card.getByText('区间内无数据').count();
    ok(emptyShown === 0, '2.2 错误不落入空态「区间内无数据」', `count=${emptyShown}`);
    fail = false;
    await retry.click();
    await card.locator('canvas').first().waitFor({ timeout: 20000 });
    ok(true, '2.2 点重试后图表数据渲染（canvas 出现）');
    await shot(page, 'fb-stats-retry');
    await ctx.close();
  }

  // ================= 2.3 首页最近流水错误态 + 重试 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    let fail = true;
    await page.route('**/api/v1/transactions*', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      if (fail) {
        await route.fulfill({ status: 500, contentType: 'application/json', body: '{"code":500,"message":"e2e 模拟失败"}' });
      } else {
        await route.continue();
      }
    });
    await page.goto(BASE + '/');
    const list = page.locator('.recent-list');
    await list.locator('.query-state .ant-alert-error').waitFor({ timeout: 20000 });
    ok(true, '2.3 首页流水接口 500 → 最近流水错误态');
    const retry = list.locator('.query-state .ant-btn', { hasText: /重\s*试/ });
    ok((await retry.count()) === 1, '2.3 错误态带「重试」按钮');
    const emptyShown = await list.getByText('还没有流水，记一笔吧').count();
    ok(emptyShown === 0, '2.3 错误不落入空态', `count=${emptyShown}`);
    fail = false;
    await retry.click();
    await list.locator('.recent-row').first().waitFor({ timeout: 20000 });
    ok(true, '2.3 点重试后最近流水恢复');
    await shot(page, 'fb-dashboard-retry');
    await ctx.close();
  }

  // ================= 2.4 流水页：错误态 / 空态 / 骨架 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    let mode = 'ok'; // ok | fail | hold
    let releaseHold = () => {};
    const holdGate = new Promise((r) => (releaseHold = r));
    await page.route('**/api/v1/transactions*', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      if (mode === 'fail') {
        return route.fulfill({ status: 500, contentType: 'application/json', body: '{"code":500,"message":"e2e 模拟失败"}' });
      }
      if (mode === 'hold') {
        await holdGate;
      }
      return route.continue();
    });
    await login(page, 'admin', 'admin123');

    // 骨架：hold 期间显示加载指示（桌面 Table loading=Spin / QueryState 骨架）
    mode = 'hold';
    await page.goto(BASE + '/records');
    await page.locator('.ant-spin, .ant-skeleton').first().waitFor({ timeout: 15000 });
    ok(true, '2.4 请求进行中显示加载指示');
    mode = 'ok';
    releaseHold();
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 20000 });

    // 错误态
    mode = 'fail';
    await page.goto(BASE + '/records');
    await page.locator('.query-state .ant-alert-error').waitFor({ timeout: 20000 });
    const emptyShown = await page.getByText('暂无数据', { exact: false }).count();
    ok(emptyShown === 0, '2.4 断网打开流水页 → 错误态而非「暂无数据」', `count=${emptyShown}`);
    await shot(page, 'fb-records-error');
    mode = 'ok';
    await page.locator('.query-state .ant-btn', { hasText: /重\s*试/ }).click();
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 20000 });
    ok(true, '2.4 点重试后列表恢复');

    // 真实空数据 → 空态而非错误
    await page.unroute('**/api/v1/transactions*');
    const real = await realTxnList();
    const emptyBody = JSON.stringify({ ...real, data: { ...(real?.data ?? {}), items: [], total: 0 } });
    await page.route('**/api/v1/transactions*', (route) =>
      route.request().method() === 'GET'
        ? route.fulfill({ status: 200, contentType: 'application/json', body: emptyBody })
        : route.continue(),
    );
    await page.goto(BASE + '/records');
    await page.locator('.ant-empty-description').filter({ hasText: '暂无数据' }).first().waitFor({ timeout: 20000 });
    ok((await page.locator('.query-state').count()) === 0, '2.4 真实空数据显示空态而非错误');
    await ctx.close();
  }

  // ================= 2.5 删除失败保留列表 + 连点确认仅 1 个 DELETE =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    const target = `fb-del-${Date.now()}`;
    const txnId = await seedTxn(target);

    let mode = 'fail'; // fail | hold | ok
    let holdRoutes = [];
    let deleteCount = 0;
    await page.route('**/api/v1/transactions/**', async (route) => {
      if (route.request().method() !== 'DELETE') return route.continue();
      deleteCount++;
      if (mode === 'fail') {
        return route.fulfill({ status: 500, contentType: 'application/json', body: '{"code":500,"message":"e2e 模拟删除失败"}' });
      }
      if (mode === 'hold') {
        holdRoutes.push(route);
        return undefined;
      }
      return route.continue();
    });
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    const row = page.locator(`.ant-table-tbody tr.ant-table-row:has-text("${target}")`);
    await row.first().waitFor({ timeout: 20000 });

    // 失败：列表保留 + 错误消息
    await row.first().hover();
    await row.first().getByRole('button', { name: '删除' }).click();
    await page.locator('.ant-popover').getByRole('button', { name: /确\s*定/ }).click();
    await page.locator('.ant-message-error').first().waitFor({ timeout: 10000 });
    ok(true, '2.5 删除失败出现错误消息');
    ok((await page.locator(`.ant-table-tbody tr.ant-table-row:has-text("${target}")`).count()) > 0, '2.5 删除失败列表保持原状');
    ok(deleteCount === 1, '2.5 失败场景仅 1 个 DELETE', `${deleteCount}`);

    // 连点确认：hold 住 DELETE，先等确认按钮可见再双击 → 仅 1 个请求
    mode = 'hold';
    deleteCount = 0;
    await row.first().hover();
    await row.first().getByRole('button', { name: '删除' }).click();
    const okBtn = page.locator('.ant-popover').getByRole('button', { name: /确\s*定/ });
    await okBtn.waitFor({ state: 'visible', timeout: 5000 });
    await okBtn.click();
    // 请求挂起期间再连点：弹层已关 / 按钮 disabled，不应再发请求
    await sleep(150);
    await okBtn.click({ timeout: 1000, force: true }).catch(() => {});
    await sleep(650);
    ok(deleteCount === 1, '2.5 连点确认仅发出 1 个 DELETE', `${deleteCount}`);
    const loadingNow = holdRoutes.length === 1;
    ok(loadingNow, '2.5 确认进入进行中状态（请求挂起中）', `pending=${holdRoutes.length}`);
    mode = 'ok';
    for (const r of holdRoutes) {
      await r.fulfill({ status: 200, contentType: 'application/json', body: '{"code":0,"data":null}' }).catch(() => {});
    }
    // 上一步是假响应（后端未真删），这里走真删验证「成功后行消失」
    await sleep(800);
    await row.first().hover();
    await row.first().getByRole('button', { name: '删除' }).click();
    await page.locator('.ant-popover').getByRole('button', { name: /确\s*定/ }).click();
    await page.locator(`.ant-table-tbody tr.ant-table-row:has-text("${target}")`).first().waitFor({ state: 'detached', timeout: 15000 }).catch(() => {});
    ok((await page.locator(`.ant-table-tbody tr.ant-table-row:has-text("${target}")`).count()) === 0, '2.5 成功后该行从列表消失');
    // 清理：兜底删除残留
    await api(`/transactions/${txnId}`, { method: 'DELETE' });
    await ctx.close();
  }

  // ================= 2.6/2.7 记账表单内联校验 + 弹层挂载 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.locator('.page-head').getByRole('button', { name: '快捷记账' }).click();
    const m = page.locator('.ant-modal-container', { hasText: '记一笔' }).first();
    await m.waitFor({ state: 'visible' });
    await sleep(500);

    // 2.6 空表单提交 → 内联 Alert + 金额错误态，Modal 不关
    await m.locator('.ant-modal-footer .ant-btn-primary').click();
    await m.locator('.txn-form-error.ant-alert-error').waitFor({ timeout: 5000 });
    ok(true, '2.6 校验失败出现表单内错误 Alert');
    ok((await m.locator('.amount-row.amount-error').count()) === 1, '2.6 金额非法时金额字段有内联错误态');
    ok(await m.isVisible(), '2.6 校验失败 Modal 不关闭');
    ok((await page.locator('.ant-message-notice').count()) === 0, '2.6 校验失败不弹全局 message');

    // 金额合法但未选账户 → 账户 Select 错误态
    await m.locator('.amount-input').fill('5.20');
    await m.locator('.cat-leaves .ant-tag-checkable').first().click();
    await m.locator('.ant-modal-footer .ant-btn-primary').click();
    await m.getByText('请选择账户').first().waitFor({ timeout: 5000 });
    ok((await m.locator('.ant-select-status-error').count()) >= 1, '2.6 未选账户时账户 Select status=error');
    ok((await m.locator('.amount-row.amount-error').count()) === 0, '2.6 金额修复后金额错误态消失');
    ok(await m.isVisible(), '2.6 未选账户提交 Modal 仍不关闭');
    // 注意:此处不能用 fullPage 截图 —— Chromium 整页截图会临时改视口高度,触发断点重算,
    // AppShell 在桌面/移动形态间重挂 Outlet,把打开的 Modal 一并卸载(用视口截图代替)。
    await page.screenshot({ path: path.join(SHOTS, 'fb-txn-inline-error.png') });

    // 2.7 弹层挂载：滚动表单到中部，打开账户下拉与日期面板 → 面板在 Modal 内且贴合触发器
    await page.evaluate(() => {
      const bodies = [...document.querySelectorAll('.ant-modal-container .ant-modal-body')];
      const b = bodies.find((el) => el.getBoundingClientRect().height > 0) ?? bodies[0];
      if (b) b.scrollTop = b.scrollHeight / 2;
    });
    await sleep(300);
    const accountSelect = page.locator('.ant-modal-container:visible .ant-select').last();
    await accountSelect.click();
    const dropdown = page.locator('.ant-select-dropdown:visible').first();
    await dropdown.waitFor({ timeout: 5000 });
    const inModal = await dropdown.evaluate((el) => !!el.closest('.ant-modal'));
    ok(inModal, '2.7 账户下拉挂载在 Modal 容器内（随内容滚动）');
    const selBox = await accountSelect.boundingBox();
    const ddBox = await dropdown.boundingBox();
    const nearSel = selBox && ddBox && Math.abs(ddBox.x - selBox.x) < 80 && adjacent(ddBox, selBox, 80);
    ok(nearSel, '2.7 账户下拉贴合触发器（上/下翻转均可）', JSON.stringify({ sel: selBox, dd: ddBox }));
    await page.keyboard.press('Escape');
    await sleep(300);
    const picker = page.locator('.ant-modal-container:visible .ant-picker').first();
    await picker.scrollIntoViewIfNeeded();
    await picker.click();
    const panel = page.locator('.ant-picker-dropdown:visible').first();
    await panel.waitFor({ timeout: 5000 });
    const panelInModal = await panel.evaluate((el) => !!el.closest('.ant-modal'));
    ok(panelInModal, '2.7 日期面板挂载在 Modal 容器内');
    const pkBox = await picker.boundingBox();
    const pnBox = await panel.boundingBox();
    const nearPk = pkBox && pnBox && Math.abs(pnBox.x - pkBox.x) < 80 && adjacent(pnBox, pkBox, 120);
    ok(nearPk, '2.7 日期面板贴合触发器（上/下翻转均可）', JSON.stringify({ pk: pkBox, pn: pnBox }));
    await page.keyboard.press('Escape');

    // 收尾：补齐字段记一笔（顺带覆盖正常提交路径）
    await accountSelect.click();
    await page.locator('.ant-select-dropdown:visible .ant-select-item').first().click();
    await m.locator('.ant-modal-footer .ant-btn-primary').click();
    await m.waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    ok(!(await m.isVisible()), '2.6 补齐字段后提交成功、Modal 关闭');
    await ctx.close();
  }

  // ================= 2.8 路由滚动复位瞬时 + 定位跳转仍工作 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 20000 });
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await sleep(300);
    const scrolled = await page.evaluate(() => window.scrollY);
    ok(scrolled > 200, '2.8 流水页已滚动到底部', `scrollY=${scrolled}`);
    await page.locator('.app-sider').getByRole('menuitem', { name: '统计' }).click();
    await page.waitForURL('**/stats', { timeout: 10000 });
    // 瞬时复位：URL 变更后极短时间内 scrollY 必须归零（平滑滚动会留下一段中间值）
    let y = -1;
    const t0 = Date.now();
    while (Date.now() - t0 < 1500) {
      y = await page.evaluate(() => window.scrollY);
      if (y === 0) break;
      await sleep(20);
    }
    ok(y === 0 && Date.now() - t0 < 400, '2.8 切统计页滚动立即复位（无平滑中间态）', `scrollY=${y} after ${Date.now() - t0}ms`);

    // 定位跳转仍平滑居中并闪烁
    await page.goto(BASE + '/');
    await page.locator('.recent-row.clickable').first().waitFor({ timeout: 20000 });
    await page.locator('.recent-row.clickable').first().click();
    await page.waitForURL('**/records*', { timeout: 10000 });
    const flashed = await page.locator('.flash').first().waitFor({ state: 'attached', timeout: 8000 }).then(() => true).catch(() => false);
    ok(flashed, '2.8 首页点流水定位仍触发闪烁');
    await ctx.close();
  }

  // ================= 2.9 键盘可达 + 焦点环（账户行） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/accounts');
    await page.locator('.acct-main').first().waitFor({ timeout: 20000 });
    // Tab 循环直到焦点落在账户行
    let reached = false;
    for (let i = 0; i < 40 && !reached; i++) {
      await page.keyboard.press('Tab');
      reached = await page.evaluate(() => document.activeElement?.classList?.contains('acct-main') ?? false);
    }
    ok(reached, '2.9 Tab 可达账户行（role=button tabindex=0）');
    const outline = await page.evaluate(() => {
      const el = document.activeElement;
      return el ? getComputedStyle(el).outlineStyle : 'none';
    });
    ok(outline !== 'none', '2.9 焦点行有可见焦点环', `outline=${outline}`);
    await page.keyboard.press('Enter');
    await page.locator('.ant-modal').getByText('编辑账户').waitFor({ timeout: 5000 }).catch(() => {});
    ok((await page.locator('.ant-modal').getByText('编辑账户').count()) > 0, '2.9 Enter 触发账户编辑');
    await shot(page, 'fb-keyboard-acct');
    await ctx.close();
  }

  // ================= 2.9 已退标签键盘可达（造一笔退款） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await ctx.newPage();
    await seedBase();
    const srcId = await seedTxn(`fb-ref-src-${Date.now()}`);
    const refund = await api('/transactions', {
      method: 'POST',
      body: JSON.stringify({
        kind: 'refund',
        amount: '5.00',
        account_id: SEED_ACCT,
        related_txn_id: srcId,
        entry_date: localToday(),
        note: `fb-refund-${Date.now()}`,
      }),
    });
    if (refund.status >= 400) {
      ok(false, '2.9 造退款流水失败', JSON.stringify(refund.json));
    } else {
      await login(page, 'admin', 'admin123');
      await page.goto(BASE + '/records');
      const refTag = page.locator('.refunded-tag').first();
      await refTag.waitFor({ timeout: 20000 }).catch(() => {});
      if ((await refTag.count()) > 0) {
        const role = await refTag.getAttribute('role');
        const tabindex = await refTag.getAttribute('tabindex');
        ok(role === 'button' && tabindex === '0', '2.9 已退标签带 role=button/tabindex=0', `role=${role} tabindex=${tabindex}`);
        await refTag.focus();
        await page.keyboard.press('Enter');
        // locateRefunds 走筛选态（patch related_txn_id），不是 URL 变更：断言「关联退款」筛选 Tag 出现
        const filterTag = page.getByText(/关联退款 #\d+/).first();
        await filterTag.waitFor({ timeout: 8000 }).catch(() => {});
        ok((await filterTag.count()) > 0, '2.9 Enter 触发已退标签定位关联退款（筛选 Tag 出现）');
      } else {
        ok(false, '2.9 已退标签未出现在流水页');
      }
      await api(`/transactions/${srcId}`, { method: 'DELETE' }).catch(() => {});
    }
    await ctx.close();
  }

  // ================= 2.10 hero 大额结余不裁切 + 指针仅鼠标 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 360, height: 780 }, isMobile: true, hasTouch: true });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.route('**/api/v1/stats/compare*', async (route) => {
      const res = await route.fetch();
      const j = await res.json();
      j.data.current.balance = '1234567.89';
      await route.fulfill({ response: res, json: j });
    });
    await page.goto(BASE + '/');
    await page.locator('.hero-value').getByText('1,234,567.89', { exact: false }).waitFor({ timeout: 20000 });
    const fit = await page.evaluate(() => {
      const el = document.querySelector('.hero-value');
      const r = el.getBoundingClientRect();
      return { scrollW: el.scrollWidth, clientW: el.clientWidth, right: r.right, vw: window.innerWidth, text: el.textContent };
    });
    ok(fit.scrollW <= fit.clientW + 1, '2.10 360px 大额结余不溢出容器（无裁切）', JSON.stringify(fit));
    ok(fit.right <= fit.vw + 1, '2.10 金额整体在视口内', `right=${fit.right} vw=${fit.vw}`);
    await shot(page, 'fb-hero-360');
    // 指针装饰：touch 不写 CSS 变量，mouse 写入
    const ptr = await page.evaluate(() => {
      const hero = document.querySelector('.hero');
      hero.style.removeProperty('--mx');
      hero.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'touch', clientX: 100, clientY: 100, bubbles: true }));
      const touch = hero.style.getPropertyValue('--mx');
      hero.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'mouse', clientX: 120, clientY: 60, bubbles: true }));
      const mouse = hero.style.getPropertyValue('--mx');
      return { touch, mouse };
    });
    ok(ptr.touch === '' && ptr.mouse !== '', '2.10 触摸不写指针变量、鼠标写入', JSON.stringify(ptr));
    await ctx.close();
  }

  // ================= 2.11 登录失败消息走 AntApp 容器（暗色截图） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await ctx.newPage();
    const consoleWarns = [];
    page.on('console', (m) => {
      if (m.type() === 'warning' || m.type() === 'error') consoleWarns.push(m.text());
    });
    await page.addInitScript(() => localStorage.setItem('jizhang-theme', 'dark'));
    await page.goto(BASE + '/login');
    await page.locator('input[placeholder="用户名"]').fill('admin');
    await page.locator('input[placeholder="密码"]').fill('wrong-pass');
    await page.getByRole('button', { name: /登\s*录/ }).click();
    await page.locator('.ant-message-error').first().waitFor({ timeout: 10000 });
    // App.useApp()（上下文实例）：无静态 message 的主题警告，且消息样式吃暗色算法（深底）
    const staticWarn = consoleWarns.some((t) => t.includes('Static function') || t.includes('静态'));
    ok(!staticWarn, '2.11 无静态 message 主题警告（走 App.useApp 上下文实例）', consoleWarns.slice(0, 2).join(' | '));
    const noticeBg = await page
      .locator('.ant-message-notice')
      .first()
      .evaluate((el) => getComputedStyle(el).backgroundColor);
    const rgb = noticeBg.match(/\d+/g)?.map(Number) ?? [255, 255, 255];
    ok(rgb[0] + rgb[1] + rgb[2] < 400, '2.11 消息样式吃暗色主题（深色底而非白底）', noticeBg);
    const theme = await page.evaluate(() => document.documentElement.dataset.theme);
    ok(theme === 'dark', '2.11 暗色主题下登录页生效', `theme=${theme}`);
    await shot(page, 'fb-login-dark');
    await ctx.close();
  }

  await browser.close();

  const failed = results.filter((r) => !r.pass);
  console.log(`\n==== ${results.length - failed.length}/${results.length} PASS ====`);
  if (failed.length) {
    for (const f of failed) console.log(`  FAIL: ${f.name} ${f.extra}`);
    process.exitCode = 1;
  }
}

main().catch((e) => {
  console.error('FATAL', e);
  process.exit(1);
});
