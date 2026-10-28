import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { assertFixture, launchBrowser, login, pageRequest } from './support/fixture.mjs';

const base = process.env.AUTH_E2E_BASE ?? 'http://127.0.0.1:5601';
await assertFixture(base);
const browser = await launchBrowser('chromium');
const shots = path.resolve('e2e/shots/multi-device');
fs.mkdirSync(shots, { recursive: true });
const errors = [];
const contexts = [];
async function capture(page, name) {
  await page.waitForTimeout(750);
  await page.locator('body').evaluate(async (element) => {
    await Promise.all(element.getAnimations({ subtree: true }).filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity).map((animation) => animation.finished.catch(() => {})));
  });
  await page.screenshot({ path: path.join(shots, name), fullPage: true });
}
const snapshot = (page) => page.evaluate(async () => {
  const url=performance.getEntriesByType('resource').map((entry)=>entry.name).find((name)=>new URL(name).pathname.endsWith('/src/stores/auth.ts')) ?? '/src/stores/auth.ts';
  const state = (await import(url)).useAuth.getState();
  return { token: state.token, sessionID: state.sessionID, username: state.user?.username, deviceType: state.deviceType };
});
async function device(kind = 'desktop', username = 'voucher-member', init) {
  const options = kind === 'phone' ? { viewport: { width: 390, height: 844 }, userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1', hasTouch: true, isMobile: true }
    : kind === 'tablet' ? { viewport: { width: 1024, height: 768 }, userAgent: 'Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1', hasTouch: true }
      : { viewport: { width: 1280, height: 900 } };
  const context = await browser.newContext(options); contexts.push(context);
  if (init) await context.addInitScript(init);
  context.on('page', (p) => p.on('pageerror', (error) => errors.push(error.message)));
  const page = await context.newPage();
  await login(page, base, username);
  return { context, page };
}
const authorized = async (context, token) => (await context.request.get(base + '/api/v1/auth/me', { headers: { Authorization: 'Bearer ' + token } })).status();
const invoke = (page, action) => page.evaluate(async (action) => {
  const moduleURL=(path)=>performance.getEntriesByType('resource').map((entry)=>entry.name).find((name)=>new URL(name).pathname.endsWith(path)) ?? path;
  const { session } = await import(moduleURL('/src/auth/session.ts'));
  const { useAuth } = await import(moduleURL('/src/stores/auth.ts'));
  if (action === 'refresh') return session.refreshAfter401(useAuth.getState().token);
  if (action === 'logout') return session.logout();
  return session.login({ username: action, password: 'voucher-test-only' });
}, action);

try {
  const phone = await device('phone');
  const tablet = await device('tablet');
  const desktop = await device();
  const viewer = await device('desktop', 'voucher-viewer');
  const oldPhone = await snapshot(phone.page);
  for (const [expected, instance] of [['phone', phone], ['tablet', tablet], ['desktop', desktop]]) {
    const state = await snapshot(instance.page);
    assert.equal(state.deviceType, expected);
    assert.equal(await authorized(instance.context, state.token), 200);
  }
  const replacement = await device('phone');
  const nextPhone = await snapshot(replacement.page);
  assert.equal(await authorized(phone.context, oldPhone.token), 401);
  const staleLogout = await phone.context.request.post(base + '/api/v1/auth/logout', { data: { protocol_version: 2, session_id: oldPhone.sessionID }, headers: { Authorization: 'Bearer ' + oldPhone.token } });
  assert.equal(staleLogout.status(), 401);
  assert.equal(staleLogout.headers()['set-cookie'], undefined);
  assert.equal(await authorized(replacement.context, nextPhone.token), 200);
  assert.equal((await pageRequest(phone.page, 'GET', '/accounts')).status, 401);
  await phone.page.waitForURL(/\/login$/);
  await phone.page.getByText('账号已在另一台同类设备登录，请重新登录', { exact: true }).waitFor();
  await capture(phone.page, 'phone-replaced.png');
  for (const instance of [tablet, desktop, viewer]) assert.equal(await authorized(instance.context, (await snapshot(instance.page)).token), 200);
  const failed = await replacement.context.request.post(base + '/api/v1/auth/login', { data: { protocol_version: 2, username: 'voucher-member', password: 'incorrect' } });
  assert.equal(failed.status(), 401);
  assert.equal(await authorized(replacement.context, nextPhone.token), 200);
  console.log('PASS phone/tablet/desktop coexist; same-type replacement; stale logout and failed login isolation');

  const tab = await desktop.context.newPage();
  await tab.goto(base + '/mine'); await tab.getByRole('button', { name: '退出所有设备' }).waitFor();
  assert.equal((await snapshot(tab)).sessionID, (await snapshot(desktop.page)).sessionID);
  let refreshes = 0;
  const countRefresh = (request) => { if (request.url().endsWith('/auth/refresh')) refreshes++; };
  desktop.context.on('request', countRefresh);
  const rejected = new Set();
  await desktop.context.route('**/api/v1/accounts?auth_retry=1', async (route) => {
    const key = route.request().frame().page();
    if (!rejected.has(key)) { rejected.add(key); await route.fulfill({ status: 401, json: { code: 10007, message: '测试令牌过期' } }); }
    else await route.continue();
  });
  const results = await Promise.all([pageRequest(desktop.page, 'GET', '/accounts?auth_retry=1'), pageRequest(tab, 'GET', '/accounts?auth_retry=1')]);
  assert(results.every((reply) => reply.status === 200), JSON.stringify(results));
  assert.equal(refreshes, 1);
  await desktop.context.unroute('**/api/v1/accounts?auth_retry=1');
  desktop.context.off('request', countRefresh);
  console.log('PASS two tabs share session and coalesce simultaneous 401 refreshes');

  let writes = 0;
  const countWrites = (request) => { if (request.url().endsWith('/transactions') && request.method() === 'POST') writes++; };
  desktop.context.on('request', countWrites);
  await desktop.page.route('**/api/v1/auth/me', (route) => route.fulfill({ status: 503, json: { code: 20001, message: '测试认证暂不可用' } }));
  await desktop.page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await desktop.page.getByText('测试认证暂不可用', { exact: true }).waitFor();
  const blocked = await pageRequest(desktop.page, 'POST', '/transactions', { kind: 'expense', amount: '1' });
  assert.equal(blocked.status, 503); assert.equal(writes, 0);
  assert.equal((await snapshot(desktop.page)).username, 'voucher-member');
  await desktop.page.unroute('**/api/v1/auth/me');
  await desktop.page.evaluate(async () => {
    const url=performance.getEntriesByType('resource').map((entry)=>entry.name).find((name)=>new URL(name).pathname.endsWith('/src/auth/session.ts')) ?? '/src/auth/session.ts';
    return (await import(url)).session.validate();
  });
  desktop.context.off('request', countWrites);
  await desktop.page.goto(base + '/mine');
  await desktop.page.getByRole('button', { name: '退出所有设备' }).waitFor();
  await capture(desktop.page, 'desktop-logout.png');
  const all = await Promise.all([replacement, tablet, desktop].map(async (instance) => ({ ...instance, ...(await snapshot(instance.page)) })));
  const viewerToken = (await snapshot(viewer.page)).token;
  await desktop.page.getByRole('button', { name: '退出所有设备' }).click();
  await Promise.all([desktop.page.waitForURL(/\/login$/), tab.waitForURL(/\/login$/)]);
  for (const instance of all) assert.equal(await authorized(instance.context, instance.token), 401);
  assert.equal(await authorized(viewer.context, viewerToken), 200);
  await replacement.page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await replacement.page.waitForURL(/\/login$/);
  console.log('PASS foreground verification blocks writes on 503; explicit logout revokes every type and synchronizes tabs');

  const race = await device('desktop', 'voucher-recorder');
  const raceTab = await race.context.newPage();
  await raceTab.goto(base + '/mine'); await raceTab.getByRole('button', { name: '退出所有设备' }).waitFor();
  for (const operation of ['refresh', 'logout']) {
    let release;
    let started;
    const held = new Promise((resolve) => { release = resolve; });
    const ready = new Promise((resolve) => { started = resolve; });
    await race.context.route('**/api/v1/auth/' + operation, async (route) => {
      const response = await route.fetch(); assert.equal(response.status(), 200);
      started(); await held; await route.fulfill({ response });
    });
    let logins = 0;
    const count = (request) => { if (request.url().endsWith('/auth/login')) logins++; };
    race.context.on('request', count);
    const previous = invoke(race.page, operation);
    await ready;
    const signing = invoke(raceTab, 'voucher-recorder');
    await new Promise((resolve) => setTimeout(resolve, 100));
    assert.equal(logins, 0, operation + ' response must settle before new login');
    release(); await Promise.all([previous, signing]);
    await race.context.unroute('**/api/v1/auth/' + operation);
    race.context.off('request', count);
    const state = await snapshot(raceTab);
    const cookies = await race.context.cookies(base + '/api/v1/auth');
    assert.equal(cookies.find((cookie) => cookie.name === 'jz_rt').value.split('.')[3], state.sessionID);
    await raceTab.reload(); await raceTab.locator('.page h2').first().waitFor();
    assert.equal((await snapshot(raceTab)).sessionID, state.sessionID);
    console.log('PASS delayed successful ' + operation + ' cannot overwrite or delete the next login cookie');
  }

  await raceTab.goto(base + '/mine');
  await raceTab.getByRole('button', { name: '退出所有设备' }).waitFor();
  await race.context.route('**/api/v1/auth/logout', (route) => route.fulfill({ status: 503, json: { code: 20001, message: '测试退出失败' } }));
  await raceTab.getByRole('button', { name: '退出所有设备' }).click();
  await raceTab.getByRole('button', { name: '重试退出' }).waitFor();
  await raceTab.goto(base + '/');
  await raceTab.waitForURL(/\/login$/);
  assert.equal((await snapshot(raceTab)).token, null);
  await race.context.unroute('**/api/v1/auth/logout');
  await raceTab.getByRole('button', { name: '重试退出' }).click();
  await raceTab.getByText('已退出所有设备', { exact: true }).waitFor();
  console.log('PASS failed logout survives reload without cookie restoration and can be retried');

  const fallback = await device('desktop', 'voucher-recorder', () => Object.defineProperty(navigator, 'locks', { value: undefined }));
  const fallbackTab = await fallback.context.newPage();
  await fallbackTab.goto(base + '/mine');
  await fallbackTab.getByRole('button', { name: '退出所有设备' }).waitFor();
  const fallbackState = await snapshot(fallbackTab);
  assert.equal((await snapshot(fallback.page)).sessionID, fallbackState.sessionID);
  await invoke(fallbackTab, 'refresh');
  await fallbackTab.getByRole('button', { name: '退出所有设备' }).click();
  await fallback.page.waitForURL(/\/login$/);
  console.log('PASS SharedWorker fallback login, shared-tab refresh and global logout');

  const noBroadcast = await device('desktop', 'voucher-recorder', () => Object.defineProperty(window, 'BroadcastChannel', { value: undefined }));
  const noBroadcastTab = await noBroadcast.context.newPage();
  await noBroadcastTab.goto(base + '/mine');
  await noBroadcastTab.getByRole('button', { name: '退出所有设备' }).waitFor();
  assert.equal((await snapshot(noBroadcast.page)).sessionID, (await snapshot(noBroadcastTab)).sessionID);
  await noBroadcastTab.getByRole('button', { name: '退出所有设备' }).click();
  await noBroadcast.page.waitForURL(/\/login$/);
  console.log('PASS missing BroadcastChannel uses shared proxy and non-secret storage lifecycle signals');

  const unsupported = await browser.newContext(); contexts.push(unsupported);
  await unsupported.addInitScript(() => {
    Object.defineProperty(navigator, 'locks', { value: undefined });
    Object.defineProperty(window, 'SharedWorker', { value: undefined });
  });
  let unsupportedCalls = 0;
  unsupported.on('request', (request) => { if (request.url().endsWith('/auth/login')) unsupportedCalls++; });
  const unsupportedPage = await unsupported.newPage();
  await unsupportedPage.goto(base + '/login');
  await unsupportedPage.getByPlaceholder('用户名').fill('voucher-recorder');
  await unsupportedPage.getByPlaceholder('密码').fill('voucher-test-only');
  await unsupportedPage.getByRole('button', { name: /登\s*录/ }).click();
  await unsupportedPage.getByText('浏览器无法协调登录，请使用较新的浏览器或安全地址后重试', { exact: true }).waitFor();
  assert.equal(unsupportedCalls, 0);
  assert.deepEqual(errors, []);
  console.log('PASS unsupported coordination blocks cookie writes; no browser runtime errors');
} finally {
  for (const context of contexts.reverse()) await context.close();
  await browser.close();
}
