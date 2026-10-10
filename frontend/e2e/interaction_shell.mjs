/* eslint-disable no-console */
/**
 * improve-mobile-web-interaction 阶段 1 走查（断点导航 / 触控热区 / 触屏可见性 /
 * 减弱动效 / Tab 对比度 / 桌面密度不变）。
 * 运行：node e2e/interaction_shell.mjs   （需 dev server :5600 与后端 :8080 已启动）
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

async function login(page, username, password) {
  await page.goto(BASE + '/login');
  await page.locator('input[placeholder="用户名"]').fill(username);
  await page.locator('input[placeholder="密码"]').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.page-head').first().waitFor({ timeout: 15000 });
}

/** API 直接种数据：种子库可能为空，列表类断言依赖至少 1 行流水 + 1 条周期模板。 */
let TOKEN = '';
async function apiLogin() {
  if (TOKEN) return TOKEN;
  const r = await fetch(BASE + '/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'admin', password: 'admin123' }),
  });
  const j = await r.json();
  TOKEN = j.data?.access_token ?? '';
  if (!TOKEN) throw new Error('API login failed: ' + JSON.stringify(j));
  return TOKEN;
}
const api = (path, init = {}) =>
  fetch(BASE + '/api/v1' + path, {
    ...init,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${TOKEN}`, ...(init.headers ?? {}) },
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

/** 造一笔流水。 */
async function seedTxn(note) {
  await seedBase();
  const { status } = await api('/transactions', {
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
}

/** 造一条周期模板（用于窄屏裁切断言），返回其 id 便于清理。 */
async function seedRule() {
  await seedBase();
  const { status, json } = await api('/recurring', {
    method: 'POST',
    body: JSON.stringify({
      amount: '99.00',
      freq: 'monthly',
      account_id: SEED_ACCT,
      category_id: SEED_CAT,
      start_date: localToday(),
      note: 'e2e-shell-rule',
    }),
  });
  if (status >= 400) throw new Error('seed rule failed: ' + status);
  return json?.data?.id ?? 0;
}

async function deleteRule(id) {
  if (!id) return;
  await api(`/recurring/${id}`, { method: 'DELETE' });
}

/** 可见元素集合的最小高度（px），无可见元素返回 null。 */
async function minHeight(locator) {
  const boxes = [];
  const n = await locator.count();
  for (let i = 0; i < n; i++) {
    const el = locator.nth(i);
    if (!(await el.isVisible())) continue;
    const b = await el.boundingBox();
    if (b) boxes.push(b.height);
  }
  return boxes.length ? Math.min(...boxes) : null;
}

/** WCAG 对比度：页面内合成 fg/bg 后计算。 */
async function tabContrast(page) {
  return page.evaluate(() => {
    const parse = (s) => {
      const m = (s || '').match(/[\d.]+/g);
      if (!m) return null;
      const v = m.map(Number);
      return { r: v[0], g: v[1], b: v[2], a: v.length > 3 ? v[3] : 1 };
    };
    const over = (fg, bg) => ({
      r: fg.r * fg.a + bg.r * (1 - fg.a),
      g: fg.g * fg.a + bg.g * (1 - fg.a),
      b: fg.b * fg.a + bg.b * (1 - fg.a),
      a: 1,
    });
    const lum = (c) => {
      const f = (x) => {
        x /= 255;
        return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    };
    const ratio = (a, b) => {
      const [l1, l2] = [lum(a), lum(b)].sort((x, y) => y - x);
      return (l1 + 0.05) / (l2 + 0.05);
    };
    const measure = () => {
      const cs = getComputedStyle(document.documentElement);
      const bg = over(parse(cs.getPropertyValue('--tab-bg')), parse(cs.getPropertyValue('--bg')));
      const fg = over(parse(cs.getPropertyValue('--tab-fg')), bg);
      return Math.round(ratio(fg, bg) * 100) / 100;
    };
    const html = document.documentElement;
    const prev = html.dataset.theme;
    const light = measure();
    html.dataset.theme = 'dark';
    const dark = measure();
    if (prev === undefined) delete html.dataset.theme;
    else html.dataset.theme = prev;
    return { light, dark };
  });
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const touch = (w, h) => ({ viewport: { width: w, height: h }, isMobile: true, hasTouch: true });

  // ================= 1.1 断点一致：991/992 两侧都有导航 =================
  for (const [w, expectTabs] of [
    [991, true],
    [992, false],
  ]) {
    const ctx = await browser.newContext({ viewport: { width: w, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    const tabs = await page.locator('.bottom-tabs').count();
    const sider = await page.locator('.app-sider').count();
    if (expectTabs) {
      ok(tabs === 1 && sider === 0, `1.1 ${w}px → 底部 Tab 形态`, `tabs=${tabs} sider=${sider}`);
      const tabCount = await page.locator('.bottom-tabs .tab').count();
      ok(tabCount === 4, `1.1 ${w}px 底部四 Tab 枚举`, `${tabCount}`);
      const bell = await page.locator('.bottom-tabs .ant-badge').count();
      ok(bell >= 1, `1.1 ${w}px 铃铛工具位仍在 Tab 栏`, `${bell}`);
    } else {
      const sb = await page.locator('.app-sider').boundingBox();
      ok(tabs === 0 && sider === 1 && (sb?.width ?? 0) > 200, `1.1 ${w}px → 完整侧栏`, `sider=${sider} w=${sb?.width}`);
    }
    await shot(page, `shell-${w}`);
    await ctx.close();
  }

  // ================= 1.1 平板竖屏 900px（原「无导航带」） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 900, height: 1200 }, hasTouch: true, isMobile: true });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    const tabs = await page.locator('.bottom-tabs').count();
    const sider = await page.locator('.ant-layout-sider').count();
    ok(tabs === 1 && sider === 0, '1.1 平板 900px 有可用主导航（无无标签触发器）', `tabs=${tabs} sider=${sider}`);
    const tabBtn = page.locator('.bottom-tabs .tab').first();
    const tb = await tabBtn.boundingBox();
    ok((tb?.height ?? 0) >= 44, '1.1 Tab 命中区 ≥44px', `${tb?.height}px`);
    // 四个 Tab 都能跳转
    const paths = ['/', '/records', '/stats', '/mine'];
    for (let i = 0; i < 4; i++) {
      await page.locator('.bottom-tabs .tab').nth(i).click();
      await sleep(600);
      ok(page.url().replace(BASE, '').split('?')[0] === paths[i], `1.1 Tab${i + 1} 跳转可达`, page.url());
    }
    const aria = await page.locator('.bottom-tabs .tab.active[aria-current="page"]').count();
    ok(aria === 1, '1.1 当前 Tab 带 aria-current="page"', `${aria}`);
    await shot(page, 'shell-900-tabs');
    await ctx.close();
  }

  // ================= 1.4/1.5/1.6/1.8 手机 375px 触控热区 =================
  {
    const ctx = await browser.newContext(touch(375, 812));
    const page = await ctx.newPage();
    // 真机媒体特性：Playwright 的 isMobile/hasTouch 不会翻转 hover/pointer，
    // 用 CDP 把 hover 置 none、pointer 置 coarse（等价真实触屏）。
    const cdp = await ctx.newCDPSession(page);
    await cdp.send('Emulation.setEmulatedMedia', {
      features: [
        { name: 'hover', value: 'none' },
        { name: 'pointer', value: 'coarse' },
      ],
    });
    await login(page, 'admin', 'admin123');

    // 1.4 主按钮 / 控件
    const headBtn = await minHeight(page.locator('.page-head .ant-btn'));
    ok(headBtn !== null && headBtn >= 40, '1.4 页面主按钮命中区 ≥40px', `${headBtn}px`);

    // 记一笔弹窗：控件 + 分类磁贴 + 提交按钮
    await page.locator('.page-head').getByRole('button', { name: '快捷记账' }).click();
    const m = page.locator('.ant-modal-container', { hasText: '记一笔' }).first();
    await m.waitFor({ state: 'visible' });
    await sleep(500);
    const selH = await minHeight(m.locator('.ant-select'));
    const pickH = await minHeight(m.locator('.ant-picker'));
    ok(selH !== null && selH >= 40, '1.4 账户下拉命中区 ≥40px', `${selH}px`);
    ok(pickH !== null && pickH >= 40, '1.4 日期选择命中区 ≥40px', `${pickH}px`);
    const tileH = await minHeight(m.locator('.cat-leaves .ant-tag'));
    ok(tileH !== null && tileH >= 40, '1.5 分类磁贴命中区 ≥40px', `${tileH}px`);
    const quickH = await minHeight(m.locator('.ant-modal-body .ant-btn-sm'));
    ok(quickH !== null && quickH >= 40, '1.4 快捷金额按钮命中区 ≥40px', `${quickH}px`);
    const submit = m.locator('.ant-modal-footer .ant-btn-primary');
    const sb = await submit.boundingBox();
    ok((sb?.height ?? 0) >= 44, '1.4 提交主操作 ≥44px', `${sb?.height}px`);
    await shot(page, 'shell-375-txn-form');
    await m.locator('.ant-modal-footer .ant-btn:not(.ant-btn-primary)').first().click();
    await sleep(400);
    // 列表类断言依赖至少 1 行数据（种子库可能为空）
    await seedTxn(`shell375-${Date.now()}`);

    // 1.6 流水行内操作：触屏不压暗 + 命中区
    await page.goto(BASE + '/records');
    await page.locator('.txn-row').first().waitFor({ timeout: 15000 });
    const rowBtns = page.locator('.txn-actions .ant-btn');
    const opH = await minHeight(rowBtns);
    ok(opH !== null && opH >= 40, '1.6 行内操作命中区 ≥40px', `${opH}px`);
    const op = await page.evaluate(() => {
      const el = document.querySelector('.txn-actions');
      if (!el) return null;
      const cs = getComputedStyle(el);
      const hover = matchMedia('(hover: hover) and (pointer: fine)').matches;
      return { opacity: cs.opacity, hover };
    });
    ok(op && op.hover === false, '1.6 触屏设备判定为无 hover', JSON.stringify(op));
    ok(op && Number(op.opacity) === 1, '1.6 触屏下行内操作不压暗（opacity=1）', `opacity=${op?.opacity}`);
    // 分页命中区
    const pageH = await minHeight(page.locator('.pager .ant-pagination-item'));
    ok(pageH !== null && pageH >= 40, '1.4 分页页码命中区 ≥40px', `${pageH}px`);
    await shot(page, 'shell-375-records');

    // 1.8 Tab 对比度
    const c = await tabContrast(page);
    ok(c.light >= 4.5, '1.8 亮色 Tab 文字对比度 ≥4.5:1', `${c.light}:1`);
    ok(c.dark >= 4.5, '1.8 暗色 Tab 文字对比度 ≥4.5:1', `${c.dark}:1`);

    await ctx.close();
  }

  // ================= 1.3 窄屏不裁切（360px） =================
  for (const [route, cardSel, label] of [
    ['/categories', '.cat-root-row', '分类管理'],
    ['/recurring', '.rule-row', '周期记账'],
  ]) {
    const ctx = await browser.newContext(touch(360, 780));
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    // 周期模板库可能为空：先 API 造一条，断言完再删掉，避免污染其他脚本
    const ruleId = route === '/recurring' ? await seedRule() : 0;
    await page.goto(BASE + route);
    await page.locator(cardSel).first().waitFor({ timeout: 15000 });
    await sleep(800);
    const vw = 360;
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    ok(overflow <= 1, `1.3 ${label} 无横向溢出`, `超出 ${overflow}px`);
    // 卡片内全部按钮右缘在视口内
    let clipped = 0;
    const btns = page.locator(`${cardSel} .ant-btn`);
    const n = await btns.count();
    for (let i = 0; i < n; i++) {
      const b = await btns.nth(i).boundingBox();
      if (b && b.x + b.width > vw + 1) clipped++;
    }
    ok(clipped === 0, `1.3 ${label} 操作不被面板裁切`, `越界 ${clipped}/${n}`);
    await shot(page, `shell-360-${route.slice(1)}`);
    await deleteRule(ruleId);
    await ctx.close();
  }

  // ================= 1.7 减弱动效 =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce' });
    const page = await ctx.newPage();
    await page.goto(BASE + '/login');
    await page.waitForLoadState('networkidle').catch(() => {});
    await sleep(500);
    const anim = await page.evaluate(() => ({
      title: getComputedStyle(document.querySelector('.art-title')).animationName,
      card: getComputedStyle(document.querySelector('.login-card')).animationName,
      sub: getComputedStyle(document.querySelector('.art-sub')).animationName,
    }));
    ok(
      anim.title === 'none' && anim.card === 'none' && anim.sub === 'none',
      '1.7 登录页入场动画在减弱动效下关停',
      JSON.stringify(anim),
    );
    // 流水定位闪烁也关停（先造一笔，种子库可能为空）
    await login(page, 'admin', 'admin123');
    await seedTxn(`shellrm-${Date.now()}`);
    await page.goto(BASE + '/');
    await page.waitForLoadState('networkidle').catch(() => {});
    await page.locator('.recent-row.clickable').first().waitFor({ timeout: 15000 }).catch(() => {});
    if ((await page.locator('.recent-row.clickable').count()) > 0) {
      await page.locator('.recent-row.clickable').first().click();
      await page.waitForURL((u) => u.pathname.startsWith('/records'), { timeout: 10000 });
      const flashAnim = await page
        .locator('.flash')
        .first()
        .waitFor({ state: 'attached', timeout: 8000 })
        .then(() => page.evaluate(() => getComputedStyle(document.querySelector('.flash')).animationName))
        .catch(() => 'n/a');
      ok(flashAnim === 'none' || flashAnim === 'n/a', '1.7 定位闪烁在减弱动效下关停', `animation=${flashAnim}`);
    } else {
      ok(false, '1.7 定位闪烁在减弱动效下关停', '首页无最近流水可点');
    }
    await ctx.close();
  }

  // ================= 1.6/1.4 桌面密度与 hover 不变（1440px） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await ctx.newPage();
    await login(page, 'admin', 'admin123');
    const headBtn = await minHeight(page.locator('.page-head .ant-btn'));
    ok(headBtn !== null && headBtn < 40, '1.4 桌面按钮密度不变（<40px）', `${headBtn}px`);

    await seedTxn(`shelldesk-${Date.now()}`);
    await page.goto(BASE + '/records');
    await page.locator('.ant-table-tbody tr.ant-table-row').first().waitFor({ timeout: 15000 });
    const before = await page.evaluate(() => getComputedStyle(document.querySelector('.row-actions')).opacity);
    const hoverOn = await page.evaluate(() => matchMedia('(hover: hover) and (pointer: fine)').matches);
    const row = page.locator('.ant-table-tbody tr.ant-table-row').first();
    await row.hover();
    await sleep(400);
    const after = await page.evaluate(() => getComputedStyle(document.querySelector('.row-actions')).opacity);
    ok(hoverOn, '1.6 桌面判定为 hover 设备', `hover=${hoverOn}`);
    ok(Number(before) < 1, '1.6 桌面默认压暗保持', `opacity=${before}`);
    ok(Number(after) === 1, '1.6 桌面 hover 提亮保持', `hover 后 opacity=${after}`);

    // 桌面分类磁贴仍是紧凑尺寸
    await page.locator('.page-head').getByRole('button', { name: '记一笔' }).click();
    const m = page.locator('.ant-modal-container', { hasText: '记一笔' }).first();
    await m.waitFor({ state: 'visible' });
    await sleep(500);
    const tileH = await minHeight(m.locator('.cat-leaves .ant-tag'));
    ok(tileH !== null && tileH < 40, '1.5 桌面磁贴密度不变（<40px）', `${tileH}px`);
    await m.locator('.ant-modal-footer .ant-btn:not(.ant-btn-primary)').first().click();
    await shot(page, 'shell-1440-density');
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
