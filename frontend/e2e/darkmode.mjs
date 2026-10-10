/* eslint-disable no-console */
/**
 * add-ledger-system 任务 12.1：主题系统走查（specs/ui-shell「暗色模式」）。
 * 运行：node e2e/darkmode.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 手动切换并保持：手动切「暗色」→ 刷新页面仍为暗色（localStorage 持久化）
 *  - 跟随系统：主题设为「跟随系统」时，操作系统切深色 → 界面自动切暗；切回浅色 → 自动回浅
 *  附加校验：
 *  - 手动暗色优先于系统（系统浅色时手动暗色仍保持暗色）
 *  - 暗色同时作用于自绘 CSS 变量与 AntD（darkAlgorithm：Segmented 底、主按钮底变化）
 *  - 移动端底部 Tab 栏同样跟随暗色
 * 全程只改本地主题偏好（localStorage），不触碰业务数据；结束时恢复默认「跟随系统 + 浅色」。
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
  results.push({ name, pass: !!cond });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });
const norm = (s) => (s ?? '').replace(/\s+/g, '');

async function login(page, username, password) {
  await page.goto(BASE + '/login');
  await page.locator('input[placeholder="用户名"]').fill(username);
  await page.locator('input[placeholder="密码"]').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.locator('.page-head').first().waitFor({ timeout: 15000 });
}

/** 主题相关计算样式与持久化状态快照。 */
function snap(page) {
  return page.evaluate(() => {
    const bg = (sel) => {
      const el = document.querySelector(sel);
      return el ? getComputedStyle(el).backgroundColor : null;
    };
    const color = (sel) => {
      const el = document.querySelector(sel);
      return el ? getComputedStyle(el).color : null;
    };
    return {
      theme: document.documentElement.dataset.theme,
      colorScheme: document.documentElement.style.colorScheme,
      stored: localStorage.getItem('jizhang-theme'),
      bodyBg: getComputedStyle(document.body).backgroundColor,
      panelBg: bg('.panel'),
      segBg: bg('.ant-segmented'),
      primaryBtnBg: bg('.ant-btn-primary'),
      selectedItemColor: color('.ant-menu-item-selected'),
      tabBg: bg('.bottom-tabs'),
      tabColor: color('.bottom-tabs .tab:not(.active)'),
    };
  });
}

// 取值与 src/styles.css 的设计变量保持一致（:root 浅色 / [data-theme='dark'] 暗色）：
// 改设计色板时这里要同步，否则断言的是「色值」而非「主题切换」这件事本身。
const LIGHT_BODY = 'rgb(243, 244, 245)'; // --bg #f3f4f5
const LIGHT_PANEL = 'rgb(255, 255, 255)'; // --panel #ffffff
const DARK_BODY = 'rgb(11, 13, 12)'; // --bg #0b0d0c
const DARK_PANEL = 'rgb(20, 23, 22)'; // --panel #141716
const DARK_TAB_BG = 'rgba(20, 23, 22, 0.86)'; // --tab-bg（半透明面板底 + 毛玻璃）
const DARK_TAB_FG = 'rgba(236, 235, 228, 0.58)'; // --tab-fg（非选中 Tab 文字）
const LIGHT_PRIMARY_BTN = 'rgb(10, 117, 104)'; // --primary-fill #0a7568

const browser = await chromium.launch({ executablePath: EXE, headless: true });

