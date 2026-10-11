/* eslint-disable no-console */
/**
 * upgrade-mobile-web-interaction 前端 e2e（随任务组追加断言）。
 * 运行：node e2e/interaction_upgrade.mjs   （需 dev server :5600 与后端 :8080 已启动）
 * 沿用 walkthrough.mjs 的 ok/login/shot 结构。
 *
 * 当前覆盖：组 1 流水筛选 URL 持久化（1.1–1.3）；组 2 移动端列表加载（2.2–2.4）。
 * 组 2 需要 3 页数据：脚本自造 30 笔（共 47 笔 → 20/20/7），结束时删除。
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

/** SPA 同文档导航没有 load 事件，用轮询代替 waitForURL。 */
async function until(fn, ms = 10000, every = 150) {
  const end = Date.now() + ms;
  for (;;) {
    let v = false;
    try {
      v = await fn();
    } catch {
      /* 轮询期间 DOM/URL 抖动，继续等 */
    }
    if (v) return true;
    if (Date.now() > end) return false;
    await sleep(every);
  }
}

const pathnameOf = (page) => new URL(page.url()).pathname;
const searchOf = (page) => new URL(page.url()).search;

/** 点击 Segmented 项（scope 选择器限定所属控件）。 */
async function clickSeg(page, scopeSel, label) {
  const scope = scopeSel ? page.locator(scopeSel) : page.locator('body');
  const item = scope.locator('.ant-segmented-item', { hasText: label }).first();
  try {
    await item.waitFor({ state: 'visible', timeout: 5000 });
  } catch {
    await scope.getByText(label, { exact: true }).first().click();
    return;
  }
  await item.click();
}

/** .filter-bar 中「类型」Segmented 当前选中项文案（直接子元素，避开 .period-picker 的预设 Segmented）。 */
async function segSelected(page, label) {
  const t = await page
    .locator('.filter-bar > .ant-segmented .ant-segmented-item-selected')
    .first()
    .innerText()
    .catch(() => '');
  return t.replace(/\s/g, '') === label;
}

/** 等某条件的流水列表请求发出（证明切页/后退后按该条件重新拉列表）。 */
const txnResp = (page, kind) =>
  page
    .waitForResponse(
      (r) =>
        r.url().includes('/transactions') &&
        r.url().includes(`kind=${kind}`) &&
        r.request().method() === 'GET',
      { timeout: 10000 },
    )
    .catch(() => null);

/** page-head eyebrow 里的「N 笔」总数（证明列表按条件刷新）。 */
async function eyebrowTotal(page) {
  const t = await page
    .locator('.page-head .eyebrow')
    .first()
    .innerText()
    .catch(() => '');
  const m = t.match(/([\d,]+)\s*笔/);
  return m ? m[1] : '';
}

