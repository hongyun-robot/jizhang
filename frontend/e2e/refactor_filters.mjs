import assert from 'node:assert/strict';
import { withFixture, login } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  try {
    await login(page, base);
    const initial = page.waitForResponse((reply) => new URL(reply.url()).pathname.endsWith('/transactions'));
    await page.goto(base + '/records?from=2026-01-01&to=2026-02-28'); await initial;
    const dates = page.locator('.period-picker .ant-picker input');
    assert.equal(await dates.nth(0).inputValue(), '2026-01-01');
    assert.equal(await dates.nth(1).inputValue(), '2026-02-28');
    const requests = [];
    page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.endsWith('/transactions') && url.searchParams.has('keyword')) requests.push(url.searchParams.get('keyword')); });
    const typed = page.waitForResponse((reply) => new URL(reply.url()).searchParams.get('keyword') === 'abcd');
    void typed.catch(() => {});
    const keyword = page.getByPlaceholder('备注 / 平台 / 商家');
    await keyword.click();
    await keyword.pressSequentially('abcd', { delay: 20 });
    assert.equal(await page.getByPlaceholder('备注 / 平台 / 商家').inputValue(), 'abcd', 'controlled keyword must keep every keystroke');
    await typed;
    assert.deepEqual(requests, ['abcd'], 'rapid typing must not send a request per letter');
    const restored = page.waitForResponse((reply) => new URL(reply.url()).searchParams.get('keyword') === 'abc');
    await page.goBack(); await restored;
    assert.equal(await page.getByPlaceholder('备注 / 平台 / 商家').inputValue(), 'abc');
    assert.equal(new URL(page.url()).searchParams.get('keyword'), 'abc');
    console.log('PASS controlled date range, keyword debounce and POP restoration');
  } finally { await context.close(); }
});