// ================= 桌面：默认跟随系统（系统浅色） =================
{
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    colorScheme: 'light',
  });
  const page = await ctx.newPage();

  await page.goto(BASE + '/login');
  await sleep(400);
  const pre = await snap(page);
  ok(
    pre.theme === 'light' && pre.bodyBg === LIGHT_BODY,
    '12.1 登录页默认跟随系统=浅色（无闪烁残留）',
    `${pre.theme} ${pre.bodyBg}`,
  );

  await login(page, 'admin', 'admin123');
  await page.goto(BASE + '/mine');
  await page.locator('.ant-card-head-title', { hasText: '外观' }).waitFor({ timeout: 10000 });

  const seg = page.locator('.ant-segmented').first();
  ok(
    norm(await seg.locator('.ant-segmented-item-selected').innerText()) === '跟随系统',
    '12.1 「我的」页提供外观切换，默认「跟随系统」',
  );

  const light = await snap(page);
  ok(
    light.theme === 'light' && light.bodyBg === LIGHT_BODY && light.panelBg === LIGHT_PANEL,
    '12.1 默认（系统浅色）：页面与面板为浅色',
    `${light.theme} ${light.bodyBg} ${light.panelBg}`,
  );
  await shot(page, 'dark-01-mine-light');

  // ---------- 手动切暗色 ----------
  await seg.locator('.ant-segmented-item', { hasText: /暗\s*色/ }).click();
  await sleep(400);
  const dark = await snap(page);
  ok(
    dark.theme === 'dark' && dark.bodyBg === DARK_BODY && dark.panelBg === DARK_PANEL,
    '12.1 手动切换为暗色 → 立即生效（页面/面板暗色）',
    `${dark.theme} ${dark.bodyBg} ${dark.panelBg}`,
  );
  ok(
    dark.stored === 'dark' && dark.colorScheme === 'dark',
    '12.1 主题选择持久化在本地（localStorage + color-scheme）',
    `${dark.stored} / ${dark.colorScheme}`,
  );
  ok(
    dark.segBg !== light.segBg && dark.segBg !== null,
    '12.1 AntD 同步暗色（darkAlgorithm：Segmented 底变化）',
    `${light.segBg} → ${dark.segBg}`,
  );
  ok(
    dark.selectedItemColor === 'rgb(45, 212, 191)',
    '12.1 暗色下主色文字提亮（侧栏选中项 #2dd4bf）',
    dark.selectedItemColor ?? 'null',
  );
  await shot(page, 'dark-02-mine-dark');

  // ---------- 刷新保持 ----------
  await page.reload();
  await page.locator('.page-head').first().waitFor({ timeout: 15000 });
  await sleep(500);
  const afterReload = await snap(page);
  ok(
    afterReload.theme === 'dark' && afterReload.bodyBg === DARK_BODY && afterReload.stored === 'dark',
    '12.1 手动切换为暗色后刷新页面 → 仍为暗色（spec）',
    `${afterReload.theme} ${afterReload.bodyBg}`,
  );

  // ---------- 手动优先于系统 ----------
  await page.emulateMedia({ colorScheme: 'light' });
  await sleep(500);
  const manualWins = await snap(page);
  ok(
    manualWins.theme === 'dark',
    '12.1 手动暗色优先于系统（系统浅色时仍保持暗色）',
    manualWins.theme,
  );

  // ---------- 统计页图表在暗色下渲染 ----------
  await page.goto(BASE + '/stats');
  // ECharts 逐个异步渲染：只等第一个 canvas 就立刻数会数到一半（批跑抢 CPU 时必现），
  // 改成轮询到 >=3 张再取值；断言本身不变。
  await page
    .waitForFunction(() => document.querySelectorAll('canvas').length >= 3, null, { timeout: 15000 })
    .catch(() => {});
  const canvases = await page.locator('canvas').count();
  ok(canvases >= 3, '12.1 暗色下统计图表照常渲染（画布不发白）', `${canvases} 个画布`);
  await shot(page, 'dark-03-stats-dark');

  // ---------- 切回跟随系统 ----------
  await page.goto(BASE + '/mine');
  await page.locator('.ant-card-head-title', { hasText: '外观' }).waitFor({ timeout: 10000 });
  await page
    .locator('.ant-segmented')
    .first()
    .locator('.ant-segmented-item', { hasText: /跟随\s*系统/ })
    .click();
  await sleep(400);
  const backToSystem = await snap(page);
  ok(
    backToSystem.stored === 'system' && backToSystem.theme === 'light',
    '12.1 切回「跟随系统」→ 按系统（浅色）立即生效',
    `${backToSystem.stored} ${backToSystem.theme}`,
  );

  // ---------- 跟随系统：系统切深色 ----------
  await page.emulateMedia({ colorScheme: 'dark' });
  await sleep(500);
  const sysDark = await snap(page);
  ok(
    sysDark.theme === 'dark' && sysDark.bodyBg === DARK_BODY && sysDark.stored === 'system',
    '12.1 主题为跟随系统且系统切深色 → 界面自动切暗（spec）',
    `${sysDark.theme} ${sysDark.bodyBg}`,
  );
  await shot(page, 'dark-04-system-dark');

  await page.goto(BASE + '/');
  await page.locator('.ant-btn-primary').first().waitFor({ timeout: 10000 });
  await sleep(400);
  const btnDark = (await snap(page)).primaryBtnBg;

  // ---------- 跟随系统：系统切回浅色 ----------
  await page.emulateMedia({ colorScheme: 'light' });
  await sleep(500);
  const sysLight = await snap(page);
  ok(
    sysLight.theme === 'light' && sysLight.bodyBg === LIGHT_BODY,
    '12.1 系统切回浅色 → 界面自动回浅',
    `${sysLight.theme} ${sysLight.bodyBg}`,
  );
  const btnLight = (await snap(page)).primaryBtnBg;
  ok(
    btnLight !== btnDark && btnLight === LIGHT_PRIMARY_BTN,
    '12.1 AntD 主按钮底色随主题切换（明暗两套 token）',
    `${btnLight} vs ${btnDark}`,
  );
  await shot(page, 'dark-05-system-light');

  await ctx.close();
}

// ================= 移动端：默认跟随系统（系统深色） =================
{
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    colorScheme: 'dark',
  });
  const page = await ctx.newPage();
  await login(page, 'admin', 'admin123');
  await sleep(600);
  const m = await snap(page);
  ok(
    m.theme === 'dark' && m.bodyBg === DARK_BODY,
    '12.1 移动端跟随系统深色 → 同一套代码渲染暗色',
    `${m.theme} ${m.bodyBg}`,
  );
  ok(
    m.tabBg === DARK_TAB_BG && m.tabColor === DARK_TAB_FG,
    '12.1 移动端底部 Tab 栏同步暗色（面板底 + 非选中 Tab 文字色）',
    `${m.tabBg} ${m.tabColor}`,
  );
  await shot(page, 'dark-06-mobile-dark');

  await page.emulateMedia({ colorScheme: 'light' });
  await sleep(500);
  const m2 = await snap(page);
  ok(
    m2.theme === 'light' && m2.bodyBg === LIGHT_BODY,
    '12.1 移动端系统切浅色 → 自动回浅',
    `${m2.theme} ${m2.bodyBg}`,
  );
  await shot(page, 'dark-07-mobile-light');
  await ctx.close();
}

await browser.close();

const failed = results.filter((r) => !r.pass);
console.log(`\n==== 暗色模式 e2e: ${results.length - failed.length} 通过, ${failed.length} 失败 ====`);
if (failed.length) {
  for (const f of failed) console.log(`FAIL  ${f.name}`);
  process.exit(1);
}
