import assert from 'node:assert/strict';
import { withFixture, login, until, shot } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const kind = process.env.SCROLL_BROWSER ?? 'chromium';

await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 500 } });
  await context.addInitScript(() => {
    const viewport = new EventTarget();
    Object.assign(viewport, {
      height: innerHeight, width: innerWidth, offsetTop: 0, offsetLeft: 0, scale: 1,
    });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const failures = [];
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  // Exercise long directories without creating accounts or submitting any form.
  await page.route('**/api/v1/accounts', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    const response = await route.fetch();
    const body = await response.json();
    const account = body.data.find((item) => item.type === 'asset');
    assert(account, 'fixture has a cash account');
    body.data.push(...Array.from({ length: 35 }, (_, index) => ({
      ...account, id: 100000 + index, name: `滚动验收账户 ${index + 1}`,
    })));
    await route.fulfill({ response, json: body });
  });
  const settle = async () => {
    await page.locator('body').evaluate(async (element) => {
      await Promise.all(element.getAnimations({ subtree: true })
        .filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity)
        .map((animation) => animation.finished.catch(() => {})));
    });
  };
  const ancestors = (target) => target.evaluate((element) => {
    const positions = [{ node: 'window', top: scrollY, left: scrollX }];
    for (let parent = element.parentElement; parent; parent = parent.parentElement) {
      positions.push({ node: parent.tagName + '.' + (parent.classList[0] ?? ''), top: parent.scrollTop, left: parent.scrollLeft });
    }
    return positions;
  });
  const wheel = async (target, delta) => {
    const point = await target.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      let top = Math.max(0, rect.top);
      let bottom = Math.min(innerHeight, rect.bottom);
      for (let parent = element.parentElement; parent; parent = parent.parentElement) {
        if (/(auto|scroll|hidden|clip)/.test(getComputedStyle(parent).overflowY)) {
          const bounds = parent.getBoundingClientRect();
          top = Math.max(top, bounds.top);
          bottom = Math.min(bottom, bounds.bottom);
        }
      }
      const middle = rect.left + rect.width / 2;
      const columns = element.tagName === 'TEXTAREA'
        ? [middle, rect.left + 3, rect.right - 3]
        : [rect.left + 3, middle, rect.right - 3];
      for (const x of columns) {
        for (const fraction of [0.5, 0.2, 0.8]) {
          const y = top + (bottom - top) * fraction;
          if (bottom - top < 2) continue;
          const hit = document.elementFromPoint(x, y);
          if (!element.contains(hit)) continue;
          let nested = false;
          for (let node = hit; node && node !== element; node = node.parentElement) {
            if (node.tagName === 'TEXTAREA' || /(auto|scroll)/.test(getComputedStyle(node).overflowY)) nested = true;
          }
          if (!nested) return { x, y };
        }
      }
      return null;
    });
    assert(point, 'wheel must hit the intended visible scroll area');
    await page.mouse.move(point.x, point.y);
    await page.waitForTimeout(100);
    await page.mouse.wheel(0, delta);
    await page.waitForTimeout(250);
  };
  const check = async (label, action) => {
    try {
      await action();
      console.log('PASS ' + label);
    } catch (error) {
      failures.push(label + ': ' + error.message);
      console.error('FAIL ' + label + ': ' + error.message);
    }
  };
  const edges = async (target, label) => {
    await settle();
    const scrollsInWrapper = await target.evaluate((element) =>
      element.scrollHeight <= element.clientHeight && getComputedStyle(element).overflowY === 'visible');
    if (scrollsInWrapper) {
      target = target.locator('xpath=ancestor::*[contains(@class,"ant-modal-wrap")]');
      label += ' outer scroll area';
    }
    for (const [edge, delta] of [['bottom', 400], ['top', -400]]) {
      await target.evaluate((element, edge) => {
        element.scrollTop = edge === 'bottom' ? element.scrollHeight : 0;
      }, edge);
      await page.waitForTimeout(100);
      const before = await ancestors(target);
      await check(label + ' ' + edge + ' stays isolated', async () => {
        await wheel(target, delta);
        assert.deepEqual(await ancestors(target), before, 'ancestor scroll positions changed');
      });
    }
  };
  const close = async (dialog) => {
    await dialog.getByRole('button', { name: /取\s*消/ }).click();
    await dialog.waitFor({ state: 'hidden' });
  };
  const open = async (route, button, title) => {
    await page.goto(base + route);
    await page.getByRole('button', { name: new RegExp(button + '$') }).first().click();
    const dialog = page.getByRole('dialog', { name: title, exact: true });
    await dialog.waitFor();
    await settle();
    return dialog;
  };
  await login(page, base);
  for (const viewport of [{ width: 1280, height: 500 }, { width: 390, height: 600 }]) {
    await page.setViewportSize(viewport);
    const label = `${kind} ${viewport.width}px`;
    const form = await open('/', '快捷记账', '记一笔');
    const body = form.locator('.ant-modal-body');
    await body.evaluate((element) => { element.scrollTop = 0; });
    await check(label + ' quick entry still scrolls normally', async () => {
      assert(await body.evaluate((element) => element.scrollHeight > element.clientHeight));
      const before = await ancestors(body);
      await wheel(body, 100);
      assert(await body.evaluate((element) => element.scrollTop > 0));
      assert.deepEqual(await ancestors(body), before);
    });
    await edges(body, label + ' quick entry');
    const note = form.getByPlaceholder('备注（可选）');
    await note.fill('滚动测试\n'.repeat(30));
    await note.scrollIntoViewIfNeeded();
    await note.click();
    await check(label + ' note scroll does not move the form', async () => {
      assert(await body.evaluate((element) => element.scrollTop > 0));
      assert(await note.evaluate((element) => element.scrollHeight > element.clientHeight));
      await note.evaluate((element) => { element.scrollTop = 0; });
      const before = await ancestors(note);
      await wheel(note, 200);
      assert(await note.evaluate((element) => element.scrollTop > 0), 'note remains scrollable');
      assert.deepEqual(await ancestors(note), before);
      await note.evaluate((element) => { element.scrollTop = 0; });
      await page.waitForTimeout(100);
      await wheel(note, -400);
      assert.deepEqual(await ancestors(note), before, 'note boundary moved the form');
    });
    if (viewport.width >= 768) {
      await form.getByRole('combobox').last().click();
      const list = page.locator('.ant-select-dropdown:visible .ant-select-dropdown-list-holder');
      await list.waitFor();
      await edges(list, label + ' nested account dropdown');
      await check(label + ' account dropdown still scrolls and selects', async () => {
        const before = await ancestors(list);
        await wheel(list, 100);
        assert(await list.evaluate((element) => element.scrollTop > 0), 'virtual list remains scrollable');
        assert.deepEqual(await ancestors(list), before);
        const option = page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').first();
        const selected = await option.innerText();
        await option.click();
        assert.equal(await form.locator('.ant-select-content-has-value').last().innerText(), selected);
      });
      await page.locator('.ant-select-dropdown:visible').waitFor({ state: 'hidden' });
    } else {
      await form.locator('.selection-trigger:not(.voucher-platform-trigger)').first().click();
      const sheet = page.getByRole('dialog', { name: '选择账户', exact: true });
      await sheet.waitFor();
      const parentBefore = await body.evaluate((element) => element.scrollTop);
      await edges(sheet.locator('.selection-sheet-list'), label + ' nested account sheet');
      assert.equal(await body.evaluate((element) => element.scrollTop), parentBefore);
      await sheet.locator('.ant-drawer-close').click();
      await sheet.waitFor({ state: 'hidden' });
      await page.evaluate(() => { visualViewport.height = 300; visualViewport.dispatchEvent(new Event('resize')); });
      await until(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--kb-inset')),
        (value) => value === '300px', 'keyboard inset');
      await edges(body, label + ' quick entry with keyboard');
      const footer = await form.locator('.ant-modal-footer').boundingBox();
      assert(footer.y + footer.height <= 301, 'footer remains above the keyboard');
      await page.evaluate(() => { visualViewport.height = 600; visualViewport.dispatchEvent(new Event('resize')); });
    }
    await shot(page, `overlay-scroll-${kind}-${viewport.width}`);
    await close(form);
    for (const [route, button, title] of [
      ['/vouchers', '登记购券', '登记购券'],
      ['/accounts', '新建账户', '新建账户'],
      ['/liabilities', '新增负债账户', '新增负债账户'],
      ['/recurring', '新建周期', '新建周期模板'],
    ]) {
      const dialog = await open(route, button, title);
      await edges(dialog.locator('.ant-modal-body'), label + ' ' + title);
      await close(dialog);
    }
    if (viewport.width < 768) {
      const drawer = await open('/records', '筛选', '筛选');
      await edges(drawer.locator('.ant-drawer-body'), label + ' filter drawer');
      await drawer.locator('.ant-drawer-close').click();
      await drawer.waitFor({ state: 'hidden' });
    }
    await check(label + ' page scrolling resumes after closing', async () => {
      await page.goto(base + '/');
      await settle();
      assert(await page.evaluate(() => document.documentElement.scrollHeight > innerHeight));
      await page.mouse.move(viewport.width - 20, 150);
      await page.mouse.wheel(0, 200);
      await page.waitForTimeout(250);
      assert(await page.evaluate(() => scrollY > 0));
    });
  }
  assert.deepEqual(errors, [], 'no browser runtime errors');
  assert.deepEqual(failures, [], 'overlay scroll regressions');
  await context.close();
}, kind, kind === 'firefox' ? {
  // Widget-level wheel events honor overscroll-behavior in Firefox automation.
  firefoxUserPrefs: { 'test.events.async.enabled': true },
} : {});
