import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { firefox, chromium } from 'playwright-core';

const fixturePages = new Map();
const fixtureTokens = new Map();
const trackedPages = new WeakSet();
const pageSessions = new WeakMap();
export const fixtureToken = (base, username, fallback = '') => fixtureTokens.get(base + ':' + username)?.token ?? fallback;
export function registerFixturePage(page, base, username = 'voucher-admin') {
  const key = base + ':' + username;
  const pages = fixturePages.get(key) ?? [];
  fixturePages.set(key, [...pages.filter((value) => value !== page && !value.isClosed()), page]);
  if (trackedPages.has(page)) return;
  trackedPages.add(page);
  page.on('response', async (response) => {
    if (response.url().endsWith('/auth/logout') && response.status() === 200) {
      const username = pageSessions.get(page)?.username;
      if (username) {
        fixtureTokens.delete(base + ':' + username);
        for (const pages of fixturePages.values()) for (const other of pages) if (pageSessions.get(other)?.username === username) pageSessions.delete(other);
      }
      return;
    }
    if (!/\/auth\/(login|refresh)$/.test(response.url()) || response.status() !== 200) return;
    const result = (await response.json().catch(() => null))?.data;
    if (result?.user?.username && result.access_token) {
      const value = { token: result.access_token, sid: result.session_id, username: result.user.username, base };
      fixtureTokens.set(base + ':' + result.user.username, value);
      pageSessions.set(page, value);
    }
  });
}
export async function fixturePageRequest(page, method, endpoint, input) {
  const current = pageSessions.get(page);
  assert(current, 'register the page before logging in to reuse its authenticated session');
  const response = await page.request.fetch(current.base + '/api/v1' + endpoint, {
    method, headers: { Authorization: 'Bearer ' + fixtureToken(current.base, current.username, current.token) }, data: input,
  });
  return { status: response.status(), body: await response.json() };
}
// Development-only helper for exercising the actual frontend retry/gating code.
export async function pageRequest(page, method, endpoint, input) {
  return page.evaluate(async ({ method, endpoint, input }) => {
    const url=performance.getEntriesByType('resource').map((entry)=>entry.name).find((name)=>new URL(name).pathname.endsWith('/src/api/client.ts')) ?? '/src/api/client.ts';
    const { api } = await import(url);
    try {
      const reply = await api.request({ method, url: endpoint, data: input });
      return { status: reply.status, body: reply.data };
    } catch (error) {
      return { status: error.response?.status ?? error.status, body: error.response?.data ?? { code: error.code, message: error.message } };
    }
  }, { method, endpoint, input });
}

export async function assertFixture(base, expected = ['voucher-browser', 'liability-browser', 'fund-browser', 'gold-browser', 'wealth-browser']) {
  const url = new URL(base);
  assert(['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname), 'tests require a local isolated fixture');
  const response = await fetch(base + '/api/v1/__test__/info');
  assert.equal(response.status, 200, 'missing isolated fixture identity');
  const info = await response.json();
  assert(expected.includes(info.fixture), 'refuse to write to a real ledger');
  return info;
}

export async function launchBrowser(kind = 'firefox', launchOptions = {}) {
  const executablePath = kind === 'firefox'
    ? process.env.FIREFOX_EXE ?? path.join(process.env.LOCALAPPDATA, 'ms-playwright/firefox-1543/firefox/firefox.exe')
    : process.env.CHROME_EXE ?? 'C:/Program Files/Google/Chrome/Application/chrome.exe';
  assert(fs.existsSync(executablePath), 'browser executable missing: ' + executablePath);
  return (kind === 'firefox' ? firefox : chromium).launch({ ...launchOptions, executablePath, headless: true });
}

export async function login(page, base, username = 'voucher-admin', password = 'voucher-test-only') {
  registerFixturePage(page, base, username);
  await page.goto(base + '/login');
  await page.getByPlaceholder('用户名').fill(username);
  await page.getByPlaceholder('密码').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((url) => !url.pathname.endsWith('/login'));
  await page.locator('.page h2').first().waitFor();
  registerFixturePage(page, base, username);
}

export async function fixtureApi(base, username = 'voucher-admin') {
  await assertFixture(base);
  let token;
  const authenticate = async () => {
    const auth = await fetch(base + '/api/v1/auth/login', {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ protocol_version: 2, username, password: 'voucher-test-only' }),
    });
    assert.equal(auth.status, 200);
    const result = (await auth.json()).data;
    token = result.access_token;
    fixtureTokens.set(base + ':' + username, { token, sid: result.session_id });
  };
  return async (method, endpoint, input, expected = 200) => {
    for (const page of [...(fixturePages.get(base + ':' + username) ?? [])].reverse()) {
      if (page.isClosed()) continue;
      if (pageSessions.get(page)?.username !== username) continue;
      const response = await fixturePageRequest(page, method, endpoint, input);
      assert.equal(response.status, expected, JSON.stringify(response.body));
      if (expected === 200) assert.equal(response.body.code, 0, JSON.stringify(response.body));
      return response.body.data;
    }
    // Preparation/cleanup without a live page may explicitly establish its own session.
    token = fixtureToken(base, username, token);
    if (!token) await authenticate();
    const request = () => fetch(base + '/api/v1' + endpoint, {
      method, headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' },
      ...(input === undefined ? {} : { body: JSON.stringify(input) }),
    });
    let response = await request();
    if (response.status === 401 && expected !== 401) { await authenticate(); response = await request(); }
    const body = await response.json();
    assert.equal(response.status, expected, JSON.stringify(body));
    if (expected === 200) assert.equal(body.code, 0, JSON.stringify(body));
    return body.data;
  };
}

export async function until(read, predicate, label, timeout = 10000) {
  const end = Date.now() + timeout;
  let value;
  while (Date.now() < end) {
    value = await read();
    if (predicate(value)) return value;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.fail(label + ': ' + JSON.stringify(value));
}

export async function withFixture(base, run, kind = 'firefox', launchOptions = {}) {
  await assertFixture(base);
  const browser = await launchBrowser(kind, launchOptions);
  const cleanup = [];
  let failure;
  let result;
  try { result = await run(browser, (action) => cleanup.push(action)); }
  catch (error) { failure = error; }
  finally {
    let error;
    for (const action of cleanup.reverse()) {
      try { await action(); } catch (caught) { error ??= caught; }
    }
    await browser.close();
    if (failure && error) throw new AggregateError([failure, error], 'test and cleanup both failed');
    if (error) throw error;
  }
  if (failure) throw failure;
  return result;
}

export async function shot(page, name) {
  const folder = path.resolve('e2e/shots/refactor-frontend');
  fs.mkdirSync(folder, { recursive: true });
  await page.waitForTimeout(750);
  await page.locator('body').evaluate(async (element) => {
    await Promise.all(element.getAnimations({ subtree: true }).filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity).map((animation) => animation.finished.catch(() => {})));
  });
  await page.screenshot({ path: path.join(folder, name + '.png'), fullPage: true });
}