/** 点击分页控件第 n 页。 */
async function clickPager(page, n) {
  await page.locator(`.ant-pagination-item-${n}`).first().click();
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

/** 桌面侧栏菜单项（记一笔/流水/统计/我的）。 */
const siderItem = (page, label) => page.locator('.app-sider .ant-menu-item', { hasText: label }).first();

// ---- API 直连辅助（组 2 造数/清理） ----
const API = process.env.API ?? 'http://127.0.0.1:8080/api/v1';
let TOKEN = '';
async function apiLogin(u = 'admin', p = 'admin123') {
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username: u, password: p }),
  });
  const j = await r.json();
  if (j.code !== 0) throw new Error(j.message);
  TOKEN = j.data.access_token;
}
async function api(path, opts = {}) {
  const r = await fetch(API + path, {
    ...opts,
    headers: { ...(opts.headers ?? {}), authorization: `Bearer ${TOKEN}`, 'content-type': 'application/json' },
  });
  return { status: r.status, json: await r.json().catch(() => null) };
}
function localToday() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
/** 造 N 笔支出流水，返回 id 列表（组 2 分页/上拉/下拉需要 3 页数据）。 */
async function seedTxns(n, tag) {
  const cats = await api('/categories');
  const roots = (cats.json?.data ?? []).flatMap((w) => w.children ?? []);
  const expRoot = roots.find((r) => r.kind === 'expense');
  const catId = expRoot?.children?.[0]?.id ?? expRoot?.id ?? 0;
  const accts = await api('/accounts');
  const acctId = accts.json?.data?.[0]?.id ?? 0;
  if (!catId || !acctId) throw new Error('seed: no category/account');
  const ids = [];
  for (let i = 0; i < n; i++) {
    const { status, json } = await api('/transactions', {
      method: 'POST',
      body: JSON.stringify({
        kind: 'expense',
        amount: '1.23',
        account_id: acctId,
        category_id: catId,
        entry_date: localToday(),
        note: `${tag}-${i}`,
      }),
    });
    if (status >= 400) throw new Error('seed txn failed: ' + status);
    ids.push(json?.data?.id);
  }
  return ids;
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const desktop = { viewport: { width: 1440, height: 900 } };

  // ================= 组 1：流水筛选 URL 持久化（1.1–1.3，管理员，桌面） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });

    // 初始：默认值（本月区间、page=1、空条件）不写入 URL
    ok(searchOf(page) === '', '1.1 初始 URL 不含默认参数', searchOf(page) || '(empty)');

    // 记一笔（支出，成为首页最近流水首行 → 供 1.2 定位跳转用）
    const NOTE = `e2e-up-${Date.now()}`;
    await page.locator('.page-head').getByRole('button', { name: '记一笔' }).click();
    const modal = page.locator('.ant-modal-container:visible');
    await modal.waitFor({ state: 'visible' });
    await modal.locator('input[inputmode="decimal"]').fill('12.34');
    const leaves = modal.locator('.cat-leaves .ant-tag');
    if ((await leaves.count()) > 0) await leaves.first().click();
    // 第 0 个 .ant-select 是平台/商家 AutoComplete，账户选择器在第 1 位
    await modal.locator('.ant-select').nth(1).click();
    await page.locator('.ant-select-dropdown:visible').last().locator('.ant-select-item-option').first().click();
    await modal.locator('textarea').fill(NOTE);
    await modal.locator('.ant-modal-footer .ant-btn-primary').click();
    await page.getByText('已记一笔').waitFor({ timeout: 10000 });
    await sleep(900);

    // 1.1 改筛选 → 地址栏出现查询串，且只含非默认值
    await clickSeg(page, '.filter-bar', '支出');
    await until(() => searchOf(page) === '?kind=expense');
    ok(searchOf(page) === '?kind=expense', '1.1 改筛选后地址栏出现 kind=expense（无 from/to/page）', searchOf(page));

    // 1.1 刷新页面条件保持
    await page.reload();
    await page
      .locator('.ant-table-tbody tr.ant-table-row')
      .first()
      .waitFor({ timeout: 15000 })
      .catch(() => {});
    ok(searchOf(page) === '?kind=expense', '1.1 刷新后 URL 条件保持', searchOf(page));
    ok(await segSelected(page, '支出'), '1.1 刷新后筛选控件回显「支出」');
    const totalA = await eyebrowTotal(page);
    ok(totalA !== '', '1.1 列表按 URL 条件加载', `${totalA} 笔`);
    await shot(page, 'upgrade-01-records-url');

    // 1.3 后退 / 前进（等 B 条件数据落定再取总数，避免 placeholderData 沿用上一组的值）
    const respIncome = txnResp(page, 'income');
    await clickSeg(page, '.filter-bar', '收入');
    await until(() => searchOf(page) === '?kind=income');
    ok(!!(await respIncome), '1.3 改为 B 触发按 B 条件的列表请求', 'kind=income');
    await until(async () => (await eyebrowTotal(page)) !== totalA, 3000);
    const totalB = await eyebrowTotal(page);
    const respBack = txnResp(page, 'expense'); // 注册在 goBack 之前，捕获恢复 A 后的刷新请求
    await page.goBack();
    await until(() => searchOf(page) === '?kind=expense');
    // URL 先于控件回显：等控件落定再断言（直接复用等待结果，避免二次读取竞态）
    const segBack = await until(() => segSelected(page, '支出'), 8000);
    ok(
      searchOf(page) === '?kind=expense' && segBack,
      '1.3 后退恢复筛选 A（URL + 控件）',
      searchOf(page),
    );
    await until(async () => (await eyebrowTotal(page)) === totalA);
    ok(await eyebrowTotal(page) === totalA, '1.3 后退后列表按 A 刷新', `${await eyebrowTotal(page)} vs ${totalA}`);
    ok(!!(await respBack), '1.3 后退触发按 A 条件的列表请求', 'kind=expense');
    const respFwd = txnResp(page, 'income');
    await page.goForward();
    await until(() => searchOf(page) === '?kind=income');
    const segFwd = await until(() => segSelected(page, '收入'), 8000); // 同上：等控件回显
    ok(
      searchOf(page) === '?kind=income' && segFwd,
      '1.3 前进恢复筛选 B',
      searchOf(page),
    );
    await until(async () => (await eyebrowTotal(page)) === totalB);
    ok(await eyebrowTotal(page) === totalB, '1.3 前进后列表按 B 刷新', `${await eyebrowTotal(page)} vs ${totalB}`);
    ok(!!(await respFwd), '1.3 前进触发按 B 条件的列表请求', 'kind=income');
    // 回到 A（?kind=expense）供分享与定位测试
    await page.goBack();
    await until(() => searchOf(page) === '?kind=expense');

    // 1.3 分享链接：新页面打开恢复同一视图状态
    const shared = page.url();
    const p2 = await ctx.newPage();
    await p2.goto(shared);
    await p2.locator('.page-head').first().waitFor({ timeout: 15000 });
    await until(() => searchOf(p2) === '?kind=expense');
    ok(await segSelected(p2, '支出'), '1.3 分享链接新页面打开恢复筛选', p2.url());
    await shot(p2, 'upgrade-02-share-restore');
    await p2.close();

    // spec：切 Tab 后返回，筛选条件与页码完整保留
    await siderItem(page, '统计').click();
    await until(() => pathnameOf(page) === '/stats');
    await sleep(500);
    await siderItem(page, '流水').click();
    await until(() => pathnameOf(page) === '/records');
    ok(
      searchOf(page) === '?kind=expense' && (await segSelected(page, '支出')),
      '1.x 切 Tab 返回筛选完整保留',
      searchOf(page),
    );

    // 1.2 locateId 定位链路：首页点最近流水 → 高亮 + URL 筛选条件仍在
    await siderItem(page, '记一笔').click();
    await until(() => pathnameOf(page) === '/');
    // 点击刚才记的那笔（必然在 kind=expense 筛选内且为首页最近流水首行）
    const target = page.locator('.recent-row.clickable', { hasText: NOTE }).first();
    await target.waitFor({ timeout: 15000 });
    await target.click();
    await until(() => pathnameOf(page) === '/records');
    ok(searchOf(page) === '?kind=expense', '1.2 定位跳转后 URL 筛选条件仍在', searchOf(page));
    const flashed = await page
      .locator('.ant-table-tbody tr.flash, .txn-row.flash')
      .first()
      .waitFor({ state: 'attached', timeout: 10000 })
      .then(() => true)
      .catch(() => false);
    ok(flashed, '1.2 首页点最近流水 → 定位高亮生效');
    await shot(page, 'upgrade-03-locate');

    // 1.2 定位闪烁是一次性动画：1.8s 后行背景回到常态（state 已清，不会再次触发）
    await sleep(2400);
    const bg = await page.evaluate(() => {
      const f = document.querySelector('.flash');
      if (!f) return { gone: true };
      const cell = f.matches('tr') ? f.querySelector('td') : f;
      const other = document.querySelector('[data-txn-id]:not(.flash)');
      const ocell = other ? (other.matches('tr') ? other.querySelector('td') : other) : null;
      return {
        gone: false,
        flash: cell ? getComputedStyle(cell).backgroundColor : null,
        other: ocell ? getComputedStyle(ocell).backgroundColor : null,
      };
    });
    ok(
      bg.gone || (bg.flash && bg.flash === bg.other),
      '1.2 定位闪烁动画结束（行背景回到常态）',
      JSON.stringify(bg),
    );
    await siderItem(page, '记一笔').click();
    await until(() => pathnameOf(page) === '/');
    await siderItem(page, '流水').click();
    await until(() => pathnameOf(page) === '/records');
    await sleep(800);
    ok((await page.locator('.flash').count()) === 0, '1.2 定位后二次导航不重触发');
    ok(searchOf(page) === '?kind=expense', '1.2 二次导航后筛选仍在', searchOf(page));

    // 顺带验证 keyword 序列化，并清理测试流水
    await page.locator('input[placeholder="备注 / 平台 / 商家"]').fill(NOTE);
    await until(() => searchOf(page).includes(`keyword=${NOTE}`));
    ok(searchOf(page).includes(`keyword=${NOTE}`), '1.x 关键字条件写入 URL', searchOf(page));
    await sleep(800);
    const row = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE }).first();
    await row.getByRole('button', { name: /删\s*除/ }).click();
    await page.locator('.ant-popconfirm .ant-btn-primary, .ant-popover .ant-btn-primary').first().click();
    await page
      .locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE })
      .waitFor({ state: 'detached', timeout: 10000 })
      .catch(() => {});
    ok(true, '1.x 清理：测试流水已删除');
    await ctx.close();
  }

  // ================= 组 2：移动端列表加载（2.2–2.4，375px） =================
  // 现网数据仅 1 页，先造 30 笔确保 3 页；总数/末页行数均按 API 实际值动态计算（不依赖固定基线）
  await apiLogin();
  const SEED_TAG = `e2e-inf-${Date.now()}`;
  let seedIds = [];
  let failMode = 'pass'; // 2.4 失败路径开关（route 拦截；react-query 会重试，须持续 500 才能翻错误态）
  try {
    seedIds = await seedTxns(30, SEED_TAG);
    const seededTotal = (await api('/transactions?page=1&page_size=1')).json?.data?.total ?? 0;
    const TOTAL = seededTotal; // 造数后总笔数（≥41 才有第 3 页）
    // 第 3 页行数按 API 实际返回取（基线笔数会随使用增长，总页数可能 >3，不能用 TOTAL-40 推算）
    const P3ROWS = (await api('/transactions?page=3&page_size=20')).json?.data?.items?.length ?? 0;
    if (TOTAL < 41 || P3ROWS === 0) throw new Error(`seed: total ${TOTAL}, page3 rows ${P3ROWS}, insufficient`);

    const ctx = await browser.newContext({
      viewport: { width: 375, height: 667 },
      isMobile: true,
      hasTouch: true,
    });
    const page = await ctx.newPage();
    // 列表请求计数（page=N 精确匹配分页请求次数）
    const listReqs = [];
    page.on('request', (r) => {
      if (r.method() !== 'GET') return;
      const u = new URL(r.url());
      if (!u.pathname.endsWith('/transactions')) return;
      listReqs.push(Number(u.searchParams.get('page') ?? '1'));
    });
    // 刷新失败注入：mode='fail' 期间所有列表请求 500（react-query 会重试，单次 500 翻不了错误态）
    await page.route('**/api/v1/transactions?*', async (route) => {
      if (failMode === 'fail') {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ code: 50000, message: 'e2e 模拟列表失败' }),
        });
        return;
      }
      await route.continue();
    });

    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    await until(() => page.locator('.txn-row').count() >= 20, 15000);
    ok((await page.locator('.txn-row').count()) === 20, '2.2 移动端首屏整页 20 行（无限查询第 1 页）');
    ok((await page.locator('.ant-table').count()) === 0, '2.2 移动端渲染移动列表（非 Table）');

    // ---- 2.2 上拉加载：滚到底 → +1 页请求、新增 20 行；加载中快速连滚不重复请求 ----
    listReqs.length = 0;
    await page.evaluate(() => window.scrollTo({ top: document.body.scrollHeight, behavior: 'instant' }));
    await until(async () => (await page.locator('.txn-row').count()) >= 40, 15000);
    const rowsAfterFirst = await page.locator('.txn-row').count();
    const p2 = listReqs.filter((p) => p === 2).length;
    ok(rowsAfterFirst === 40 && p2 === 1, '2.2 滚到底追加一页（40 行，page=2 请求恰好 1 次）', `${rowsAfterFirst} 行 / page2×${p2}`);
    // 触发下一页后在加载途中快速连滚多次 → page=3 仍只 1 次
    listReqs.length = 0;
    await page.evaluate(() => window.scrollTo({ top: document.body.scrollHeight, behavior: 'instant' }));
    for (let i = 0; i < 4; i++) {
      await page.evaluate(() => window.scrollTo({ top: document.body.scrollHeight, behavior: 'instant' }));
      await sleep(60);
    }
    // 基线笔数随使用增长，总页数可能 >3：等前 3 页（60 行）到位即证明 +1 页完成
    const CUM3 = Math.min(TOTAL, 60);
    await until(async () => (await page.locator('.txn-row').count()) >= CUM3, 15000);
    const p3 = listReqs.filter((p) => p === 3).length;
    ok((await page.locator('.txn-row').count()) >= CUM3 && p3 === 1, '2.2 加载中快速连滚仍只 +1（page=3 请求恰好 1 次）', `page3×${p3}`);
    await shot(page, 'upgrade-04-mobile-infinite');

    // ---- 2.3 分页跳页：缓存命中直取 / 未命中回填、回顶、首条 = 第 41 条 ----
    await clickPager(page, 1);
    await until(() => !new URL(page.url()).searchParams.get('page'));
    // startPage=1 的前三页都已缓存 → 直取（缓存命中路径）；总页数可能 >3，只要求缓存页全量在位
    await until(async () => (await page.locator('.txn-row').count()) >= CUM3);
    ok((await page.locator('.txn-row').count()) >= CUM3, '2.3 回第 1 页缓存命中直取（全量行）', `>=${CUM3} 行`);
    // html 是 scroll-behavior: smooth，测试里的滚动必须显式 instant，否则读到动画中间态
    await page.evaluate(() => window.scrollTo({ top: 600, behavior: 'instant' }));
    const scrollBefore = await page.evaluate(() => window.scrollY);
    const apiP3 = await api('/transactions?page=3&page_size=20');
    const wantFirst = apiP3.json?.data?.items?.[0]?.id;
    await clickPager(page, 3);
    await until(() => new URL(page.url()).searchParams.get('page') === '3');
    await until(async () => (await page.locator('.txn-row').count()) === P3ROWS, 15000);
    const gotFirst = Number(await page.locator('.txn-row').first().getAttribute('data-txn-id'));
    const scrollAfter = await page.evaluate(() => window.scrollY);
    ok(scrollBefore > 0, '2.3 跳页前已滚动（验证回顶有意义）', `scrollY=${scrollBefore}`);
    ok(scrollAfter === 0, '2.3 点第 3 页后列表回到顶部', `scrollY=${scrollAfter}`);
    ok(gotFirst === wantFirst, '2.3 第 3 页首条 = 第 41 条（与后端同页一致）', `dom=${gotFirst} api=${wantFirst}`);
    await shot(page, 'upgrade-05-mobile-page3');

    // ---- 2.4 下拉刷新：顶部下拉触发恰好 1 次刷新；失败走 QueryState 错误态 + 重试 ----
    const cdp = await ctx.newCDPSession(page);
    const pullDrag = async () => {
      const box = await page.locator('.txn-list').boundingBox();
      const x = Math.round(box.x + box.width / 2);
      const y0 = Math.round(box.y + 30);
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y: y0 }] });
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: y0 + 80 }] });
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: y0 + 180 }] });
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    };
    // 下拉刷新要求 scrollTop===0（组件 onTouchStart 的前置守卫），先瞬时回顶并等落定
    const toTop = async () => {
      await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }));
      await until(() => page.evaluate(() => window.scrollY === 0));
    };
    await toTop();
    await sleep(200);
    listReqs.length = 0;
    await pullDrag();
    await sleep(1500); // 等刷新请求与渲染落定
    const refreshReqs = listReqs.length;
    const rowsAfterRefresh = await page.locator('.txn-row').count();
    ok(refreshReqs === 1 && rowsAfterRefresh === P3ROWS, '2.4 顶部下拉触发恰好 1 次刷新并展示最新数据', `请求×${refreshReqs} / ${rowsAfterRefresh} 行`);

    // 失败路径：持续 500（含 react-query 重试）→ 错误态（不得落入空态）+ 重试按钮
    await toTop();
    await sleep(200);
    failMode = 'fail';
    await pullDrag();
    const errShown = await until(() => page.locator('.query-state[role="alert"]').isVisible().catch(() => false), 20000);
    const errText = errShown ? await page.locator('.query-state').innerText() : '';
    ok(errShown && errText.includes('数据加载失败'), '2.4 刷新失败进入 QueryState 错误态（非空态）', errText.split('\n')[0] ?? '');
    // antd 会在两字中文按钮中间插空格（accessible name = "重 试"），用正则匹配
    const retryBtn = page.getByRole('button', { name: /重\s*试/ });
    ok(await retryBtn.isVisible().catch(() => false), '2.4 错误态提供「重试」按钮');
    await shot(page, 'upgrade-06-mobile-refresh-error');
    failMode = 'pass';
    await retryBtn.click();
    await until(async () => (await page.locator('.txn-row').count()) === P3ROWS, 15000);
    ok((await page.locator('.txn-row').count()) === P3ROWS, '2.4 点重试后列表恢复', `${P3ROWS} 行`);
    await ctx.close();

  // ---- 2.5 桌面 1440px 分页控件不受影响 ----
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records?page=3');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });
    const apiP3 = await api('/transactions?page=3&page_size=20');
    const wantFirst = apiP3.json?.data?.items?.[0]?.id;
    const gotFirst = Number(
      await page.locator('.ant-table-tbody tr[data-txn-id]').first().getAttribute('data-txn-id'),
    );
    ok((await page.locator('.txn-list').count()) === 0, '2.5 桌面渲染 Table（非移动列表）');
    ok(gotFirst === wantFirst, '2.5 桌面 URL page=3 直达同页数据', `dom=${gotFirst} api=${wantFirst}`);
    const totalTxt = await page.locator('.ant-pagination-total-text').innerText().catch(() => '');
    ok(new RegExp(`共\\s*${TOTAL}\\s*笔`).test(totalTxt), '2.5 桌面分页总数含造数', `${totalTxt} / 期望 ${TOTAL}`);
    await clickPager(page, 1);
    // 行数在 page3 也是满页时无法区分切页，等第 1 页 active 翻转（URL push 后渲染完成）
    await until(() => page.locator('.ant-pagination-item-1.ant-pagination-item-active').count() === 1, 10000);
    ok(
      await page.locator('.ant-pagination-item-1').evaluate((el) => el.classList.contains('ant-pagination-item-active')),
      '2.5 桌面点第 1 页正常回切',
    );
    await ctx.close();
  }
  } finally {
    // 清理：无论成败都删除组 2 造数（否则残留数据会污染后续基线）
    for (const id of seedIds) await api(`/transactions/${id}`, { method: 'DELETE' });
    if (seedIds.length) console.log(`      组 2 造数已清理（${seedIds.length} 笔 ${SEED_TAG}）`);
  }

  // ================= 组 3：筛选栏移动端收纳（3.1–3.2，375px） =================
  {
    const ctx = await browser.newContext({
      viewport: { width: 375, height: 667 },
      isMobile: true,
      hasTouch: true,
    });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    await until(() => page.locator('.txn-row').count() > 0, 15000);

    // 3.1 移动端单行「筛选入口 + 摘要」，不再渲染多行 filter-bar；首屏可见列表首行
    ok((await page.locator('.filter-entry').count()) === 1, '3.1 移动端渲染单行筛选入口');
    ok((await page.locator('.filter-bar').count()) === 0, '3.1 移动端不再渲染多行 filter-bar');
    const head = await page.locator('.txn-row').first().boundingBox();
    ok(
      !!head && head.y + head.height <= 667,
      '3.1 首屏可见列表首行（筛选栏不抢首屏）',
      head ? `rowBottom=${Math.round(head.y + head.height)}` : '(no row)',
    );
    await shot(page, 'upgrade-07-mobile-filter-entry');

    // 3.2 开抽屉改条件 → URL/列表刷新，收起后摘要同步
    await page.locator('.filter-entry .ant-btn').click();
    await until(() => page.locator('.ant-drawer-open').count() === 1, 8000);
    ok((await page.locator('.ant-drawer-open').count()) === 1, '3.2 点筛选打开底部抽屉');
    const totalBefore = await eyebrowTotal(page);
    await clickSeg(page, '.ant-drawer-open', '支出');
    await until(() => searchOf(page).includes('kind=expense'));
    ok(searchOf(page) === '?kind=expense', '3.2 抽屉内改条件写入 URL', searchOf(page));
    await until(async () => {
      const t = await eyebrowTotal(page);
      return t !== '' && t !== totalBefore;
    }, 8000);
    ok((await eyebrowTotal(page)) !== totalBefore, '3.2 抽屉内改条件列表刷新', `${totalBefore} → ${await eyebrowTotal(page)}`);
    // 抽屉内应有「清除」按钮（antd 两字中文插空格）
    ok(
      await page.locator('.ant-drawer-open').getByRole('button', { name: /清\s*除/ }).isVisible().catch(() => false),
      '3.2 抽屉内提供「清除」',
    );
    await shot(page, 'upgrade-08-mobile-filter-drawer');
    // 收起抽屉 → 摘要同步显示「支出」
    await page.locator('.ant-drawer-close').click();
    await until(() => page.locator('.ant-drawer-open').count() === 0, 8000);
    const summary = await page.locator('.filter-entry-summary').innerText();
    ok(summary.includes('支出'), '3.2 收起后摘要与条件同步', summary);
    ok((await page.locator('.filter-entry .ant-badge-count').innerText()) === '1', '3.2 摘要入口显示条件数角标');

    // 3.2 清除 → 条件重置（URL 回默认、摘要只剩区间、角标消失）
    await page.locator('.filter-entry .ant-btn').click();
    await until(() => page.locator('.ant-drawer-open').count() === 1, 8000);
    await page.locator('.ant-drawer-open').getByRole('button', { name: /清\s*除/ }).click();
    await until(() => searchOf(page) === '');
    ok(searchOf(page) === '', '3.2 清除后 URL 条件重置', searchOf(page) || '(empty)');
    await page.locator('.ant-drawer-close').click();
    await until(() => page.locator('.ant-drawer-open').count() === 0, 8000);
    const summaryCleared = await page.locator('.filter-entry-summary').innerText();
    ok(!summaryCleared.includes('支出') && /–/.test(summaryCleared), '3.2 清除后摘要只剩区间', summaryCleared);
    await ctx.close();
  }

  // ---- 3.2 桌面筛选形态无回归（1440px 仍是 filter-bar，无收纳入口/抽屉） ----
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });
    ok((await page.locator('.filter-bar').count()) === 1, '3.2 桌面仍渲染 filter-bar');
    ok((await page.locator('.filter-entry').count()) === 0, '3.2 桌面无移动端收纳入口');
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
  process.exit(1); // 关闭残留浏览器进程，避免挂住
});
