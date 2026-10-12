import assert from 'node:assert/strict';
import {
  withFixture,
  fixtureApi,
  login,
  until,
  shot,
} from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  const api = await fixtureApi(base);
  const name = `生命周期商家-${Date.now()}`;
  await api('POST', '/voucher-platforms', { name });
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const dialog = (name) => page.getByRole('dialog', { name, exact: true });
  const noInputFocus = async () => {
    await until(
      () =>
        page.evaluate(() => !document.activeElement.matches('input,textarea')),
      Boolean,
      'non-input focus'
    );
  };
  const close = async (sheet) => {
    await sheet.locator('.ant-drawer-close').click();
    await sheet.waitFor({ state: 'hidden' });
  };
  try {
    let failTypes = true;
    await page.route('**/api/v1/accounts/liability-types', (route) =>
      failTypes
        ? route.fulfill({
            status: 503,
            contentType: 'application/json',
            body: JSON.stringify({ code: 503, message: '测试类型读取失败' }),
          })
        : route.continue()
    );
    await login(page, base);
    await page.goto(base + '/liabilities');
    await page.getByRole('button', { name: /新增负债账户/ }).click();
    const personal = dialog('新增负债账户');
    await personal.getByLabel('消费信用类型', { exact: true }).click();
    let sheet = dialog('选择消费信用类型');
    await sheet
      .locator('.selection-error')
      .filter({ hasText: '测试类型读取失败' })
      .waitFor();
    await noInputFocus();
    failTypes = false;
    await sheet.getByRole('button', { name: /重\s*试/ }).click();
    await sheet.getByRole('button', { name: '信用卡', exact: true }).waitFor();
    await noInputFocus();
    await sheet.getByRole('button', { name: '信用卡', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(
      await personal.getByLabel('消费信用类型', { exact: true }).innerText(),
      '信用卡'
    );
    await personal.getByRole('button', { name: /取\s*消/ }).click();
    await personal.waitFor({ state: 'hidden' });
    await page.unroute('**/api/v1/accounts/liability-types');
    console.log(
      'PASS credit type loading error/retry and selection, retry never focuses text input'
    );

    let failDirectory = true;
    await page.route('**/api/v1/voucher-platforms', (route) =>
      failDirectory && route.request().method() === 'GET'
        ? route.fulfill({
            status: 503,
            contentType: 'application/json',
            body: JSON.stringify({ code: 503, message: '测试目录读取失败' }),
          })
        : route.continue()
    );
    await page.goto(base + '/records');
    await page
      .locator('.page-head')
      .getByRole('button', { name: /记一笔$/ })
      .click();
    const form = dialog('记一笔');
    const platform = form.getByLabel('平台 / 商家', { exact: true });
    await platform.click();
    sheet = dialog('选择平台 / 商家');
    await sheet
      .locator('.selection-error')
      .filter({ hasText: '选项加载失败，可重试或填写名称。' })
      .waitFor();
    await noInputFocus();
    failDirectory = false;
    await sheet.getByRole('button', { name: /重\s*试/ }).click();
    await sheet.getByRole('button', { name, exact: true }).waitFor();
    await noInputFocus();
    await sheet.getByRole('button', { name, exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await page.unroute('**/api/v1/voucher-platforms');
    const account = form.locator(
      '.selection-trigger:not(.voucher-platform-trigger)'
    );
    await account.click();
    sheet = dialog('选择账户');
    await sheet.getByRole('button', { name: '测试现金', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await account.click();
    await sheet.waitFor();
    await page.setViewportSize({ width: 768, height: 844 });
    await sheet.waitFor({ state: 'hidden' });
    await form.getByRole('combobox').last().waitFor();
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await account.innerText(), '测试现金');
    assert.equal(
      await page.locator('.selection-sheet.ant-drawer-open').count(),
      0
    );
    await account.click();
    await sheet.waitFor();
    // Exercise the owner close signal even when a portaled child is still open.
    await form
      .getByRole('button', { name: /取\s*消/ })
      .evaluate((button) => button.click());
    await form.waitFor({ state: 'hidden' });
    await sheet.waitFor({ state: 'hidden' });
    await until(
      () =>
        page.evaluate(() =>
          document.documentElement.style.getPropertyValue('--kb-inset')
        ),
      (value) => value === '',
      'all keyboard listeners released'
    );
    console.log(
      'PASS platform directory error/retry, breakpoint closes old surface and retains value, parent closure removes child and keyboard observers'
    );

    for (const mode of ['dark', 'light']) {
      await page.goto(base + '/mine');
      await page
        .getByText(mode === 'dark' ? '暗色' : '浅色', { exact: true })
        .click();
      await until(
        () => page.evaluate(() => document.documentElement.dataset.theme),
        (theme) => theme === mode,
        'theme state'
      );
      await page.goto(base + '/vouchers');
      await page.getByRole('button', { name: /登记购券$/ }).click();
      const voucher = dialog('登记购券');
      await voucher.getByLabel('支付账户', { exact: true }).click();
      sheet = dialog('选择支付账户');
      await sheet.waitFor();
      await noInputFocus();
      const background = await sheet.evaluate(
        (el) => getComputedStyle(el).backgroundColor
      );
      assert.notEqual(
        background,
        'rgba(0, 0, 0, 0)',
        'panel has an opaque themed background'
      );
      await shot(page, `selection-voucher-mobile-${mode}`);
      await close(sheet);
      await voucher.getByRole('button', { name: /取\s*消/ }).click();
      await page.setViewportSize({ width: 992, height: 844 });
      await page.locator('.filter-bar .ant-select').click();
      await page.locator('.ant-select-dropdown:visible').waitFor();
      await shot(page, `selection-desktop-${mode}`);
      await page.keyboard.press('Escape');
      await page.setViewportSize({ width: 390, height: 844 });
    }
    assert.deepEqual(errors, []);
    console.log(
      'PASS real light/dark theme state and mobile/desktop screenshots, no page exceptions'
    );
  } catch (error) {
    await shot(page, 'selection-lifecycle-failure').catch(() => {});
    throw error;
  } finally {
    await context.close();
  }
});
