import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { webkit } from 'playwright-core';
import { assertFixture, launchBrowser, login, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const kind = process.env.SCROLL_BROWSER ?? 'chromium';
await assertFixture(base);
mkdirSync('e2e/shots/refactor-frontend', { recursive: true });
const browser = kind === 'webkit' ? await webkit.launch() : await launchBrowser(kind);
try {
  for (const viewport of [
    { width: 1280, height: 600 },
    { width: 390, height: 600 },
    { width: 1024, height: 768 },
  ]) {
    const touch = viewport.width < 1280;
    const context = await browser.newContext({
      viewport, hasTouch: touch,
      ...(kind !== 'firefox' ? { isMobile: touch } : {}),
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await login(page, base);
    // Keep the background long even when this isolated ledger has no records.
    await page.locator('.page').evaluate((element) => { element.style.minHeight = '2400px'; });
    const trigger = page.getByRole('button', { name: /快捷记账$/ });
    await trigger.scrollIntoViewIfNeeded();
    await page.evaluate(() => window.scrollTo({ top: 80, behavior: 'instant' }));
    const original = await page.evaluate(() => ({
      scrollY, position: document.body.style.position,
      top: document.body.style.top, left: document.body.style.left, right: document.body.style.right,
    }));
    assert(original.scrollY > 0, 'start from a scrolled background');
    await trigger.evaluate((element) => element.addEventListener('click', () => {
      element.dataset.scrollBeforeOpen = String(window.scrollY);
    }, { capture: true, once: true }));
    await trigger.click();
    original.scrollY = Number(await trigger.getAttribute('data-scroll-before-open'));
    const form = page.getByRole('dialog', { name: '记一笔', exact: true });
    const body = form.locator('.ant-modal-body');
    await body.waitFor();
    const backgroundTop = () => page.locator('.page').evaluate((element) => element.getBoundingClientRect().top);
    const before = await backgroundTop();
    for (const edge of ['bottom', 'top']) {
      await body.evaluate((element, edge) => { element.scrollTop = edge === 'bottom' ? element.scrollHeight : 0; }, edge);
      if (edge === 'bottom') assert(await body.evaluate((element) => element.scrollTop > 0), 'modal content still scrolls');
      // Model Safari allowing root panning despite AntD's body overflow lock.
      await page.evaluate((delta) => window.scrollBy({ top: delta, behavior: 'instant' }), edge === 'bottom' ? 200 : -200);
      assert.equal(await backgroundTop(), before, `${edge}: root panning must not move the background`);
    }
    if (touch && viewport.width < 768) {
      await form.locator('.selection-trigger:not(.voucher-platform-trigger)').first().click();
      const sheet = page.getByRole('dialog', { name: '选择账户', exact: true });
      await sheet.waitFor();
      await sheet.locator('.ant-drawer-close').click();
      await sheet.waitFor({ state: 'hidden' });
      await page.evaluate(() => window.scrollBy({ top: 200, behavior: 'instant' }));
      assert.equal(await backgroundTop(), before, 'closing the nested sheet keeps the background locked');
    }
    await page.screenshot({ path: `e2e/shots/refactor-frontend/txn-modal-scroll-${kind}-${viewport.width}.png`, animations: 'disabled' });
    await form.getByRole('button', { name: '购券：先付款，使用时计消费' }).click();
    const voucher = page.getByRole('dialog', { name: '登记购券', exact: true });
    await voucher.waitFor();
    await page.evaluate(() => window.scrollBy({ top: 200, behavior: 'instant' }));
    assert.equal(await backgroundTop(), before, 'switching to purchase keeps the background locked');
    await voucher.getByRole('button', { name: /取\s*消/ }).click();
    await voucher.waitFor({ state: 'hidden' });
    await until(() => page.evaluate(() => document.body.style.position), (value) => value === original.position, 'restore body styles');
    assert.deepEqual(await page.evaluate(() => ({
      scrollY, position: document.body.style.position,
      top: document.body.style.top, left: document.body.style.left, right: document.body.style.right,
    })), original, 'closing restores the original background position and inline styles');
    await page.evaluate(() => window.scrollBy({ top: 200, behavior: 'instant' }));
    assert.equal(await page.evaluate(() => scrollY), original.scrollY + 200, 'page scrolling resumes');
    // Reopening and cancelling ordinary entry must release its lock too.
    await page.evaluate(() => window.scrollTo({ top: 80, behavior: 'instant' }));
    await trigger.click();
    await form.getByRole('button', { name: /取\s*消/ }).click();
    await form.waitFor({ state: 'hidden' });
    assert.equal(await page.evaluate(() => document.body.style.position), original.position);
    assert.deepEqual(errors, [], 'no browser runtime errors');
    console.log(`PASS ${kind} ${viewport.width}px background lock, nested selection, purchase switch and scroll restoration`);
    await context.close();
  }
} finally {
  await browser.close();
}
