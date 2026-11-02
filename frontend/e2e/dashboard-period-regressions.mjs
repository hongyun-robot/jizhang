import assert from 'node:assert/strict';
import dayjs from 'dayjs';
import { withFixture, fixtureApi, login, until } from './support/fixture.mjs';

const base = process.env.DASHBOARD_E2E_BASE ?? 'http://127.0.0.1:5619';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const current = (await api('GET', '/budgets')).period_key;
  const previous = dayjs(current + '-01').subtract(1, 'month').format('YYYY-MM');
  const account = (await api('GET', '/accounts')).find((row) => row.type === 'asset');
  const category = (await api('GET', '/budgets')).categories[0].category_id;
  const note = '跨月份最近流水';
  const txn = await api('POST', '/transactions', { kind: 'expense', amount: '9', account_id: account.id, category_id: category, entry_date: previous + '-15', note });
  cleanup(() => api('DELETE', `/transactions/${txn.id}`));
  const failures = [];
  const selectedCase = process.env.DASHBOARD_REGRESSION_CASE;
  for (const width of [431, 1280]) {
    for (const name of ['first-click', 'reload-input', 'reload-touch', 'focus-validation', 'toggle', 'recent']) {
      if (selectedCase && selectedCase !== name) continue;
      const context = await browser.newContext({ viewport: { width, height: 884 }, timezoneId: 'Asia/Shanghai', isMobile: width < 768, hasTouch: width < 768 });
      const page = await context.newPage();
      let release;
      try {
        if (name === 'first-click') {
          const pending = new Promise((resolve) => { release = resolve; });
          await page.route('**/api/v1/budgets', async (route) => { await pending; await route.continue().catch(() => {}); });
          await page.route('**/api/v1/stats/compare', async (route) => { await pending; await route.continue().catch(() => {}); });
        }
        await login(page, base);
        const input = page.getByRole('textbox', { name: '选择账期' });
        const popup = page.locator('.hero-period-popup:visible');
        if (name !== 'first-click') await until(() => input.inputValue(), (value) => value === current, 'current period');
        if (name === 'first-click') {
          await page.locator('.hero-range').click();
          await until(() => popup.count(), (count) => count === 1, 'first click must open the popup even while period metadata is loading', 1500);
          release();
          await until(() => input.inputValue(), (value) => value === current, 'current period after metadata loads');
          assert.equal(await popup.count(), 1, 'loading completion must preserve the opened popup');
        } else if (name === 'focus-validation') {
          await page.waitForFunction(() => [...document.querySelectorAll('.page, .hero')].every((element) => getComputedStyle(element).transform === 'none'));
          const pending = new Promise((resolve) => { release = resolve; });
          await page.route('**/api/v1/auth/me', async (route) => { await pending; await route.continue().catch(() => {}); });
          const requested = page.waitForRequest((request) => request.url().endsWith('/api/v1/auth/me'));
          await page.evaluate(() => window.dispatchEvent(new Event('focus')));
          await requested;
          const box = await input.boundingBox();
          // The real trace loses the click when validation disables the page
          // for pointer-down, then finishes before pointer-up reaches the input.
          await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
          await page.mouse.down();
          const validated = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/me'));
          release();
          assert.equal((await validated).status(), 200);
          await page.waitForFunction(() => !document.querySelector('.hero-range')?.closest('[inert]'));
          await page.mouse.up();
          await popup.waitFor({ state: 'visible', timeout: 1500 });
        } else if (name.startsWith('reload-')) {
          for (let attempt = 0; attempt < 5; attempt++) {
            await page.reload();
            await input.waitFor({ state: 'visible' });
            const box = await input.boundingBox();
            if (name === 'reload-touch' && width < 768) {
              await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2);
            } else {
              await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2, { delay: 80 });
            }
            await popup.waitFor({ state: 'visible', timeout: 1500 });
            await page.waitForTimeout(500);
            assert(await popup.isVisible(), `first ${name} on refresh ${attempt + 1} must remain open`);
          }
        } else if (name === 'toggle') {
          await input.click();
          await popup.waitFor();
          await input.click();
          await until(() => popup.count(), (count) => count === 0, 'second click must close the popup', 1500);
          const trigger = page.locator('.hero-range');
          const box = await trigger.boundingBox();
          await trigger.click({ position: { x: box.width - 10, y: box.height / 2 } });
          await popup.waitFor();
          await page.locator('h2').getByText('首页', { exact: true }).click();
          await until(() => popup.count(), (count) => count === 0, 'outside click closes the popup');
          for (const key of ['Enter', ' ', 'ArrowDown']) {
            await input.focus();
            await input.press(key);
            await popup.waitFor();
            await input.press('Escape');
            await until(() => popup.count(), (count) => count === 0, 'Escape closes keyboard-opened popup');
          }
        } else {
          await until(() => page.locator('.recent-list').innerText(), (text) => text.includes(note), 'current period must show recent transactions from other months', 1500);
          await page.goto(base + '/?period_key=' + previous);
          await until(() => page.locator('.recent-list').innerText(), (text) => text.includes(note), 'historical period retains its own records');
          await input.click();
          await popup.waitFor();
          const year = Number(current.slice(0, 4));
          if (Number(previous.slice(0, 4)) !== year) await popup.locator('.ant-picker-header-super-next-btn').click();
          await popup.locator(`.ant-picker-cell[title="${current}"]`).click();
          await until(() => input.inputValue(), (value) => value === current, 'return to current via month selection');
          await until(() => page.locator('.recent-list').innerText(), (text) => text.includes(note), 'selecting current month restores cross-month recent transactions');
          assert(!new URL(page.url()).searchParams.has('period_key'));
          await page.locator('.recent-content').filter({ hasText: note }).click();
          await page.waitForURL((url) => url.pathname === '/records' && url.searchParams.get('period_key') === previous);
          await until(() => page.locator('.page').innerText(), (text) => text.includes(note), 'cross-month recent transaction remains visible after navigation');
        }
        console.log(`PASS ${width}px ${name}`);
      } catch (error) {
        failures.push(`${width}px ${name}: ${error.message}`);
        console.log(`FAIL ${failures.at(-1)}`);
      } finally { release?.(); await context.close(); }
    }
  }
  assert.equal(failures.length, 0, failures.join('\n'));
}, 'chromium');
