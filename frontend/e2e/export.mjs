import { currentAuthToken, latestBearer, trackPageAuth } from './support/auth-tokens.mjs';
/* eslint-disable no-console */
/**
 * add-ledger-system 任务 11.2：流水页导出按钮走查（specs/transactions「CSV 导出」）。
 * 运行：node e2e/export.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 默认区间（2026-10）下点击「导出」→ 下载 CSV：BOM 三字节 EF BB BF、
 *    数据行数 = 列表「共 N 笔」、内容与列表一致（中文分类全路径/金额方向/备注）
 *  - 组合筛选（kind=支出 + 备注关键字）后导出 → 行数 = 筛选后命中数（与列表同参）
 *  - Excel 中文不乱码：由 UTF-8 BOM 保证（Excel 依 BOM 判定编码），脚本校验
 *    BOM + UTF-8 解码后中文完整（无替换符 U+FFFD、无问号乱码）
 *  - viewer（只读）也可导出（四档角色均可读）
 * 导出为纯读操作，不改动数据，结束时校验基线未被触碰（支出仍为 922.66）。
 */
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SHOTS = path.join(__dirname, 'shots');
const DL = path.join(__dirname, 'downloads');
fs.mkdirSync(SHOTS, { recursive: true });
fs.rmSync(DL, { recursive: true, force: true });
fs.mkdirSync(DL, { recursive: true });

const EXE =
  process.env.CHROME_EXE ??
  path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe');
const BASE = process.env.BASE ?? 'http://localhost:5600';
const API = process.env.API ?? 'http://127.0.0.1:8080/api/v1';

const results = [];
let step = 0;
function ok(cond, label, extra = '') {
  const name = `${String(++step).padStart(2, '0')} ${label}`;
  results.push({ name, pass: !!cond });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? `  [${extra}]` : ''}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });

async function apiLogin(u, p) {
  const _uiToken = currentAuthToken(BASE, u);
  if (_uiToken) { return _uiToken; }
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ protocol_version: 2, username: u, password: p }),
  });
  const j = await r.json();
  if (j.code !== 0) throw new Error(j.message);
  return j.data.access_token;
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

/** 解析下载的 CSV：{bytes, bom, header, dataLines, text} */
function parseCsv(file) {
  const bytes = fs.readFileSync(file);
  const bom = bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  const text = bytes.toString('utf8').replace(/^﻿/, '');
  const lines = text.split('\r\n').filter((l) => l !== '');
  return {
    bytes,
    bom,
    header: lines[0] ?? '',
    dataLines: Math.max(0, lines.length - 1),
    text,
  };
}

/** 点击导出并等待下载完成，返回保存路径。 */
async function doExport(page, tag) {
  const [dl] = await Promise.all([
    page.waitForEvent('download', { timeout: 20000 }),
    page.getByRole('button', { name: /导\s*出/ }).click(),
  ]);
  const file = path.join(DL, `${tag}-${dl.suggestedFilename()}`);
  await dl.saveAs(file);
  return file;
}

