/* eslint-disable no-console */
// 一次性对照:断点收敛(600/768/992)后 375/768/900/1440 四档逐页截图 + 溢出检查。
// 运行:node e2e/breakpoint-diff.mjs
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(__dirname, 'shots', 'bp');
fs.rmSync(OUT, { recursive: true, force: true });
fs.mkdirSync(OUT, { recursive: true });
const EXE = path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe');
const BASE = 'http://localhost:5600';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const VPS = [
  { name: '375', width: 375, height: 740, isMobile: true },
  { name: '768', width: 768, height: 900 },
  { name: '900', width: 900, height: 900 },
  { name: '1440', width: 1440, height: 900 },
];
const PAGES = ['/', '/records', '/stats', '/budget', '/accounts', '/categories', '/recurring', '/vouchers', '/mine', '/assets', '/login'];

const b = await chromium.launch({ executablePath: EXE, headless: true });
let issues = 0;
for (const vp of VPS) {
  const ctx = await b.newContext({ viewport: { width: vp.width, height: vp.height }, isMobile: !!vp.isMobile, hasTouch: !!vp.isMobile });
  const p = await ctx.newPage();
  await p.goto(BASE + '/login');
  await p.locator('input[placeholder="用户名"]').fill('admin');
  await p.locator('input[placeholder="密码"]').fill('admin123');
  await p.getByRole('button', { name: /登\s*录/ }).click();
  await p.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
  for (const pathName of PAGES) {
    await p.goto(BASE + pathName).catch(() => {});
    await p.waitForTimeout(900);
    const r = await p.evaluate(() => ({ docW: document.documentElement.clientWidth, scrollW: document.documentElement.scrollWidth }));
    const over = r.scrollW > r.docW + 1;
    if (over) issues++;
    console.log(`${over ? 'OVERFLOW' : 'ok     '} ${vp.name.padEnd(4)} ${pathName.padEnd(12)} doc=${r.docW} scroll=${r.scrollW}`);
    await p.screenshot({ path: path.join(OUT, `${vp.name}${pathName.replace(/\//g, '_')}.png`), fullPage: false });
  }
  await ctx.close();
}
await b.close();
console.log(`\n${issues === 0 ? '四档全部无横向溢出' : issues + ' 处溢出'};截图在 e2e/shots/bp/`);
if (issues) process.exitCode = 1;
