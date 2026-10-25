import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import { fixtureApi, login, shot, until, withFixture } from './support/fixture.mjs';

const base = process.env.TRANSACTION_ORDER_E2E_BASE ?? 'http://127.0.0.1:5625';
const reportPath = path.resolve('../bin/transaction-order-stability.json');
const reports = [];

await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const marker = Date.now();
  const account = await api('POST', '/accounts', { name: `排序稳定性-${marker}`, type: 'asset', request_key: randomUUID() });
  const category = await api('POST', '/categories', { kind: 'expense', name: `排序稳定性-${marker}` });
  for (const amount of ['1', '2']) {
    const transaction = await api('POST', '/transactions', { account_id: account.id, category_id: category.id, kind: 'expense', amount, entry_date: new Date().toISOString().slice(0, 10), note: `稳定性-${amount}` });
    cleanup(() => api('DELETE', `/transactions/${transaction.id}`));
  }
  for (const mobile of [true, false]) {
    const context = await browser.newContext({ viewport: mobile ? { width: 427, height: 884 } : { width: 1440, height: 1000 }, isMobile: mobile, hasTouch: mobile });
    const page = await context.newPage();
    await login(page, base);
    let saving = false;
    await page.route('**/transactions?**', async (route) => {
      if (saving) await new Promise((resolve) => setTimeout(resolve, 150));
      await route.continue();
    });
    for (const route of ['/records', '/']) {
      if (process.env.SORT_STABILITY_CASE === 'mobile-records' && (!mobile || route !== '/records')) continue;
      await page.goto(base + route + (route === '/records' ? `?account_id=${account.id}` : ''));
      const selector = route === '/' ? '.recent-row' : mobile ? '.txn-row' : 'tr[data-order-id]';
      const handles = page.locator(`${selector} [data-sort-handle]`);
      await until(() => handles.nth(1).isEnabled(), Boolean, 'ready to sort');
      await page.waitForTimeout(1000);
      await handles.nth(1).evaluate((element) => element.scrollIntoView({ block: 'center', behavior: 'instant' }));
      const from = await handles.nth(1).boundingBox();
      const to = await handles.first().boundingBox();
      const id = await handles.nth(1).locator('xpath=ancestor::*[@data-order-id][1]').getAttribute('data-order-id');
      assert(from && to);
      await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
      await page.mouse.down();
      await page.mouse.move(from.x + from.width / 2 + 8, from.y + from.height / 2, { steps: 3 });
      await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 });
      await page.waitForTimeout(200);
      await page.evaluate(({ id, selector }) => {
        window.__sortFrames = [];
        const start = performance.now();
        const sample = () => {
          const nodes = [...document.querySelectorAll(`[data-order-id="${id}"]`)];
          const rows = nodes.map((element) => {
            const style = getComputedStyle(element);
            const box = element.getBoundingClientRect();
            let opacity = 1;
            for (let parent = element; parent; parent = parent.parentElement) opacity *= Number(getComputedStyle(parent).opacity);
            return { opacity, visible: style.visibility === 'visible', y: box.y, h: box.height, animations: element.getAnimations().map((animation) => ({ name: animation.animationName, time: animation.currentTime, state: animation.playState })), attributes: [...element.attributes].filter((attr) => attr.name.startsWith('data-')).map((attr) => [attr.name, attr.value]) };
          });
          const first = document.querySelector(selector)?.getBoundingClientRect();
          const ptr = document.querySelector('.ptr-indicator');
          window.__sortFrames.push({ ms: performance.now() - start, rows, firstY: first?.y, ptr: ptr ? { text: ptr.textContent, transform: getComputedStyle(ptr).transform } : null });
          if (performance.now() - start < 1300) requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
      }, { id, selector });
      const save = page.waitForResponse((response) => response.url().endsWith('/transactions/reorder') && response.request().method() === 'PUT');
      saving = true;
      await page.mouse.up();
      assert.equal((await save).status(), 200);
      await page.waitForTimeout(1400);
      saving = false;
      const frames = await page.evaluate(() => window.__sortFrames);
      const opacities = frames.map((frame) => Math.max(0, ...frame.rows.filter((row) => row.visible && row.h > 0).map((row) => row.opacity)));
      const minOpacity = Math.min(...opacities);
      const report = { mobile, route, minOpacity, frames };
      reports.push(report);
      console.log(JSON.stringify({ mobile, route, minOpacity, firstY: [Math.min(...frames.map((frame) => frame.firstY)), Math.max(...frames.map((frame) => frame.firstY))], refreshIndicator: frames.some((frame) => frame.ptr?.text === '刷新中…') }));
      if (route === '/records' && !process.env.SORT_STABILITY_CASE) {
        await shot(page, `transaction-order-stable-${mobile ? 'mobile' : 'desktop'}`);
        if (mobile) {
          // Sorting saves are background reads; a real pull must still refresh.
          const cdp = await page.context().newCDPSession(page);
          await page.evaluate(() => window.scrollTo(0, 0));
          const first = await page.locator('.txn-main').first().boundingBox();
          assert(first);
          const point = { x: first.x + first.width / 2, y: first.y + 12 };
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [point] });
          for (let i = 1; i <= 8; i++) {
            await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: point.x, y: point.y + 20 * i }] });
            await page.waitForTimeout(20);
          }
          assert.match(await page.locator('.ptr-indicator').textContent(), /松开刷新/);
          saving = true;
          const refresh = page.waitForResponse((response) => response.url().includes('/transactions?') && response.request().method() === 'GET');
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
          await until(() => page.locator('.ptr-indicator').textContent(), (text) => text === '刷新中…', 'real pull-to-refresh indicator');
          assert.equal((await refresh).status(), 200);
          saving = false;
          await cdp.detach();
        }
      }
    }
    if (!process.env.SORT_STABILITY_CASE) {
      await page.locator('.recent-content').first().click();
      await page.waitForURL(/\/records/);
      const highlighted = page.locator('[data-txn-id].flash').first();
      await highlighted.waitFor();
      const animation = await highlighted.evaluate((element) => getComputedStyle(element.querySelector('td') ?? element).animationName);
      assert.equal(animation, 'row-flash', 'navigation highlight must remain available');
    }
    await context.close();
  }
}, 'chromium');

fs.mkdirSync(path.dirname(reportPath), { recursive: true });
fs.writeFileSync(reportPath, JSON.stringify(reports, null, 2));
assert(reports.every((report) => report.minOpacity >= 0.98), 'A dropped row fades out or disappears during sort/save');
assert(reports.every((report) => !report.frames.some((frame) => frame.ptr?.text === '刷新中…')), 'Saving a sort incorrectly shows the pull-to-refresh indicator');
assert(reports.every((report) => Math.max(...report.frames.map((frame) => frame.firstY)) - Math.min(...report.frames.map((frame) => frame.firstY)) <= 1), 'The list jumps vertically after drop');
console.log('PASS dropped rows remain opaque throughout save and refresh.');
