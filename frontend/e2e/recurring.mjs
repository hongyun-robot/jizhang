/* eslint-disable no-console */
/**
 * add-ledger-system 任务 9.4：specs/recurring-entries 全场景浏览器走查。
 * 运行：node e2e/recurring.mjs   （需 dev server :5600 与后端 :8080 已启动）
 *
 * 覆盖场景：
 *  - 按角色与创建者归属授权（specs/recurring-entries 归属隔离）：
 *    member 可进 /recurring 自助建模板并生成带「自动」标记的流水、入口在「我的」页可见；
 *    member 改他人模板 403；viewer 可读列表（空）但写 403、UI 直达被拦且无入口；
 *    admin 看全部模板并显示创建人；懒补齐（ensure）对任意登录角色开放
 *  - 创建「每月1日 2500 银行卡 → 居家/房租」模板 → 打开应用懒补齐生成到期流水
 *    （entry_date=2026-10-01、period_key=2026-10、source=recurring，流水行带「自动」标记）
 *  - 停机跨期补齐：start=2026-08-01 的月度模板一次补齐 08/09/10 三期；再次打开不重复（幂等）
 *  - 生成笔改删独立：修正生成笔 100→120 → 模板金额不变；删除生成笔 → 模板保持启用，
 *    重写 next_run_date 后照常按模板金额重新生成
 *  - 模板编辑不影响已生成流水；启用/暂停状态 UI 往返
 * 运行结束恢复基线（规则清空、生成笔清空、10 月支出回到 922.66）——
 * 分类全程复用基线种子（居家/房租、居家/水电燃气），不增不删。
 *
 * SQL 只用 ASCII 谓词（rule_id IS NOT NULL / id），中文值读回后在 JS 侧过滤
 * ——避免 Windows 命令行的 CJK 编码问题。
 */
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';
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
  const r = await fetch(`${API}/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username: u, password: p }),
  });
  const j = await r.json();
  if (j.code !== 0) throw new Error(j.message);
  return j.data.access_token;
}

async function api(method, p, token, body) {
  const r = await fetch(API + p, {
    method,
    headers: { 'content-type': 'application/json', authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, body: j };
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

async function expectMsg(page, re, timeout = 6000) {
  try {
    await page.locator('.ant-message', { hasText: re }).first().waitFor({ timeout });
    return true;
  } catch {
    return false;
  }
}

/** dev 环境 SQL（查询文本只用 ASCII，中文在 JS 侧过滤）。 */
function sql(q) {
  return execSync(
    `wsl -d Ubuntu-20.04 -u root -- docker exec jizhang-mysql mysql -uroot -pdevpass --default-character-set=utf8mb4 jizhang -N -e "${q}"`,
    { encoding: 'utf8' },
  ).trim();
}
function sqlRows(q) {
  return sql(q)
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);
}

/** 生成笔（带 rule_id）：{id, entry_date, period_key, amount, note, source} */
function ruleTxns() {
  return sqlRows(
    'SELECT id, entry_date, period_key, amount, note, source FROM transactions WHERE rule_id IS NOT NULL ORDER BY entry_date, id',
  ).map((l) => {
    const [id, entry_date, period_key, amount, note, source] = l.split('\t');
    return { id, entry_date, period_key, amount, note, source };
  });
}
/** 模板：{id, note, amount, status, next} */
function rules() {
  return sqlRows('SELECT id, note, amount, status, next_run_date FROM recurring_rules ORDER BY id').map(
    (l) => {
      const [id, note, amount, status, next] = l.split('\t');
      return { id, note, amount, status, next };
    },
  );
}
const txnNote = (note) => ruleTxns().filter((t) => t.note === note);
const findRoot = (forest, name) => {
  for (const w of forest ?? []) {
    const hit = (w.children ?? []).find((r) => r.name === name);
    if (hit) return hit;
  }
  return null;
};

async function fillRuleModal(page, opts) {
  await page.getByRole('button', { name: /新建周期/ }).click();
  const modal = page.locator('.ant-modal-container', { hasText: '新建周期模板' });
  await modal.waitFor({ state: 'visible' });
  await modal.locator('.amount-row input').fill(opts.amount);
  await modal.locator('.ant-segmented-item', { hasText: '每月' }).click();
  await modal.locator('.cat-roots .ant-tag', { hasText: opts.rootRe }).click();
  await modal.locator('.cat-leaves .ant-tag', { hasText: opts.leafRe }).click();
  await modal.locator('.ant-select').first().click();
  const opt = page
    .locator('.ant-select-dropdown:visible .ant-select-item-option', { hasText: '银行卡' })
    .first();
  await opt.waitFor({ timeout: 5000 });
  await opt.click();
  const picker = modal.locator('.ant-picker input');
  await picker.click();
  await picker.fill(opts.start);
  await picker.press('Enter');
  await modal.locator('input[placeholder*="备注"]').fill(opts.note);
  const pickerOk = (await picker.inputValue()) === opts.start;
  await modal.locator('.ant-modal-footer .ant-btn-primary').click();
  const got = await expectMsg(page, /已创建周期模板/);
  return { got, pickerOk };
}

async function main() {
  const browser = await chromium.launch({ executablePath: EXE, headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();

  // ================= 0. 基线准备（API + SQL，幂等） =================
  const tok = await apiLogin('admin', 'admin123');
  await api('PUT', '/settings', tok, { settlement_day: 1 });
  sql('DELETE FROM transactions WHERE rule_id IS NOT NULL');
  sql('DELETE FROM recurring_rules');

  // 分类全程复用基线种子（严禁增删分类——基线 36 个、8 个支出根）
  const forest = (await api('GET', '/categories', tok)).body.data;
  const home = findRoot(forest, '居家');
  const rentCat = (home?.children ?? []).find((c) => c.name === '房租');
  const utilCat = (home?.children ?? []).find((c) => c.name === '水电燃气');
  ok(!!home && !!rentCat && !!utilCat, 'setup：基线分类 居家/房租、居家/水电燃气 可用');
  const roots0 = sql("SELECT COUNT(*) FROM categories WHERE parent_id IS NULL AND kind='expense'");
  ok(roots0 === '8', 'setup：支出一级分类基线 8 个', `got=${roots0}`);

  // ================= 1. 空态（管理员可进 /recurring） =================
  await login(page, 'admin', 'admin123');
  await page.goto(BASE + '/recurring');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  ok(
    await page.locator('.ant-empty', { hasText: '还没有周期模板' }).isVisible(),
    '管理员进入 /recurring，显示空态引导',
  );
  await shot(page, 'recurring-01-empty');

  // ================= 2. 按角色与创建者归属授权（specs/recurring-entries 归属隔离） =================
  const dazhiRole = sql("SELECT role FROM users WHERE username='dazhi'");
  ok(dazhiRole === 'viewer', 'setup：dazhi 角色为 viewer', `role=${dazhiRole}`);

  // 2a. 普通成员：「我的」页有入口，/recurring 可直达（列表只含自己的）
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const p2 = await ctx2.newPage();
  await login(p2, 'xiaowang', 'member123456');
  await p2.goto(BASE + '/mine');
  await p2.waitForLoadState('networkidle').catch(() => {});
  ok(
    (await p2.locator('button', { hasText: '周期记账' }).count()) > 0,
    'member「我的」页可见周期记账入口',
  );
  await p2.goto(BASE + '/recurring');
  await p2.waitForLoadState('networkidle').catch(() => {});
  await sleep(600);
  ok(
    new URL(p2.url()).pathname === '/recurring',
    'member 直达 /recurring 可进入（不再仅管理员）',
    new URL(p2.url()).pathname,
  );
  ok(
    await p2.locator('.ant-empty', { hasText: '还没有周期模板' }).isVisible(),
    'member 空列表（只显示自己创建的模板）',
  );
  const mTok = await apiLogin('xiaowang', 'member123456');
  const vTok = await apiLogin('dazhi', 'member123456');
  const mGet = await api('GET', '/recurring', mTok);
  ok(
    mGet.status === 200 && mGet.body.data.length === 0,
    'member 读模板 200 且为空列表',
    `status=${mGet.status} len=${mGet.body.data?.length}`,
  );
  await shot(p2, 'recurring-02-member-open');

  // 2b. 只读成员：无入口、UI 直达被拦；列表可读（空）、写接口 403
  const ctxV = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const pV = await ctxV.newPage();
  await login(pV, 'dazhi', 'member123456');
  await pV.goto(BASE + '/mine');
  await pV.waitForLoadState('networkidle').catch(() => {});
  ok(
    (await pV.locator('button', { hasText: '周期记账' }).count()) === 0,
    'viewer「我的」页无周期记账入口',
  );
  await pV.goto(BASE + '/recurring');
  await pV.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  ok(new URL(pV.url()).pathname === '/', 'viewer 直达 /recurring 被拦回首页', new URL(pV.url()).pathname);
  const vGet = await api('GET', '/recurring', vTok);
  ok(
    vGet.status === 200 && vGet.body.data.length === 0,
    'viewer 读模板 200 且为空列表',
    `status=${vGet.status} len=${vGet.body.data?.length}`,
  );
  const vPost = await api('POST', '/recurring', vTok, {
    amount: '10',
    freq: 'monthly',
    account_id: 1,
    category_id: 1,
    start_date: '2026-10-01',
  });
  ok(vPost.status === 403, 'viewer 创建模板 403', `status=${vPost.status}`);
  await shot(pV, 'recurring-02-viewer-blocked');
  await ctxV.close();

  const mEnsure = await api('POST', '/recurring/ensure', mTok);
  ok(
    mEnsure.status === 200 && mEnsure.body.code === 0 && mEnsure.body.data.generated === 0,
    '懒补齐对任意登录角色开放（此刻无到期规则 → 0）',
    `status=${mEnsure.status} generated=${mEnsure.body?.data?.generated}`,
  );

  // 2c. 普通成员自助创建月度扣款模板 → 打开应用生成带「自动」标记的流水
  const m = await fillRuleModal(p2, {
    amount: '80',
    rootRe: /^娱乐$/,
    leafRe: /^游戏$/,
    start: '2026-10-01',
    note: '游戏会员',
  });
  ok(m.got, 'member 自助创建「每月 1 日 80 娱乐/游戏」模板成功');
  await sleep(600);
  // 中文值读回后在 JS 侧过滤（避免 Windows 命令行 CJK 编码问题）
  const memberRuleRow = sqlRows('SELECT id, note, created_by FROM recurring_rules')
    .map((l) => {
      const [id, note, created_by] = l.split('\t');
      return { id, note, created_by };
    })
    .find((r) => r.note === '游戏会员');
  const memberId = sql("SELECT id FROM users WHERE username='xiaowang'");
  ok(
    !!memberRuleRow && memberRuleRow.created_by === memberId,
    'member 模板创建人 = xiaowang',
    `rule.created_by=${memberRuleRow?.created_by} memberId=${memberId}`,
  );
  await p2.goto(BASE + '/');
  await p2.waitForLoadState('networkidle').catch(() => {});
  await sleep(1200);
  const mTxn = txnNote('游戏会员');
  ok(
    mTxn.length === 1 && mTxn[0].source === 'recurring' && mTxn[0].amount === '80.00',
    'member 模板生成流水 source=recurring、金额 80.00',
    `count=${mTxn.length}`,
  );
  await p2.goto(BASE + '/records');
  await p2.waitForSelector('.ant-table-tbody tr.ant-table-row', { timeout: 10000 });
  const mTxnRow = p2
    .locator('.ant-table-tbody tr.ant-table-row', { hasText: '游戏会员' })
    .first();
  ok(await mTxnRow.isVisible().catch(() => false), 'member 流水页出现生成的流水行');
  ok((await mTxnRow.locator('.src-tag').count()) > 0, 'member 生成笔带「自动」标记');
  await shot(p2, 'recurring-02b-member-created');
  await ctx2.close();

  // ================= 3. 创建模板：每月1日 2500 银行卡 → 居家/房租 =================
  const a = await fillRuleModal(page, {
    amount: '2500',
    rootRe: /^居家$/,
    leafRe: /^房租$/,
    start: '2026-10-01',
    note: '房租',
  });
  ok(a.pickerOk, '首次生成日期输入生效 2026-10-01');
  ok(a.got, '创建成功提示');
  await sleep(600);
  const ruleRow = page.locator('.rule-row').first();
  const rowText = await ruleRow.innerText();
  ok(
    /2,?500\.00/.test(rowText) &&
      rowText.includes('每月') &&
      rowText.includes('启用') &&
      rowText.includes('居家/房租') &&
      rowText.includes('银行卡'),
    '模板行显示 金额/每月/启用/居家·房租/银行卡',
    rowText.replace(/\n/g, ' | '),
  );
  ok(rowText.includes('2026-10-01'), '新模板下次生成 = 起算日 2026-10-01（到期即生成）', rowText.replace(/\n/g, ' | '));
  await shot(page, 'recurring-03-created');

  // ================= 3b. 归属隔离：admin 看全部含创建人；member 改他人模板 403 =================
  const aList = (await api('GET', '/recurring', tok)).body.data ?? [];
  ok(aList.length === 2, 'admin 列表 = 全部模板（member 1 + admin 1）', `len=${aList.length}`);
  const creatorNames = aList.map((r) => r.created_by_name);
  ok(
    aList.every((r) => !!r.created_by_name) &&
      creatorNames.includes('admin') &&
      creatorNames.includes('xiaowang'),
    'admin 列表每条带创建人姓名（admin/xiaowang）',
    JSON.stringify(creatorNames),
  );
  const mGet2 = await api('GET', '/recurring', mTok);
  ok(
    mGet2.status === 200 && mGet2.body.data.length === 1 && mGet2.body.data[0].note === '游戏会员',
    'member 列表仍只有自己的模板（不含 admin 的房租）',
    `len=${mGet2.body.data?.length} note=${mGet2.body.data?.[0]?.note}`,
  );
  const adminRent = aList.find((r) => r.note === '房租');
  const mPut = await api('PUT', `/recurring/${adminRent?.id}`, mTok, {
    amount: '9999',
    freq: 'monthly',
    account_id: adminRent.account_id,
    category_id: adminRent.category_id,
    start_date: '2026-10-01',
  });
  ok(mPut.status === 403, 'member 改他人（admin）模板 403', `status=${mPut.status}`);
  const rentAfter = (await api('GET', '/recurring', tok)).body.data.find(
    (r) => r.id === adminRent?.id,
  );
  ok(
    Number(rentAfter?.amount) === 2500,
    '越权修改被拒后模板金额不变（2500）',
    `got=${rentAfter?.amount}`,
  );

  // ================= 4. 打开应用懒补齐 → 生成到期流水（spec 场景 1） =================
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(1200);
  let rent = txnNote('房租');
  ok(rent.length === 1, '懒补齐生成 1 笔房租流水', `count=${rent.length}`);
  if (rent.length === 1) {
    ok(
      rent[0].entry_date === '2026-10-01' && rent[0].period_key === '2026-10',
      'entry_date=2026-10-01 且按日期归期 2026-10',
      `${rent[0].entry_date} / ${rent[0].period_key}`,
    );
    ok(
      rent[0].source === 'recurring' && rent[0].amount === '2500.00',
      'source=recurring，金额 2500.00',
      `${rent[0].source} / ${rent[0].amount}`,
    );
  }
  await page.goto(BASE + '/records');
  await page.waitForSelector('.ant-table-tbody tr.ant-table-row', { timeout: 10000 });
  const rentRow = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: '房租' }).first();
  ok(await rentRow.isVisible().catch(() => false), '流水页出现生成的房租行');
  const rentT = await rentRow.innerText().catch(() => '');
  ok(/2,?500\.00/.test(rentT), '流水行金额 2,500.00', rentT.replace(/\n/g, ' | '));
  ok((await rentRow.locator('.src-tag').count()) > 0, '生成笔带「自动」标记');
  await shot(page, 'recurring-04-generated-record');

  await page.goto(BASE + '/recurring');
  await page.waitForSelector('.rule-row');
  const adv = await page.locator('.rule-row', { hasText: '房租' }).first().innerText();
  ok(adv.includes('2026-11-01'), '生成后节奏推进到 2026-11-01', adv.replace(/\n/g, ' | '));

  // ================= 5. 停机跨期补齐 + 幂等（spec 场景 2） =================
  const b = await fillRuleModal(page, {
    amount: '100',
    rootRe: /^居家$/,
    leafRe: /^水电燃气$/,
    start: '2026-08-01',
    note: '水电费',
  });
  ok(b.got, '创建水电费模板成功');
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(1200);
  let util = txnNote('水电费');
  ok(util.length === 3, '跨期一次补齐 3 笔（08/09/10 三期）', `count=${util.length}`);
  ok(
    util.map((t) => t.entry_date).join(',') === '2026-08-01,2026-09-01,2026-10-01',
    '补齐日期 08-01 / 09-01 / 10-01（月锚不漂移）',
    util.map((t) => t.entry_date).join(','),
  );
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(1200);
  util = txnNote('水电费');
  ok(util.length === 3, '再次打开应用不重复生成（幂等）', `count=${util.length}`);
  await shot(page, 'recurring-05-catchup');

  // ================= 6. 生成笔改删独立（spec 场景 3） =================
  await page.goto(BASE + '/records');
  await page.waitForSelector('.ant-table-tbody tr.ant-table-row', { timeout: 10000 });
  const utilRow = page.locator('.ant-table-tbody tr.ant-table-row', { hasText: '水电费' }).first();
  await utilRow.getByRole('button', { name: /编\s*辑/ }).click();
  const em = page.locator('.ant-modal-container', { hasText: '编辑流水' });
  await em.waitFor({ state: 'visible' });
  await em.locator('.amount-row input').fill('120');
  await em.locator('.ant-modal-footer .ant-btn-primary').click();
  ok(await expectMsg(page, /已保存修改/), '修正生成笔 100 → 120 保存成功');
  await sleep(500);
  const amtCount = {};
  for (const t of txnNote('水电费')) amtCount[t.amount] = (amtCount[t.amount] ?? 0) + 1;
  ok(
    amtCount['100.00'] === 2 && amtCount['120.00'] === 1,
    '仅该笔变为 120，其余仍 100（改笔互不影响）',
    JSON.stringify(amtCount),
  );
  const utilRule = rules().find((r) => r.note === '水电费');
  ok(utilRule?.amount === '100.00', '模板金额不受生成笔修改影响（100.00）', `got=${utilRule?.amount}`);

  await utilRow.getByRole('button', { name: /删\s*除/ }).click();
  await page.locator('.ant-popconfirm .ant-btn-primary').click();
  ok(await expectMsg(page, /已删除，余额/), '删除该生成笔成功');
  await sleep(500);
  ok(txnNote('水电费').length === 2, '删除后剩 2 笔', `count=${txnNote('水电费').length}`);
  const afterDel = rules().find((r) => r.note === '水电费');
  ok(afterDel?.status === 'enabled', '删除生成笔后模板保持启用', `status=${afterDel?.status}`);

  // 重写 next_run 到被删期次 → 下一次打开按模板金额重新生成
  sql(`UPDATE recurring_rules SET next_run_date='2026-10-01' WHERE id=${afterDel.id}`);
  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(1200);
  const regen = txnNote('水电费');
  ok(
    regen.length === 3 && regen.every((t) => t.amount === '100.00'),
    '下一周期照常按模板金额重新生成（100 × 3）',
    JSON.stringify(regen.map((t) => `${t.entry_date}:${t.amount}`)),
  );

  // ================= 7. 模板编辑不影响已生成流水；暂停/启用往返 =================
  await page.goto(BASE + '/recurring');
  await page.waitForSelector('.rule-row');
  await page.locator('.rule-row', { hasText: '房租' }).first().getByRole('button', { name: /编\s*辑/ }).click();
  const rm = page.locator('.ant-modal-container', { hasText: '编辑周期模板' });
  await rm.waitFor({ state: 'visible' });
  await rm.locator('.amount-row input').fill('2600');
  await rm.locator('.ant-modal-footer .ant-btn-primary').click();
  ok(await expectMsg(page, /已保存模板/), '编辑模板金额 2500 → 2600 保存成功');
  await sleep(600);
  const rentRule2 = await page.locator('.rule-row', { hasText: '房租' }).first().innerText();
  ok(/2,?600\.00/.test(rentRule2), '模板行金额更新为 2,600.00', rentRule2.replace(/\n/g, ' | '));
  const rentT2 = txnNote('房租');
  ok(
    rentT2.length === 1 && rentT2[0].amount === '2500.00',
    '已生成的房租流水金额不变（模板编辑只影响后续）',
    JSON.stringify(rentT2),
  );

  const pauseBtn = page
    .locator('.rule-row', { hasText: '房租' })
    .first()
    .getByRole('button', { name: /暂\s*停/ });
  await pauseBtn.click();
  ok(await expectMsg(page, /已暂停/), '暂停模板成功提示');
  await sleep(600);
  const st1 = await page
    .locator('.rule-row', { hasText: '房租' })
    .first()
    .locator('.rule-status')
    .innerText();
  ok(st1 === '暂停', '状态标签 = 暂停', `got=${st1}`);
  await shot(page, 'recurring-06-paused');

  await page
    .locator('.rule-row', { hasText: '房租' })
    .first()
    .getByRole('button', { name: /启\s*用/ })
    .click();
  ok(await expectMsg(page, /已启用/), '重新启用成功提示');
  await sleep(600);
  const st2 = await page
    .locator('.rule-row', { hasText: '房租' })
    .first()
    .locator('.rule-status')
    .innerText();
  ok(st2 === '启用', '状态标签回到 启用', `got=${st2}`);

  // ================= 8. 恢复基线 =================
  sql('DELETE FROM recurring_rules');
  sql('DELETE FROM transactions WHERE rule_id IS NOT NULL');
  // 组 10 起：打开应用懒补齐会顺带生成 recurring_patched 提醒，基线一并清空。
  sql('DELETE FROM notifications');
  const roots1 = sql("SELECT COUNT(*) FROM categories WHERE parent_id IS NULL AND kind='expense'");
  ok(roots1 === '8', '基线恢复：支出一级分类 8 个', `got=${roots1}`);
  const exp = sql(
    "SELECT COALESCE(SUM(amount),0) FROM transactions WHERE period_key='2026-10' AND kind='expense'",
  );
  ok(Number(exp) === 922.66, '基线恢复：2026-10 支出回到 922.66', `got=${exp}`);
  ok(sql('SELECT COUNT(*) FROM recurring_rules') === '0', '基线恢复：模板表清空');
  ok(ruleTxns().length === 0, '基线恢复：生成笔清空', `got=${ruleTxns().length}`);
  await page.goto(BASE + '/records');
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(500);
  await shot(page, 'recurring-07-baseline-restored');

  await ctx.close();
  await browser.close();

  const pass = results.filter((r) => r.pass).length;
  console.log(`\n${pass}/${results.length} PASS`);
  if (pass !== results.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error('FATAL', e);
  process.exit(1); // 失败也要退出（否则 browser 进程挂住脚本）
});
