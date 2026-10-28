// Real-source production acceptance. All financial writes belong to a unique
// temporary member ledger. Never attach or alter an existing user's account.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { randomUUID } from 'node:crypto';

const base = process.env.BASE ?? 'https://jizhang.zhanghongyun.cn';
assert.equal(new URL(base).origin, 'https://jizhang.zhanghongyun.cn');
assert.equal(process.env.JZ_PROD_WEALTH_CONFIRM, 'jizhang.zhanghongyun.cn', 'explicit production acceptance flag required');
assert(process.env.JZ_PASS, 'administrator password must be supplied privately');
const started = new Date();
const username = `wealth-deploy-${randomUUID()}`;
const password = randomUUID();
const summary = { started_at: started.toISOString(), site: base, code: 'FBAE68020E', checks: [], cleanup: {} };
let adminToken, token, user, account;
let failed;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
function check(condition, label) { assert(condition, label); summary.checks.push(label); console.log('PASS ' + label); }
async function request(method, pathname, input, bearer = token, timeout = 85_000) {
  const response = await fetch(base + '/api/v1' + pathname, {
    method, headers: { 'content-type': 'application/json', ...(bearer ? { authorization: `Bearer ${bearer}` } : {}) },
    ...(input === undefined ? {} : { body: JSON.stringify(input) }), signal: AbortSignal.timeout(timeout),
  });
  const body = await response.json();
  if (!response.ok || body.code !== 0) throw new Error(`${method} ${pathname}: HTTP ${response.status}, code ${body.code}, ${String(body.message ?? '').slice(0, 160)}`);
  return body.data;
}
function scaled(value, places) {
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(String(value));
  assert(match && (match[3]?.length ?? 0) <= places, 'unexpected decimal format');
  return (match[1] ? -1n : 1n) * BigInt(match[2] + (match[3] ?? '').padEnd(places, '0'));
}
function rounded(numerator, denominator) {
  const sign = numerator < 0n ? -1n : 1n;
  const absolute = numerator < 0n ? -numerator : numerator;
  return sign * ((absolute * 2n + denominator) / (denominator * 2n));
}
function money(cents) {
  const abs = cents < 0n ? -cents : cents;
  return `${cents < 0n ? '-' : ''}${abs / 100n}.${String(abs % 100n).padStart(2, '0')}`;
}