/** 列表「共 N 笔」总数。 */
async function listTotal(page) {
  const txt = await page.locator('.ant-pagination-total-text').innerText().catch(() => '');
  const m = /共\s*(\d+)\s*笔/.exec(txt);
  if (m) return Number(m[1]);
  // 无分页文本（0 或 1 页且可能不显示）→ 用行数兜底
  return await page.locator('.ant-table-tbody tr.ant-table-row').count();
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ acceptDownloads: true, viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();

  // ================= 1. admin 导出默认区间 =================
  await login(page, 'admin', 'admin123');
  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(600);

  ok(await page.getByRole('button', { name: /导\s*出/ }).isVisible(), '流水页显示导出按钮');
  const total0 = await listTotal(page);
  ok(total0 > 0, '列表有数据', `total=${total0}`);

  // 基线（导出为纯读）：导出前先取本月支出合计，全部导出完成后再对比。
  // 不硬编码金额/月份——数据演进或跨月运行都不误报，语义仍是「导出不改数据」。
  const periodKey = `${new Date().getFullYear()}-${String(new Date().getMonth() + 1).padStart(2, '0')}`;
  const monthExpenseSum = async (token) => {
    const r = await fetch(
      `${API}/transactions?period_key=${periodKey}&kind=expense&page_size=100`,
      { headers: { authorization: `Bearer ${latestBearer(BASE, token)}` } },
    );
    const j = await r.json();
    return j.data.items.reduce((s, t) => s + Number(t.amount), 0);
  };
  const sumBefore = await monthExpenseSum(await apiLogin('admin', 'admin123'));

  const f0 = await doExport(page, 'default');
  const c0 = parseCsv(f0);
  ok(c0.bom, 'BOM 头三字节 = EF BB BF');
  ok(
    c0.header ===
      '日期,类型,分类,账户,转入账户,金额,备注,来源,用途,券编号,余额影响,消费影响,平台 / 商家',
    'CSV 表头与后端导出契约一致（含券/用途扩展列）',
    c0.header,
  );
  ok(c0.dataLines === total0, '默认区间导出行数 = 列表命中数', `${c0.dataLines} vs ${total0}`);

  // 中文完整性（Excel 不乱码由 BOM 保证；此处校验 UTF-8 解码后中文完好）
  ok(!c0.text.includes('�'), '无 U+FFFD 替换符（中文未乱码）');
  ok(/餐饮\/(午餐|早餐|晚餐|外卖|买菜)/.test(c0.text), '含分类全路径中文（餐饮/…）');
  ok(/支出/.test(c0.text) && /收入/.test(c0.text), '含类型中文标签');
  // 金额方向与列表一致：支出为负
  ok(/,-\d+\.\d\d,/.test(c0.text), '支出金额带负号（与列表一致）');
  await shot(page, 'export-01-default');
  console.log(`      默认导出样例（前 4 行）:\n${c0.text.split('\r\n').slice(0, 4).join('\r\n')}`);

  // ================= 2. 组合筛选后导出 =================
  await page.locator('.ant-segmented-item', { hasText: '支出' }).first().click();
  await sleep(700);
  const totalExp = await listTotal(page);
  ok(totalExp > 0 && totalExp <= total0, '筛选后列表命中数变化', `expense=${totalExp}`);

  const f1 = await doExport(page, 'expense');
  const c1 = parseCsv(f1);
  ok(c1.bom, '筛选导出仍带 BOM');
  ok(c1.dataLines === totalExp, '筛选导出行数 = 列表命中数', `${c1.dataLines} vs ${totalExp}`);
  ok(!/^\d{4}-\d\d-\d\d,收入,/m.test(c1.text), '筛选导出不含收入行（kind=支出 生效）');

  // 再叠加备注关键字（基线里有备注「午饭」的流水）
  await page.locator('input[placeholder="备注 / 平台 / 商家"]').fill('午饭');
  await sleep(800);
  const totalKw = await listTotal(page);
  const f2 = await doExport(page, 'keyword');
  const c2 = parseCsv(f2);
  ok(c2.dataLines === totalKw, '关键字叠加导出行数 = 列表命中数', `${c2.dataLines} vs ${totalKw}`);
  ok(totalKw > 0, '关键字筛选有命中（非空对比）', `total=${totalKw}`);
  ok(/午饭/.test(c2.text), '导出内容含筛选关键字');
  await shot(page, 'export-02-filtered');

  // 与后端 API 同参对比（列表与导出同一套筛选语义）
  const token = await apiLogin('admin', 'admin123');
  const r = await fetch(`${API}/transactions?kind=expense&keyword=${encodeURIComponent('午饭')}`, {
    headers: { authorization: `Bearer ${latestBearer(BASE, token)}` },
  });
  const j = await r.json();
  ok(c2.dataLines === j.data.total, '导出行数 = 后端同参 total', `${c2.dataLines} vs ${j.data.total}`);

  // ================= 3. viewer 只读也可导出 =================
  await login(page, 'dazhi', 'member123456');
  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  const totalV = await listTotal(page);
  const f3 = await doExport(page, 'viewer');
  const c3 = parseCsv(f3);
  ok(c3.bom, 'viewer 导出含 BOM');
  ok(c3.dataLines === totalV, 'viewer 导出行数 = 其列表命中数', `${c3.dataLines} vs ${totalV}`);
  await shot(page, 'export-03-viewer');

  // ================= 4. 基线未被触碰（导出为纯读，导出前后同参对比） =================
  const tokenA = await apiLogin('admin', 'admin123');
  const sumAfter = await monthExpenseSum(tokenA);
  ok(
    sumAfter === sumBefore,
    `基线未变：${periodKey} 支出合计导出前后一致`,
    `${sumBefore} → ${sumAfter}`,
  );

  await ctx.close();
  await browser.close();

  const pass = results.filter((r) => r.pass).length;
  console.log(`\n${pass}/${results.length} PASS`);
  if (pass !== results.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error('FATAL', e);
  process.exit(1);
});
