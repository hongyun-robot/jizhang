/* eslint-disable no-console */
/**
 * 视觉自检脚本：不依赖后端，用 Playwright 拦截 /api/v1/** 返回拟真数据，
 * 对各页面在 多视口 × 亮/暗 主题 下截图，供设计走查（排版/留白/层级/色彩/动效）。
 *
 * 用法：npm run dev 起 :5600 后
 *   node e2e/preview.mjs                # 全部页面
 *   node e2e/preview.mjs dash records    # 指定页面
 * 截图落在 e2e/preview/ 下。
 */
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(__dirname, 'preview');
fs.rmSync(OUT, { recursive: true, force: true });
fs.mkdirSync(OUT, { recursive: true });

const EXE =
  process.env.CHROME_EXE ??
  path.join(process.env.LOCALAPPDATA, 'ms-playwright', 'chromium-1208', 'chrome-win64', 'chrome.exe');
const BASE = process.env.BASE ?? 'http://localhost:5600';

const ok = (data) => ({ code: 0, message: 'ok', data });

// ---------- 拟真数据 ----------

const CATS = {
  expense: [
    { id: 1, name: '餐饮', children: [
      { id: 11, name: '早餐' }, { id: 12, name: '午餐' },
      { id: 13, name: '晚餐' }, { id: 14, name: '外卖' },
    ] },
    { id: 2, name: '交通', children: [
      { id: 21, name: '地铁公交' }, { id: 22, name: '打车' }, { id: 23, name: '加油' },
    ] },
    { id: 3, name: '居住', children: [
      { id: 31, name: '房租' }, { id: 32, name: '物业' }, { id: 33, name: '水电燃气' },
    ] },
    { id: 4, name: '购物', children: [
      { id: 41, name: '日用品' }, { id: 42, name: '服饰' }, { id: 43, name: '数码' },
    ] },
    { id: 5, name: '教育', children: [{ id: 51, name: '学费' }, { id: 52, name: '绘本' }] },
    { id: 6, name: '医疗健康', children: [{ id: 61, name: '门诊' }, { id: 62, name: '药品' }] },
    { id: 7, name: '人情往来', children: [{ id: 71, name: '随礼' }] },
  ],
  income: [
    { id: 101, name: '工资', children: [{ id: 111, name: '月薪' }, { id: 112, name: '奖金' }] },
    { id: 102, name: '理财收益', children: [{ id: 113, name: '利息' }, { id: 114, name: '分红' }] },
    { id: 103, name: '其他', children: [{ id: 115, name: '退款' }] },
  ],
};

const CAT_NAME = new Map();
for (const g of Object.values(CATS)) {
  for (const p of g) {
    CAT_NAME.set(p.id, p.name);
    for (const c of p.children ?? []) CAT_NAME.set(c.id, `${p.name} / ${c.name}`);
  }
}

const ACCOUNTS = [
  { id: 1, name: '招商储蓄卡', type: 'asset', opening_balance: '120000.00', sort: 1, status: 'active', balance: '132480.55' },
  { id: 2, name: '微信零钱', type: 'asset', opening_balance: '2000.00', sort: 2, status: 'active', balance: '3126.80' },
  { id: 3, name: '支付宝', type: 'asset', opening_balance: '1500.00', sort: 3, status: 'active', balance: '2841.36' },
  { id: 4, name: '宝宝教育金', type: 'asset', opening_balance: '30000.00', sort: 4, status: 'active', balance: '52600.00' },
  { id: 5, name: '信用卡', type: 'liability', opening_balance: '0.00', repayment_day: 8, sort: 5, status: 'active', balance: '-4286.17' },
];

const NOTES_EXPENSE = [
  ['晚餐 · 番茄牛腩', 13, 128.5], ['地铁通勤', 21, 6], ['瑞幸生椰拿铁', 12, 19.9],
  ['超市采购 · 周末', 41, 246.3], ['外卖 · 黄焖鸡', 14, 32], ['加油 · 92#', 23, 300],
  ['绘本 ×3', 52, 88], ['门诊挂号', 61, 50], ['水电燃气 · 9月', 33, 286.4],
  ['打车回家', 22, 43.6], ['儿童乐园门票', 41, 120], ['随礼 · 同事结婚', 71, 600],
  ['午餐 · 公司食堂', 12, 22], ['日用纸巾', 41, 39.9], ['早餐 · 豆浆油条', 11, 8.5],
  ['房租 · 10月', 31, 4800], ['物业费', 32, 320], ['钢琴课', 51, 480],
  ['药品 · 感冒灵', 62, 26.8], ['服饰 · 秋装', 42, 359],
];
const NOTES_INCOME = [
  ['月薪', 111, 18600], ['季度奖金', 112, 6000], ['理财分红', 114, 342.7], ['退款 · 退货', 115, 89],
];