try {
  const login = await request('POST', '/auth/login', { protocol_version: 2, username: process.env.JZ_USER ?? 'admin', password: process.env.JZ_PASS }, null);
  adminToken = login.access_token;
  assert(adminToken, 'administrator login did not return a session');
  const products = await request('GET', '/wealth/products', undefined, adminToken);
  let product = products.find((p) => p.code === summary.code);
  check(product?.registration_code === 'Z7007923000092' && product.admission_status === 'enabled', 'exact Minsheng E product is available');
  product = await request('POST', `/wealth/products/${product.id}/refresh`, {}, adminToken);
  const refreshEnd = Date.now() + 95_000;
  while (product.sync_status === 'refreshing' && Date.now() < refreshEnd) {
    await sleep(3000);
    product = (await request('GET', '/wealth/products', undefined, adminToken)).find((p) => p.code === summary.code);
  }
  check(['updated', 'no_new_nav'].includes(product.sync_status) && !product.last_error && !!product.last_success, 'production native HTTP NAV source succeeds');
  check(new Date(product.last_success).getTime() >= started.getTime() - 90_000, 'NAV source has a fresh production success timestamp');
  const quotes = (await request('GET', `/wealth/products/${product.id}/navs`, undefined, adminToken))
    .filter((q) => q.source === 'automatic').sort((a, b) => a.nav_date.localeCompare(b.nav_date));
  check(quotes.length >= 2, 'complete public NAV history is stored');
  const right = quotes.at(-1), left = quotes.at(-2);
  const baseline = left.nav_date.slice(0, 10), latest = right.nav_date.slice(0, 10);
  assert(baseline < latest, 'NAV dates are not distinct');
  const opening = 1_000_000n;
  const priorNAV = scaled(left.unit_nav, 8), latestNAV = scaled(right.unit_nav, 8);
  const expected = rounded(opening * (latestNAV - priorNAV), priorNAV);
  assert(expected !== 0n, 'latest interval has no nonzero return to verify a ledger posting');
  summary.source = { product_id: product.id, status: product.sync_status, last_success: product.last_success, records: quotes.length, baseline, baseline_nav: left.unit_nav, latest, latest_nav: right.unit_nav };
  user = await request('POST', '/users', { username, password, role: 'member' }, adminToken);
  summary.test_user_id = user.id;
  token = (await request('POST', '/auth/login', { protocol_version: 2, username, password }, null)).access_token;
  const creation = { request_key: randomUUID(), name: `上线理财验证-${started.toISOString().replace(/[:.]/g, '-')}`, type: 'asset', asset_class: 'wealth_cash', opening_balance: money(opening), wealth_cash: { product_id: product.id, start_date: baseline } };
  account = await request('POST', '/accounts', creation);
  assert.equal(account.owner_id, user.id);
  summary.test_account_id = account.id;
  const created = await request('GET', `/wealth/cash-accounts/${account.id}`);
  check(account.asset_class === 'wealth_cash' && created.link?.active && created.link.product_id === product.id && scaled(account.opening_balance, 2) === opening, 'one account request creates the configured wallet and starting balance');
  const replay = await request('POST', '/accounts', creation);
  check(replay.id === account.id && (await request('GET', '/accounts')).filter((a) => a.name === account.name).length === 1, 'production account creation retry does not duplicate the wallet');
  console.log('WAIT existing background scheduler for automatic return; no cash-refresh write is used');
  let view;
  const scheduledEnd = Date.now() + 150_000;
  while (Date.now() < scheduledEnd) {
    view = await request('GET', `/wealth/cash-accounts/${account.id}`);
    if (view.yields.some((y) => y.nav_date.slice(0, 10) === latest && y.txn_id)) break;
    if (['blocked_closed', 'dividend_review'].includes(view.link?.sync_status)) throw new Error(`automatic posting requires review: ${view.link.sync_status}`);
    await sleep(3000);
  }
  const posting = view?.yields.find((y) => y.nav_date.slice(0, 10) === latest);
  check(!!posting?.txn_id && !posting.manual_override && posting.status === 'applied', 'existing production scheduler posts an automatic return');
  check(scaled(posting.amount, 2) === expected, 'posted return matches exact public NAV interval calculation');
  check(scaled(view.account.balance, 2) === opening + expected, 'account balance increases by the adopted return exactly once');
  check(view.link.policy === 'yulibao_20250715', 'YuLibao cash policy is selected automatically');
  const transactions = await request('GET', `/transactions?account_id=${account.id}&page_size=100`);
  const rows = transactions.items.filter((t) => t.wealth_cash_yield_id === posting.id);
  check(rows.length === 1 && rows[0].id === posting.txn_id && rows[0].source === 'wealth_auto', 'one managed return appears in the ordinary ledger');
  check(rows[0].purpose === 'wealth_cash_yield' && scaled(rows[0].balance_effect, 2) === expected && scaled(rows[0].expense_effect, 2) === 0n, 'ledger balance and consumption effects are correct');
  const beforeAudits = view.audits.length;
  const repeated = await request('POST', `/wealth/cash-accounts/${account.id}/refresh`, {});
  check(scaled(repeated.account.balance, 2) === opening + expected && repeated.yields.length === view.yields.length && repeated.audits.length === beforeAudits, 'repeat refresh is financially idempotent');
  const ordinary = (await request('GET', '/accounts')).find((a) => a.id === account.id);
  const independent = await request('GET', '/wealth/accounts');
  check(scaled(ordinary.balance, 2) === opening + expected && !independent.some((a) => a.account.id === account.id), 'cash and wealth views share one asset');
  summary.posting = { baseline, latest, opening: money(opening), expected_return: money(expected), actual_return: posting.amount, balance: repeated.account.balance, yield_id: posting.id, transaction_id: posting.txn_id, scheduled_at: view.link.last_reconciled_at, repeated_without_duplicate: true };
} catch (error) {
  failed = error;
  summary.error = String(error).replaceAll(password, '<redacted>').replaceAll(process.env.JZ_PASS, '<redacted>').slice(0, 500);
} finally {
  if (account && token) {
    try {
      const current = await request('GET', `/wealth/cash-accounts/${account.id}`);
      if (current.link?.active) await request('PUT', `/wealth/cash-accounts/${account.id}`, { request_key: randomUUID(), expected_revision: current.link.revision, product_id: current.link.product_id, start_date: current.link.start_date.slice(0, 10), active: false });
      summary.cleanup.link_stopped = true;
      await request('PUT', `/accounts/${account.id}`, { name: account.name, type: 'asset', asset_class: 'cash', opening_balance: money(1_000_000n), status: 'disabled' });
      summary.cleanup.account_disabled = true;
    } catch (error) { failed ??= error; summary.cleanup.account_error = String(error).slice(0, 200); }
  }
  if (user && adminToken) {
    try {
      await request('PATCH', `/users/${user.id}/status`, { status: 'disabled' }, adminToken);
      summary.cleanup.user_disabled = true;
      if (token) {
        const revoked = await fetch(base + '/api/v1/accounts', { headers: { authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(15_000) });
        assert.equal(revoked.status, 401, 'disabled test user session still works');
        summary.cleanup.session_revoked = true;
      }
    } catch (error) { failed ??= error; summary.cleanup.user_error = String(error).slice(0, 200); }
  }
  summary.completed_at = new Date().toISOString();
  summary.result = failed ? 'failed' : 'passed';
  if (process.env.WEALTH_PROD_EVIDENCE) fs.writeFileSync(process.env.WEALTH_PROD_EVIDENCE, JSON.stringify(summary, null, 2) + '\n');
}
if (failed) { console.error('PROD_WEALTH: FAILED ' + (summary.error ?? String(failed).slice(0, 200))); process.exitCode = 1; }
else console.log(`PROD_WEALTH: ALL_GREEN ${summary.checks.length} checks; test ledger stopped and session revoked`);
