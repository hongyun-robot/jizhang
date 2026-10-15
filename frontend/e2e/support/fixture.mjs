import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { firefox, chromium } from 'playwright-core';

export async function assertFixture(base, expected = ['voucher-browser', 'liability-browser', 'fund-browser', 'gold-browser']) {
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
  await page.goto(base + '/login');
  await page.getByPlaceholder('用户名').fill(username);
  await page.getByPlaceholder('密码').fill(password);
  await page.getByRole('button', { name: /登\s*录/ }).click();
  await page.waitForURL((url) => !url.pathname.endsWith('/login'));
  await page.locator('.page h2').first().waitFor();
}

export async function fixtureApi(base, username = 'voucher-admin') {
  await assertFixture(base);
  const auth = await fetch(base + '/api/v1/auth/login', {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username, password: 'voucher-test-only' }),
  });
  assert.equal(auth.status, 200);
  const token = (await auth.json()).data.access_token;
  return async (method, endpoint, input) => {
    const response = await fetch(base + '/api/v1' + endpoint, {
      method, headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' },
      ...(input === undefined ? {} : { body: JSON.stringify(input) }),
    });
    const body = await response.json();
    assert.equal(response.status, 200, JSON.stringify(body));
    assert.equal(body.code, 0, JSON.stringify(body));
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
