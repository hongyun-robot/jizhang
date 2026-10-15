import assert from 'node:assert/strict';
import { withFixture, login, shot, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser) => {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const failures = [];
  const check = async (label, action) => {
    try { await action(); console.log('PASS ' + label); }
    catch (error) { failures.push(label + ': ' + error.message); console.error('FAIL ' + label + ': ' + error.message); }
  };
  const fonts = async (label, surface = page) => check(label + ' input fonts prevent focus zoom', async () => {
    const small = await surface.locator('input:visible, textarea:visible, select:visible').evaluateAll((elements) =>
      elements.filter((element) => !['checkbox', 'radio', 'range', 'color', 'hidden'].includes(element.type))
        .map((element) => ({ placeholder: element.getAttribute('placeholder'), type: element.type, font: getComputedStyle(element).fontSize }))
        .filter((element) => parseFloat(element.font) < 16));
    assert.deepEqual(small, []);
  });
  await page.goto(base + '/login');
  await fonts('login');
  await login(page, base);
  await page.goto(base + '/');
  await page.getByRole('button', { name: /快捷记账$/ }).click();
  const form = page.getByRole('dialog', { name: '记一笔', exact: true });
  const input = form.locator('.ant-picker input');
  await input.click();
  const panel = page.locator('.ant-picker-dropdown:visible');
  await panel.waitFor();
  await check('calendar months and weekdays are Chinese', async () => {
    const month = await panel.locator('.ant-picker-month-btn').innerText();
    const weekdays = await panel.locator('thead th').allTextContents();
    console.log('calendar locale:', JSON.stringify({ month, weekdays }));
    assert(month.includes('月'));
    assert(weekdays.every((day) => /^[一二三四五六日天]$/.test(day)));
  });
  await check('mobile date opens without an editable keyboard input', async () => {
    assert(await input.evaluate((element) => element.readOnly));
    assert.equal(await page.evaluate(() => visualViewport.scale), 1);
  });
  await fonts('quick entry');
  await shot(page, 'date-calendar-mobile');
  const selectedDate = (await input.inputValue()).slice(0, 8) + '06';
  await panel.locator(`td[title="${selectedDate}"] .ant-picker-cell-inner`).click();
  assert.equal(await input.inputValue(), selectedDate, 'readonly input still selects a date');
  await form.getByRole('button', { name: /取\s*消/ }).click();
  await page.goto(base + '/records');
  await page.getByRole('button', { name: /筛选$/ }).click();
  await fonts('filters');
  const drawer = page.getByRole('dialog', { name: '筛选', exact: true });
  await drawer.getByText('自定义', { exact: true }).click();
  const range = drawer.locator('.ant-picker input');
  assert.equal(await range.count(), 2);
  assert(await range.evaluateAll((inputs) => inputs.every((element) => element.readOnly)), 'both range inputs suppress the keyboard');
  await range.first().tap();
  await panel.waitFor();
  assert((await panel.locator('.ant-picker-month-btn').allTextContents()).every((month) => month.includes('月')));
  const rangeBox = await panel.boundingBox();
  assert(rangeBox.x >= 0 && rangeBox.y >= 0 && rangeBox.x + rangeBox.width <= 391 && rangeBox.y + rangeBox.height <= 845, 'range calendar fits the phone viewport');
  await shot(page, 'date-range-mobile');
  const start = (await range.first().inputValue()).slice(0, 8) + '06';
  const end = start.slice(0, 8) + '07';
  await panel.locator(`.ant-picker-cell-in-view[title="${start}"] .ant-picker-cell-inner`).first().click();
  await panel.locator(`.ant-picker-cell-in-view[title="${end}"] .ant-picker-cell-inner`).first().click();
  await until(() => Promise.all([range.first().inputValue(), range.last().inputValue()]),
    (values) => values[0] === start && values[1] === end, 'selected date range updates the filter');
  await fonts('custom date range');
  await drawer.locator('.ant-drawer-close').click();
  await drawer.waitFor({ state: 'hidden' });

  for (const [route, button, title] of [
    ['/vouchers', '登记购券', '登记购券'],
    ['/accounts', '新建账户', '新建账户'],
    ['/liabilities', '新增负债账户', '新增负债账户'],
    ['/recurring', '新建周期', '新建周期模板'],
    ['/categories', '新建一级', '新建分类（一级）'],
  ]) {
    await page.goto(base + route);
    await fonts(route + ' page');
    await page.getByRole('button', { name: new RegExp(button + '$') }).first().click();
    const dialog = page.getByRole('dialog', { name: title, exact: true });
    await dialog.waitFor();
    await fonts(title);
    assert(await dialog.locator('.ant-picker input').evaluateAll((inputs) => inputs.every((element) => element.readOnly)));
    await dialog.getByRole('button', { name: /取\s*消/ }).click();
    await dialog.waitFor({ state: 'hidden' });
  }
  await page.goto(base + '/mine');
  await page.getByRole('button', { name: /用户管理$/ }).click();
  await page.getByRole('button', { name: /新建用户$/ }).click();
  const user = page.getByRole('dialog', { name: '新建用户', exact: true });
  await user.waitFor();
  await fonts('user creation and password');
  await user.getByRole('button', { name: /取\s*消/ }).click();
  for (const route of ['/settings', '/budgets', '/stats']) {
    await page.goto(base + route);
    await page.locator('.page h2').first().waitFor();
    await fonts(route);
  }
  await page.goto(base + '/budgets');
  const monthInput = page.locator('.ant-picker input').first();
  await monthInput.tap();
  await panel.waitFor();
  assert(await monthInput.evaluate((element) => element.readOnly), 'month selector suppresses the keyboard');
  await panel.locator('.ant-picker-cell-inner').first().waitFor();
  const months = await panel.locator('.ant-picker-cell-inner').allTextContents();
  assert.equal(months.length, 12);
  assert(months.every((month) => month.includes('月')), 'all month names are Chinese');
  await shot(page, 'date-month-mobile');
  await page.keyboard.press('Escape');

  const tablet = await browser.newContext({ viewport: { width: 820, height: 1180 }, isMobile: true, hasTouch: true });
  const tabletPage = await tablet.newPage();
  await login(tabletPage, base);
  await tabletPage.goto(base + '/');
  await tabletPage.getByRole('button', { name: /快捷记账$/ }).click();
  const tabletDate = tabletPage.getByRole('dialog', { name: '记一笔', exact: true }).locator('.ant-picker input');
  await tabletDate.tap();
  assert(await tabletDate.evaluate((element) => element.readOnly), 'touch tablet date suppresses the keyboard above 768px');
  await fonts('touch tablet', tabletPage);
  await tablet.close();

  const desktop = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const desktopPage = await desktop.newPage();
  await login(desktopPage, base);
  await desktopPage.goto(base + '/');
  await desktopPage.getByRole('button', { name: /快捷记账$/ }).click();
  const desktopDate = desktopPage.getByRole('dialog', { name: '记一笔', exact: true }).locator('.ant-picker input');
  assert(!(await desktopDate.evaluate((element) => element.readOnly)), 'desktop retains typed date entry');
  assert.equal(await desktopDate.evaluate((element) => getComputedStyle(element).fontSize), '14px', 'desktop control density remains');
  await desktopDate.fill(selectedDate);
  await desktopDate.press('Enter');
  assert.equal(await desktopDate.inputValue(), selectedDate);
  await desktop.close();
  assert.deepEqual(failures, [], 'mobile date/input regressions');
  await context.close();
}, 'chromium');