function dayStr(d) {
  const dt = new Date(2026, 9, d);
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`;
}

function buildTxns() {
  const rows = [];
  let id = 1000;
  const push = (o) =>
    rows.push({
      id: id++,
      created_by: 1,
      source: 'manual',
      created_at: `${o.entry_date}T12:00:00+08:00`,
      updated_at: `${o.entry_date}T12:00:00+08:00`,
      ...o,
    });

  // 10 月：1–25 日每天 2–3 笔支出，让统计图表有真实节奏
  for (let d = 1; d <= 25; d++) {
    const n = 2 + (d % 2);
    for (let i = 0; i < n; i++) {
      const [note, cid, amt] = NOTES_EXPENSE[(d * 3 + i * 5) % NOTES_EXPENSE.length];
      push({
        kind: 'expense',
        amount: (amt * (1 + ((d + i) % 3) * 0.15)).toFixed(2),
        account_id: (i % 3) + 1,
        category_id: cid,
        entry_date: dayStr(d),
        period_key: '2026-10',
        note,
        source: i === 0 && d % 4 === 0 ? 'recurring' : 'manual',
      });
    }
    if (d === 1 || d === 12) {
      const [note, cid, amt] = NOTES_INCOME[d === 1 ? 0 : 1];
      push({
        kind: 'income',
        amount: amt.toFixed(2),
        account_id: 1,
        category_id: cid,
        entry_date: dayStr(d),
        period_key: '2026-10',
        note,
      });
    }
    if (d === 6) {
      push({
        kind: 'income',
        amount: '342.70',
        account_id: 3,
        category_id: 114,
        entry_date: dayStr(d),
        period_key: '2026-10',
        note: '理财分红',
      });
    }
  }

  // 9 月：给「上期对比 / 账期汇总」一份可比数据
  for (let d = 4; d <= 28; d++) {
    const [note, cid, amt] = NOTES_EXPENSE[d % NOTES_EXPENSE.length];
    push({
      kind: 'expense',
      amount: (amt * (1 + (d % 4) * 0.2)).toFixed(2),
      account_id: (d % 3) + 1,
      category_id: cid,
      entry_date: `2026-09-${String(d).padStart(2, '0')}`,
      period_key: '2026-09',
      note,
    });
    if (d === 1) {
      push({
        kind: 'income',
        amount: '18600.00',
        account_id: 1,
        category_id: 111,
        entry_date: '2026-09-01',
        period_key: '2026-09',
        note: '月薪',
      });
    }
  }

  push({
    kind: 'transfer',
    amount: '2000.00',
    account_id: 1,
    transfer_account_id: 4,
    entry_date: '2026-10-05',
    period_key: '2026-10',
    note: '转入教育金',
    created_at: '2026-10-05T20:00:00+08:00',
    updated_at: '2026-10-05T20:00:00+08:00',
  });

  return rows.sort((a, b) => (a.entry_date < b.entry_date ? 1 : a.entry_date > b.entry_date ? -1 : b.id - a.id));
}
const TXNS = buildTxns();

function filterTxns(sp) {
  let list = TXNS;
  const kind = sp.get('kind');
  if (kind) list = list.filter((t) => t.kind === kind);
  const kw = sp.get('keyword');
  if (kw) list = list.filter((t) => (t.note ?? '').includes(kw));
  const acct = sp.get('account_id');
  if (acct) list = list.filter((t) => t.account_id === Number(acct) || t.transfer_account_id === Number(acct));
  const cat = sp.get('category_id');
  if (cat) list = list.filter((t) => Math.floor(t.category_id / 10) === Number(cat) || t.category_id === Number(cat));
  const start = sp.get('start_date');
  if (start) list = list.filter((t) => t.entry_date >= start);
  const end = sp.get('end_date');
  if (end) list = list.filter((t) => t.entry_date <= end);
  const pk = sp.get('period_key');
  if (pk) list = list.filter((t) => t.period_key === pk);
  const amin = sp.get('amount_min');
  if (amin) list = list.filter((t) => Number(t.amount) >= Number(amin));
  const amax = sp.get('amount_max');
  if (amax) list = list.filter((t) => Number(t.amount) <= Number(amax));
  const page = Number(sp.get('page') ?? 1);
  const size = Number(sp.get('page_size') ?? 20);
  return { total: list.length, page, page_size: size, items: list.slice((page - 1) * size, page * size) };
}

const sum = (rows, kind) =>
  rows.filter((r) => r.kind === kind).reduce((s, r) => s + Number(r.amount), 0).toFixed(2);

const OCT = TXNS.filter((t) => t.period_key === '2026-10');
const OCT_INCOME = sum(OCT, 'income');
const OCT_EXPENSE = sum(OCT, 'expense');
const OCT_BALANCE = (Number(OCT_INCOME) - Number(OCT_EXPENSE)).toFixed(2);

const COMPARE = {
  start: '2026-10-01', end: '2026-10-31', previous_start: '2026-09-01', previous_end: '2026-09-30',
  current: { income: OCT_INCOME, expense: OCT_EXPENSE, balance: OCT_BALANCE },
  previous: { income: '22600.00', expense: '16184.32', balance: '6415.68' },
  change: { income_percent: 4.6, expense_percent: -8.4, balance_percent: 21.7 },
};

function shareOf(kind) {
  const rows = OCT.filter((t) => t.kind === kind);
  const total = rows.reduce((s, r) => s + Number(r.amount), 0);
  const byRoot = new Map();
  for (const r of rows) {
    const root = Math.floor(r.category_id / 10) * 10;
    // 一级分类 id 本身就是 1..7 / 101..103
    const rootId = r.category_id >= 100 ? (r.category_id < 110 ? 101 : r.category_id < 120 ? 102 : 103) : (r.category_id < 20 ? 1 : r.category_id < 30 ? 2 : r.category_id < 40 ? 3 : r.category_id < 50 ? 4 : r.category_id < 60 ? 5 : r.category_id < 70 ? 6 : 7);
    void root;
    byRoot.set(rootId, (byRoot.get(rootId) ?? 0) + Number(r.amount));
  }
  const items = [...byRoot.entries()]
    .map(([id, amount]) => ({ id, name: CAT_NAME.get(id) ?? '其他', amount: amount.toFixed(2), percent: total ? Math.round((amount / total) * 1000) / 10 : 0 }))
    .sort((a, b) => Number(b.amount) - Number(a.amount));
  return { total: total.toFixed(2), items };
}

const SHARE = { start: '2026-10-01', end: '2026-10-31', expense: shareOf('expense'), income: shareOf('income') };

const TREND = {
  granularity: 'day', start: '2026-10-01', end: '2026-10-31',
  points: Array.from({ length: 31 }, (_, i) => {
    const d = i + 1;
    const day = `2026-10-${String(d).padStart(2, '0')}`;
    const rows = TXNS.filter((t) => t.entry_date === day);
    const income = rows.filter((r) => r.kind === 'income').reduce((s, r) => s + Number(r.amount), 0);
    const expense = rows.filter((r) => r.kind === 'expense').reduce((s, r) => s + Number(r.amount), 0);
    return {
      key: day, label: `${d}日`,
      income: income.toFixed(2), expense: expense.toFixed(2),
      balance: (income - expense).toFixed(2),
    };
  }),
};

const PERIODS = {
  year: 2026,
  periods: [
    { period_key: '2026-10', income: OCT_INCOME, expense: OCT_EXPENSE, balance: OCT_BALANCE },
    { period_key: '2026-09', income: '22600.00', expense: '16184.32', balance: '6415.68' },
    { period_key: '2026-08', income: '21800.00', expense: '18940.75', balance: '2859.25' },
    { period_key: '2026-07', income: '24300.00', expense: '17260.40', balance: '7039.60' },
    { period_key: '2026-06', income: '21800.00', expense: '15480.00', balance: '6320.00' },
    { period_key: '2026-05', income: '23150.00', expense: '19720.30', balance: '3429.70' },
  ],
};

const ASSETS = {
  start: '2026-10-01', end: '2026-10-31',
  points: Array.from({ length: 31 }, (_, i) => ({
    date: `2026-10-${String(i + 1).padStart(2, '0')}`,
    total: (186000 + i * 420 + Math.sin(i / 3) * 1800).toFixed(2),
    accounts: ACCOUNTS.filter((a) => a.type === 'asset').map((a) => ({
      account_id: a.id, name: a.name, type: a.type,
      balance: (Number(a.balance) / 31 * (i + 1)).toFixed(2),
    })),
  })),
  ending_total: '186750.34',
};

const BUDGETS = {
  period_key: '2026-10',
  total: { budget: '50000.00', consumed: OCT_EXPENSE, percent: 74, over: false, over_amount: '0.00' },
  categories: [
    { category_id: 1, category_name: '餐饮', budget: '4500.00', consumed: '4128.50', percent: 92, over: false, over_amount: '0.00' },
    { category_id: 2, category_name: '交通', budget: '1200.00', consumed: '1349.60', percent: 100, over: true, over_amount: '149.60' },
    { category_id: 3, category_name: '居住', budget: '5500.00', consumed: '5406.40', percent: 98, over: false, over_amount: '0.00' },
    { category_id: 4, category_name: '购物', budget: '2000.00', consumed: '765.20', percent: 38, over: false, over_amount: '0.00' },
    { category_id: 5, category_name: '教育', budget: '1800.00', consumed: '480.00', percent: 27, over: false, over_amount: '0.00' },
  ],
};

const NOTIFS = {
  unread: 3,
  list: [
    { id: 1, kind: 'budget_overdue', read_at: null, created_at: '2026-10-08T08:00:00+08:00',
      payload: JSON.stringify({ period_key: '2026-10', scope: 'category', category_name: '交通', budget: '1200.00', consumed: '1349.60', over_amount: '149.60' }) },
    { id: 2, kind: 'repayment_due', read_at: null, created_at: '2026-10-07T08:00:00+08:00',
      payload: JSON.stringify({ account_id: 5, account_name: '信用卡', debt: '4286.17', repayment_date: '2026-10-08', days: 1, overdue: false }) },
    { id: 3, kind: 'budget_overdue', read_at: null, created_at: '2026-10-01T08:00:00+08:00',
      payload: JSON.stringify({ period_key: '2026-09', scope: 'total', budget: '16000.00', consumed: '16184.32', over_amount: '184.32' }) },
    { id: 4, kind: 'recurring_patched', read_at: '2026-09-30T08:05:00+08:00', created_at: '2026-09-30T08:00:00+08:00',
      payload: JSON.stringify({ generated: 2 }) },
  ],
};

const RULES = [
  { id: 1, amount: '4800.00', freq: 'monthly', account_id: 1, category_id: 31, note: '房租', start_date: '2026-01-05', next_run_date: '2026-11-05', status: 'enabled', created_by: 1, created_by_name: 'admin', created_at: '2026-01-05T10:00:00+08:00', updated_at: '2026-01-05T10:00:00+08:00' },
  { id: 2, amount: '18600.00', freq: 'monthly', account_id: 1, category_id: 111, note: '月薪', start_date: '2026-01-10', next_run_date: '2026-11-10', status: 'enabled', created_by: 1, created_by_name: 'admin', created_at: '2026-01-10T10:00:00+08:00', updated_at: '2026-01-10T10:00:00+08:00' },
  { id: 3, amount: '19.90', freq: 'daily', account_id: 2, category_id: 12, note: '咖啡', start_date: '2026-03-01', next_run_date: '2026-10-11', status: 'paused', created_by: 2, created_by_name: 'xiaowang', created_at: '2026-03-01T08:00:00+08:00', updated_at: '2026-09-01T08:00:00+08:00' },
  { id: 4, amount: '320.00', freq: 'yearly', account_id: 1, category_id: 32, note: '物业费', start_date: '2026-01-20', next_run_date: '2027-01-20', status: 'enabled', created_by: 1, created_by_name: 'admin', created_at: '2026-01-20T10:00:00+08:00', updated_at: '2026-01-20T10:00:00+08:00' },
];

const USERS = [
  { id: 1, username: 'admin', role: 'admin', status: 'active', created_at: '2026-01-01T00:00:00+08:00', updated_at: '2026-01-01T00:00:00+08:00' },
  { id: 2, username: 'xiaowang', role: 'recorder', status: 'active', created_at: '2026-01-12T00:00:00+08:00', updated_at: '2026-02-01T00:00:00+08:00' },
  { id: 3, username: 'dazhi', role: 'member', status: 'active', created_at: '2026-02-20T00:00:00+08:00', updated_at: '2026-02-20T00:00:00+08:00' },
  { id: 4, username: 'xiaoli', role: 'viewer', status: 'active', created_at: '2026-03-03T00:00:00+08:00', updated_at: '2026-03-03T00:00:00+08:00' },
  { id: 5, username: 'grandma', role: 'viewer', status: 'disabled', created_at: '2026-04-11T00:00:00+08:00', updated_at: '2026-06-01T00:00:00+08:00' },
];

const AUDIT = {
  total: 12, page: 1, page_size: 20,
  items: Array.from({ length: 8 }, (_, i) => ({
    id: 8 - i, user_id: 1, username: 'admin', action: i % 3 === 1 ? 'period.reopen' : 'period.close',
    period_key: `2026-${String(9 - (i % 4)).padStart(2, '0')}`,
    created_at: `2026-0${6 + (i % 4)}-1${i % 9}T14:30:00+08:00`,
  })),
};

const USER = { id: 1, username: 'admin', role: 'admin', status: 'active' };

// ---------- 路由分发 ----------

function handler(url, method, postBody) {
  const u = new URL(url);
  const p = u.pathname.replace(/^.*\/api\/v1/, '');
  const sp = u.searchParams;

  if (p === '/auth/login' && method === 'POST') {
    if (!postBody?.username || !postBody?.password) return { status: 400, body: { code: 400, message: '请输入用户名和密码' } };
    return { body: ok({ access_token: 'mock-token', token_type: 'Bearer', expires_in: 900, user: USER }) };
  }
  if (p === '/auth/refresh') return { body: ok({ access_token: 'mock-token', token_type: 'Bearer', expires_in: 900 }) };
  if (p === '/auth/me') return { body: ok(USER) };
  if (p === '/accounts') return { body: ok(ACCOUNTS) };
  if (p === '/categories')
    return {
      body: ok([
        { id: 0, kind: 'expense', name: '支出', children: CATS.expense.map((r) => ({ ...r, kind: 'expense' })) },
        { id: 0, kind: 'income', name: '收入', children: CATS.income.map((r) => ({ ...r, kind: 'income' })) },
      ]),
    };
  if (p === '/transactions' && method === 'GET') return { body: ok(filterTxns(sp)) };
  if (p === '/transactions/export') return { status: 200, body: 'id,kind,amount\n', headers: { 'content-disposition': 'attachment; filename="transactions.csv"' } };
  if (p.startsWith('/stats/category')) return { body: ok(SHARE) };
  if (p.startsWith('/stats/trend')) return { body: ok(TREND) };
  if (p.startsWith('/stats/compare')) return { body: ok(COMPARE) };
  if (p.startsWith('/stats/periods')) return { body: ok(PERIODS) };
  if (p.startsWith('/stats/assets')) return { body: ok(ASSETS) };
  if (p.startsWith('/budgets')) return { body: ok(BUDGETS) };
  if (p.startsWith('/notifications')) return { body: ok(NOTIFS) };
  if (p.startsWith('/recurring')) return { body: ok(method === 'POST' ? { generated: 0 } : RULES) };
  if (p.startsWith('/users')) return { body: ok(USERS) };
  if (p.startsWith('/settings')) return { body: ok({ settlement_day: 1 }) };
  if (p.startsWith('/audit-logs')) return { body: ok(AUDIT) };
  if (p.startsWith('/closings/preview')) {
    // 按所选账期回填区间与收支，避免「选 09 月却显示 10 月」的假数据
    const key = sp.get('period_key') ?? '2026-10';
    const [y, m] = key.split('-').map((n) => Number(n));
    const s = PERIODS.periods.find((x) => x.period_key === key);
    const lastDay = y && m ? new Date(y, m, 0).getDate() : 31;
    return {
      body: ok({
        period_key: key,
        start_date: `${key}-01`,
        end_date: `${key}-${String(lastDay).padStart(2, '0')}`,
        closed: false,
        ended: key < '2026-10',
        income: s?.income ?? '0.00',
        expense: s?.expense ?? '0.00',
        balance: s?.balance ?? '0.00',
      }),
    };
  }
  return { body: ok(null) };
}

// ---------- 页面清单 ----------

const PAGES = [
  { key: 'dash', path: '/' },
  { key: 'records', path: '/records' },
  { key: 'stats', path: '/stats' },
  { key: 'mine', path: '/mine' },
  { key: 'budgets', path: '/budgets' },
  { key: 'notifications', path: '/notifications' },
  { key: 'recurring', path: '/recurring' },
  { key: 'accounts', path: '/accounts' },
  { key: 'categories', path: '/categories' },
  { key: 'settings', path: '/settings' },
  { key: 'audit', path: '/audit' },
  // 叠加态：记账弹窗（在同一路径上打开，再截图）
  {
    key: 'txnform',
    path: '/records',
    open: async (page) => {
      await page
        .getByRole('button', { name: /记一笔|快捷记账/ })
        .first()
        .click()
        .catch(() => {});
      await page.locator('.ant-modal').first().waitFor({ timeout: 8000 }).catch(() => {});
    },
  },
];

const VIEWPORTS = [
  { key: 'desktop', width: 1440, height: 900 },
  { key: 'laptop', width: 1180, height: 800 },
  { key: 'mobile', width: 390, height: 844, isMobile: true, hasTouch: true },
  { key: 'narrow', width: 360, height: 780, isMobile: true, hasTouch: true },
];

async function mock(page) {
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request();
    let body;
    try {
      body = req.postDataJSON();
    } catch {
      body = null;
    }
    // 略微延迟，让骨架屏/入场动画进入稳态
    await new Promise((r) => setTimeout(r, 40));
    const res = handler(req.url(), req.method(), body);
    await route.fulfill({
      status: res.status ?? 200,
      contentType: res.headers?.['content-type'] ?? 'application/json',
      headers: res.headers,
      body: typeof res.body === 'string' ? res.body : JSON.stringify(res.body),
    });
  });
}

async function main() {
  const only = process.argv.slice(2);
  const pages = only.length ? PAGES.filter((p) => only.includes(p.key)) : PAGES;
  const browser = await chromium.launch({ executablePath: EXE, headless: true });

  for (const vp of VIEWPORTS) {
    if (only.length === 0 && (vp.key === 'laptop' || vp.key === 'narrow')) continue;
    for (const theme of ['light', 'dark']) {
      if (theme === 'dark' && vp.key !== 'desktop' && vp.key !== 'mobile') continue;
      const ctx = await browser.newContext({
        viewport: { width: vp.width, height: vp.height },
        isMobile: vp.isMobile,
        hasTouch: vp.hasTouch,
        locale: 'zh-CN',
        deviceScaleFactor: 2,
      });
      await ctx.addInitScript((t) => localStorage.setItem('jizhang-theme', t), theme);
      const page = await ctx.newPage();
      await mock(page);

      // 登录（走 UI，验证登录页本身）
      await page.goto(`${BASE}/login`);
      await page.waitForSelector('input[placeholder="用户名"]');
      await page.screenshot({ path: path.join(OUT, `${vp.key}-${theme}-login.png`), fullPage: true });
      await page.fill('input[placeholder="用户名"]', 'admin');
      await page.fill('input[placeholder="密码"]', 'admin123');
      await page.getByRole('button', { name: /登\s*录/ }).click();
      await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 15000 });
      await page.locator('.page-head').first().waitFor({ timeout: 15000 }).catch(() => {});

      for (const pg of pages) {
        await page.goto(BASE + pg.path);
        await page.waitForTimeout(1400); // 入场动画 + 图表渲染
        if (pg.open) {
          await pg.open(page);
          await page.waitForTimeout(700);
        }
        await page.screenshot({
          path: path.join(OUT, `${vp.key}-${theme}-${pg.key}.png`),
          fullPage: true,
        });
      }
      await ctx.close();
    }
  }

  await browser.close();
  console.log(`screenshots → ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
