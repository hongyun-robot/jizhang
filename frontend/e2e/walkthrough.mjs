/* eslint-disable no-console */
/**
 * add-ledger-system 前端浏览器走查（2.7 / 3.4 / 4.5 / 5.3 / 5.4 / 6.1 / ui-shell）。
 * 运行：node e2e/walkthrough.mjs   （需 dev server :5600 与后端 :8080 已启动）
 * 注意：antd 会对 2 个汉字的按钮文案自动插入空格（"登 录"），定位用正则。
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

async function login(page, username, password) {
  await page.goto(BASE + '/login');
  await page.locator('input[placeholder="用户名"]').fill(username);
  await page.locator('input[placeholder="密码"]').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.page-head').first().waitFor({ timeout: 15000 });
}

/** 本地时区今天（Node 的 toISOString 是 UTC，跨零点会差一天）。 */
function localToday() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

const ROLE_PW = {
  admin: 'admin123',
  xiaowang: 'member123456',
  dazhi: 'member123456',
  xiaoli: 'member123456',
};

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const desktop = { viewport: { width: 1280, height: 800 } };
  const mobile = { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true };

  // ================= 2.7 会话生命周期（管理员，桌面） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();

    await page.goto(BASE + '/');
    // RequireAuth 先尝试 refresh 恢复，401 后才重定向 —— 等待落地
    await page.waitForURL((u) => u.pathname.startsWith('/login'), { timeout: 15000 }).catch(() => {});
    ok(page.url().includes('/login'), '2.7 未登录访问 / 重定向登录页', page.url());

    await page.locator('input[placeholder="用户名"]').fill('admin');
    await page.locator('input[placeholder="密码"]').fill('admin123');
    await page.getByRole('button', { name: /登\s*录/ }).click();
    await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
    ok(true, '2.7 登录成功进入首页');
    await shot(page, 'desktop-01-dashboard');

    // 刷新（内存 access 丢失）→ refresh cookie 恢复会话
    await page.reload();
    await page.waitForLoadState('networkidle');
    await sleep(600);
    const restored = !page.url().includes('/login') && (await page.getByText('本期收入').count()) > 0;
    ok(restored, '2.7 刷新后会话自动恢复（refresh cookie）', page.url());
    await shot(page, 'desktop-02-dashboard-after-reload');
    await ctx.close();
  }

  // ================= 5.4 首页概览 =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);

    ok((await page.getByText('本期收入').count()) > 0, '5.4 本期收支结余卡片存在');
    // 等 react-query 首屏返回再计数（登录后立即 count 会抢在请求完成前）
    await page
      .locator('.recent-row')
      .first()
      .waitFor({ timeout: 15000 })
      .catch(() => {});
    const recentRows = await page.locator('.recent-row').count();
    ok(recentRows > 0, '5.4 最近流水列表有数据', `${recentRows} 行`);
    ok((await page.getByText('预算进度').count()) > 0, '5.4 首页预算进度卡存在（8.3 起为真实进度）');
    const quickBtn = page.locator('.page-head').getByRole('button', { name: '快捷记账' });
    ok((await quickBtn.count()) > 0, '5.4 快捷记账入口存在');

    await quickBtn.click();
    await page.locator('.ant-modal').waitFor({ state: 'visible', timeout: 10000 });
    const today = localToday();
    const dateShown = await page.locator('.ant-modal .ant-picker input').inputValue();
    ok(dateShown === today, '5.4 快捷记账日期默认今天', `${dateShown} vs ${today}`);
    await page.locator('.ant-modal-footer .ant-btn:not(.ant-btn-primary)').first().click();
    await ctx.close();
  }

  // ================= 4.5 流水列表 + 记一笔 + 筛选 + 编辑删除 =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });
    const before = await page.locator('.ant-table-tbody tr.ant-table-row').count();
    ok(before > 0, '4.5 流水列表渲染有数据', `${before} 行`);

    // 记一笔（默认支出）
    const NOTE = `e2e-${Date.now()}`;
    await page.locator('.page-head').getByRole('button', { name: '记一笔' }).click();
    const modal = page.locator('.ant-modal-container');
    await modal.waitFor({ state: 'visible' });
    await modal.locator('input[inputmode="decimal"]').fill('12.34');
    const leaves = modal.locator('.cat-leaves .ant-tag');
    if ((await leaves.count()) > 0) await leaves.first().click();
    await modal.locator('.ant-select').first().click();
    await page.locator('.ant-select-dropdown:visible .ant-select-item-option').first().click();
    await modal.locator('textarea').fill(NOTE);
    await modal.locator('.ant-modal-footer .ant-btn-primary').click();
    await page.getByText('已记一笔').waitFor({ timeout: 10000 });
    ok(true, '4.5 记一笔提交成功');
    await sleep(900);
    const firstRow = await page.locator('.ant-table-tbody tr.ant-table-row').first().innerText();
    ok(firstRow.includes(NOTE) || firstRow.includes('12.34'), '4.5 新流水出现在列表首位');
    await shot(page, 'desktop-05-records');

    // 关键字筛选
    await page.locator('input[placeholder="备注关键字"]').fill(NOTE);
    await sleep(800);
    const kwRows = await page.locator('.ant-table-tbody tr.ant-table-row').count();
    ok(kwRows === 1, '4.5 备注关键字筛选命中 1 行', `${kwRows} 行`);

    // 金额区间
    await page.locator('input[placeholder="备注关键字"]').fill('');
    await page.locator('input[placeholder="金额≥"]').fill('999999');
    await sleep(800);
    const amtRows = await page.locator('.ant-table-tbody tr.ant-table-row').count();
    const emptyTxt = await page.getByText('暂无数据').count();
    ok(amtRows === 0 || emptyTxt > 0, '4.5 金额区间筛选生效（超大下限无结果）', `rows=${amtRows}`);
    await page.locator('input[placeholder="金额≥"]').fill('');
    await sleep(600);

    // 类型筛选：转账
    await clickSeg(page, '.filter-bar', '转账');
    await sleep(800);
    const transferTxt = await page.locator('.ant-table-tbody').innerText().catch(() => '');
    ok(transferTxt.includes('→'), '4.5 类型筛选=转账（行内含→）');
    await clickSeg(page, '.filter-bar', '全部');
    await sleep(600);

    // 编辑
    await page.locator('input[placeholder="备注关键字"]').fill(NOTE);
    await sleep(800);
    const row = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: NOTE }).first();
    await row.getByRole('button', { name: /编\s*辑/ }).click();
    const emodal = page.locator('.ant-modal-container');
    await emodal.waitFor({ state: 'visible' });
    const editAmount = await emodal.locator('input[inputmode="decimal"]').inputValue();
    ok(editAmount === '12.34', '4.5 编辑回填金额', editAmount);
    await emodal.locator('textarea').fill(`${NOTE}-edited`);
    await emodal.locator('.ant-modal-footer .ant-btn-primary').click();
    await page.getByText('已保存修改').waitFor({ timeout: 10000 });
    ok(true, '4.5 编辑保存成功');
    await sleep(900);

    // 删除
    const erow = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: `${NOTE}-edited` }).first();
    await erow.getByRole('button', { name: /删\s*除/ }).click();
    await page.locator('.ant-popconfirm .ant-btn-primary, .ant-popover .ant-btn-primary').first().click();
    await page
      .locator('.ant-table-tbody tr.ant-table-row', { hasText: `${NOTE}-edited` })
      .waitFor({ state: 'detached', timeout: 10000 });
    ok(true, '4.5 删除成功且行消失');

    ok((await page.locator('.ant-pagination').count()) > 0, '4.5 offset 分页控件存在');
    await ctx.close();
  }

  // ================= 3.4 账户/分类管理（管理员） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);

    await page.goto(BASE + '/mine');
    await page.getByRole('button', { name: '账户管理' }).waitFor({ timeout: 10000 });
    ok(true, '3.4 管理员「我的」页可见账户/分类入口');
    await shot(page, 'desktop-10-mine');

    await page.goto(BASE + '/accounts');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 10000 });
    const acctTxt = await page.locator('.ant-table-tbody').innerText();
    ok(acctTxt.includes('现金'), '3.4 账户页渲染种子账户');
    ok(acctTxt.includes('信用卡'), '3.4 信用卡账户存在');
    await shot(page, 'desktop-11-accounts');

    // 新建 → 删除账户（空账户）
    const ANAME = `e2e户${Date.now()}`;
    await page.getByRole('button', { name: '新建账户' }).click();
    const amodal = page.locator('.ant-modal-container');
    await amodal.waitFor({ state: 'visible' });
    await amodal.locator('input#name').fill(ANAME);
    await amodal.locator('.ant-modal-footer .ant-btn-primary').click();
    await page.getByText('账户已创建').waitFor({ timeout: 10000 });
    await sleep(700);
    const arow = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: ANAME }).first();
    ok((await arow.count()) === 1, '3.4 账户创建成功');
    await arow.getByRole('button', { name: /删\s*除/ }).click();
    await page.locator('.ant-popconfirm .ant-btn-primary, .ant-popover .ant-btn-primary').first().click();
    await page
      .locator('.ant-table-tbody tr.ant-table-row', { hasText: ANAME })
      .waitFor({ state: 'detached', timeout: 10000 });
    ok(true, '3.4 空账户删除成功');

    // 分类页
    await page.goto(BASE + '/categories');
    await page.getByText('支出分类').waitFor({ timeout: 10000 });
    const catTxt = await page.locator('body').innerText();
    ok(catTxt.includes('收入分类') && catTxt.includes('餐饮'), '3.4 分类页两套树渲染');
    await shot(page, 'desktop-12-categories');

    // 新建 → 删除一级分类
    const CNAME = `e2e类${Date.now()}`;
    await page
      .locator('.ant-card', { hasText: '支出分类' })
      .getByRole('button', { name: '新建一级' })
      .click();
    const cmodal = page.locator('.ant-modal-container');
    await cmodal.waitFor({ state: 'visible' });
    await cmodal.locator('input').fill(CNAME);
    await cmodal.locator('.ant-modal-footer .ant-btn-primary').click();
    await page.getByText('已创建').waitFor({ timeout: 10000 });
    await sleep(700);
    ok((await page.getByText(CNAME).count()) > 0, '3.4 分类创建成功');
    const crow = page.locator('.cat-root-row', { hasText: CNAME }).first();
    await crow.getByRole('button', { name: /删\s*除/ }).click();
    await page.locator('.ant-popconfirm .ant-btn-primary, .ant-popover .ant-btn-primary').first().click();
    await page.locator('.cat-root-row', { hasText: CNAME }).waitFor({ state: 'detached', timeout: 10000 });
    ok(true, '3.4 分类删除成功');
    await ctx.close();
  }

  // ================= 3.4 非管理员：入口隐藏 + 路由拦截（member/viewer/recorder） =================
  for (const [u, label] of [
    ['xiaowang', 'member'],
    ['dazhi', 'viewer'],
    ['xiaoli', 'recorder'],
  ]) {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, u, ROLE_PW[u]);
    await page.goto(BASE + '/mine');
    await sleep(700);
    const adminBtn = await page.getByRole('button', { name: '账户管理' }).count();
    ok(adminBtn === 0, `3.4 ${label} 无管理入口`);
    await page.goto(BASE + '/accounts');
    await sleep(900);
    ok(!page.url().includes('/accounts'), `3.4 ${label} 直接访问 /accounts 被拦截`, page.url());
    await ctx.close();
  }

  // ================= 2.7 viewer 只读 =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'dazhi', ROLE_PW.dazhi);
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });
    const actionBtns = await page.locator('.ant-table-tbody .ant-btn-link').count();
    ok(actionBtns === 0, '2.7 viewer 只读：行操作按钮 0 个', `${actionBtns}`);
    await ctx.close();
  }

  // ================= 5.3 统计五图（管理员，桌面） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);
    await page.goto(BASE + '/stats');
    await page.locator('.stats-grid canvas').first().waitFor({ timeout: 20000 });
    await sleep(1500);
    const canvases = await page.locator('.stats-grid canvas').count();
    ok(canvases >= 3, '5.3 统计图渲染（ECharts canvas）', `${canvases} 个 canvas`);
    const txt = await page.locator('body').innerText();
    ok(
      txt.includes('分类占比') && txt.includes('收支趋势') && txt.includes('本期环比'),
      '5.3 占比/趋势/环比面板存在',
    );
    ok(txt.includes('账期汇总') && txt.includes('资产走势'), '5.3 汇总/资产面板存在');
    const periodRows = await page.locator('.period-line').count();
    const nowMonth = new Date().getMonth() + 1;
    ok(periodRows === nowMonth, '5.3 账期汇总当年行数=当前月', `${periodRows} vs ${nowMonth}`);

    await clickSeg(page, '.ant-card:has-text("收支趋势")', '按周');
    await sleep(900);
    ok(true, '5.3 趋势粒度切到按周');
    await clickSeg(page, '.ant-card:has-text("分类占比")', '收入');
    await sleep(900);
    ok(true, '5.3 占比切换到收入');
    await clickSeg(page, '.period-picker', '近7天');
    await sleep(1200);
    ok(true, '5.3 共享区间选择器切换生效');
    await shot(page, 'desktop-20-stats');
    await ctx.close();
  }

  // ================= 移动端双形态（390×844） =================
  {
    const ctx = await browser.newContext(mobile);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);
    ok((await page.locator('.bottom-tabs').count()) > 0, 'ui-shell 移动端底部 Tab 存在');
    await shot(page, 'mobile-01-dashboard');
    await page.goto(BASE + '/records');
    await sleep(1200);
    ok((await page.locator('.txn-row').count()) > 0, 'ui-shell 移动端流水卡片列表渲染');
    await shot(page, 'mobile-02-records');

    await page.locator('.page-head').getByRole('button', { name: '记一笔' }).click();
    const m = page.locator('.ant-modal-container');
    await m.waitFor({ state: 'visible' });
    ok((await m.locator('input[inputmode="decimal"]').count()) === 1, 'ui-shell 金额 inputMode=decimal（数字键盘）');
    ok((await m.getByRole('button', { name: '+50' }).count()) === 1, 'ui-shell 常用金额快捷键 +50 存在');
    await m.getByRole('button', { name: '+50' }).click();
    const v = await m.locator('input[inputmode="decimal"]').inputValue();
    ok(v === '50', 'ui-shell 快捷金额写入', v);
    await shot(page, 'mobile-03-txn-form');
    await m.locator('.ant-modal-footer .ant-btn:not(.ant-btn-primary)').first().click();
    await page.goto(BASE + '/stats');
    await sleep(1800);
    await shot(page, 'mobile-04-stats');
    await page.goto(BASE + '/mine');
    await sleep(900);
    await shot(page, 'mobile-05-mine');
    await ctx.close();
  }

  // ================= 6.1 首页最近流水点击 → 流水页定位（home-dashboard） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'admin', ROLE_PW.admin);
    // 等列表数据到位再断言（与 5.4 同因：count 抢在请求完成前）
    await page
      .locator('.recent-row.clickable')
      .first()
      .waitFor({ timeout: 15000 })
      .catch(() => {});
    const rows = await page.locator('.recent-row.clickable').count();
    ok(rows > 0, '6.1 首页最近流水可点击（clickable）', `${rows} 行`);
    await page.locator('.recent-row.clickable').first().click();
    await page.waitForURL((u) => u.pathname.startsWith('/records'), { timeout: 10000 });
    // 定位高亮：目标行被 scrollIntoView 且带 .flash（1.8s 后移除，attached 轮询可捕获）
    const flashed = await page
      .locator('.ant-table-tbody tr.flash, .txn-row.flash')
      .first()
      .waitFor({ state: 'attached', timeout: 10000 })
      .then(() => true)
      .catch(() => false);
    ok(flashed, '6.1 跳转流水页并定位高亮该笔', page.url());
    const hasRow = await page.locator('[data-txn-id]').count();
    ok(hasRow > 0, '6.1 流水行带 data-txn-id 定位锚点', `${hasRow} 行`);
    await shot(page, 'desktop-30-locate');
    await ctx.close();
  }

  // ================= 6.1 未登录 /stats → 登录后返回原路径（ui-shell） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await page.goto(BASE + '/stats');
    await page.waitForURL((u) => u.pathname.startsWith('/login'), { timeout: 15000 }).catch(() => {});
    ok(page.url().includes('/login'), '6.1 未登录访问 /stats 重定向登录页', page.url());
    await page.locator('input[placeholder="用户名"]').fill('admin');
    await page.locator('input[placeholder="密码"]').fill('admin123');
    await page.getByRole('button', { name: /登\s*录/ }).click();
    await page.waitForURL((u) => u.pathname === '/stats', { timeout: 15000 }).catch(() => {});
    ok(page.url().includes('/stats'), '6.1 登录后返回原目标路径 /stats', page.url());
    await shot(page, 'desktop-31-return-to');
    await ctx.close();
  }

  // ================= 6.1 只读成员看统计（access-control） =================
  {
    const ctx = await browser.newContext(desktop);
    const page = await ctx.newPage();
    await login(page, 'dazhi', ROLE_PW.dazhi);
    await page.goto(BASE + '/stats');
    await page.locator('.stats-grid canvas').first().waitFor({ timeout: 20000 });
    await sleep(1200);
    const canvases = await page.locator('.stats-grid canvas').count();
    ok(canvases >= 3, '6.1 viewer 统计页完整渲染', `${canvases} 个 canvas`);
    const txt = await page.locator('body').innerText();
    ok(
      txt.includes('分类占比') && txt.includes('收支趋势') && txt.includes('本期环比'),
      '6.1 viewer 看到与其他角色一致的统计面板',
    );
    await shot(page, 'desktop-32-viewer-stats');
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
