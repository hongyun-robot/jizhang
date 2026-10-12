import assert from 'node:assert/strict';
import {
  withFixture,
  fixtureApi,
  login,
  until,
  shot,
} from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const prefix = `选择验收-${Date.now()}`;
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  await context.addInitScript(() => {
    const viewport = new EventTarget();
    Object.assign(viewport, {
      height: 844,
      width: 390,
      offsetTop: 0,
      offsetLeft: 0,
      scale: 1,
    });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const dialog = (name) => page.getByRole('dialog', { name, exact: true });
  const settle = async () =>
    page.locator('body').evaluate(async (element) => {
      await Promise.all(
        element
          .getAnimations({ subtree: true })
          .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
          .map((a) => a.finished.catch(() => {}))
      );
    });
  const noInputFocus = async () => {
    await settle();
    assert(
      await page.evaluate(
        () =>
          !document.activeElement.matches(
            'input,textarea,[contenteditable=true]'
          )
      ),
      'opening/refreshing must not focus text input'
    );
  };
  const closeSheet = async (sheet) => {
    await sheet.locator('.ant-drawer-close').click();
    await sheet.waitFor({ state: 'hidden' });
  };
  const openSheet = async (trigger, name) => {
    await trigger.click();
    const sheet = dialog(`选择${name}`);
    await sheet.waitFor();
    await noInputFocus();
    return sheet;
  };
  const accountsBefore = await api('GET', '/accounts');
  const cash = accountsBefore.find((a) => a.name === '测试现金');
  const platform = `${prefix}-商家`;
  await api('POST', '/voucher-platforms', { name: platform });
  try {
    await login(page, base);
    await page.goto(base + '/records');
    await page
      .locator('.page-head')
      .getByRole('button', { name: /记一笔$/ })
      .click();
    const form = dialog('记一笔');
    const account = form
      .locator('.selection-trigger:not(.voucher-platform-trigger)')
      .first();
    let sheet = await openSheet(account, '账户');
    await sheet.getByLabel('搜索选项').click();
    assert(
      await sheet
        .getByLabel('搜索选项')
        .evaluate((el) => document.activeElement === el)
    );
    await sheet.getByLabel('搜索选项').fill('不存在的账户');
    await sheet.getByText('无匹配选项', { exact: true }).waitFor();
    assert.equal(await sheet.getByRole('button', { name: /创建/ }).count(), 1);
    await closeSheet(sheet);
    assert.equal(await account.innerText(), '账户');
    sheet = await openSheet(account, '账户');
    assert.equal(await sheet.getByLabel('搜索选项').inputValue(), '');
    const row = sheet.getByRole('button', { name: '测试现金', exact: true });
    assert((await row.boundingBox()).height >= 44);
    await row.click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(await account.innerText(), '测试现金');
    await until(
      () => account.evaluate((el) => document.activeElement === el),
      Boolean,
      'focus restored after closing animation'
    );
    await form.getByPlaceholder('0.00').click();
    sheet = await openSheet(account, '账户');
    await sheet.getByLabel('搜索选项').click();
    await page.evaluate(() => {
      visualViewport.height = 470;
      visualViewport.dispatchEvent(new Event('resize'));
    });
    await until(
      () => sheet.boundingBox(),
      (box) => box.y + box.height <= 471,
      'sheet above simulated keyboard'
    );
    const list = await sheet.locator('.selection-sheet-list').boundingBox();
    assert(list.height > 44, 'keyboard leaves a usable scrollable list');
    await shot(page, 'selection-keyboard-mobile');
    await page.keyboard.press('Escape');
    await sheet.waitFor({ state: 'hidden' });
    assert(await form.isVisible(), 'Escape must only close the selection');
    await page.evaluate(() => {
      visualViewport.height = 844;
      visualViewport.dispatchEvent(new Event('resize'));
    });

    await form.getByText('转账', { exact: true }).click();
    sheet = await openSheet(
      form.getByRole('button', { name: '转入账户', exact: true }),
      '转入账户'
    );
    assert.equal(
      await sheet
        .getByRole('button', { name: '测试现金', exact: true })
        .count(),
      0,
      'same-account transfer stays excluded'
    );
    await closeSheet(sheet);
    await form.getByText('支出', { exact: true }).click();
    const platformField = form.getByLabel('平台 / 商家', { exact: true });
    sheet = await openSheet(platformField, '平台 / 商家');
    await sheet.getByRole('button', { name: platform, exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    sheet = await openSheet(platformField, '平台 / 商家');
    await sheet.getByLabel('搜索，或输入新名称创建').fill(`${prefix}-未确认`);
    await closeSheet(sheet);
    assert.equal(await platformField.innerText(), platform);
    assert(
      !(await api('GET', '/voucher-platforms')).some((p) =>
        p.name.endsWith('-未确认')
      )
    );
    sheet = await openSheet(platformField, '平台 / 商家');
    await sheet
      .getByRole('button', { name: '清空平台 / 商家', exact: true })
      .click();
    await sheet.waitFor({ state: 'hidden' });
    let posts = 0;
    await page.route('**/api/v1/voucher-platforms', async (route) => {
      if (route.request().method() === 'GET') await route.continue();
      else {
        posts++;
        await route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: JSON.stringify({ code: 503, message: '测试创建失败' }),
        });
      }
    });
    sheet = await openSheet(platformField, '平台 / 商家');
    await sheet.getByLabel('搜索，或输入新名称创建').fill('😀'.repeat(129));
    assert.equal(
      await sheet.getByRole('button', { name: /创建「/ }).count(),
      0
    );
    await sheet.getByLabel('搜索，或输入新名称创建').fill(`${prefix}-失败草稿`);
    await sheet.getByRole('button', { name: /创建「/ }).click();
    await sheet.getByText('测试创建失败', { exact: true }).waitFor();
    assert.equal(posts, 1);
    await sheet
      .getByRole('button', { name: '使用此名称', exact: true })
      .click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(await platformField.innerText(), `${prefix}-失败草稿`);
    await page.unroute('**/api/v1/voucher-platforms');
    let release;
    const held = new Promise((resolve) => {
      release = resolve;
    });
    let committed;
    const started = new Promise((resolve) => {
      committed = resolve;
    });
    await page.route('**/api/v1/voucher-platforms', async (route) => {
      if (route.request().method() === 'GET') return route.continue();
      const response = await route.fetch();
      committed();
      await held;
      await route.fulfill({ response });
    });
    sheet = await openSheet(platformField, '平台 / 商家');
    const delayedName = `${prefix}-迟到`;
    await sheet.getByLabel('搜索，或输入新名称创建').fill(delayedName);
    await sheet
      .getByRole('button', { name: `创建「${delayedName}」`, exact: true })
      .click();
    await started;
    await closeSheet(sheet);
    release();
    await until(
      () => api('GET', '/voucher-platforms'),
      (rows) => rows.some((row) => row.name === delayedName),
      'delayed creation persists'
    );
    await settle();
    assert.equal(
      await platformField.innerText(),
      `${prefix}-失败草稿`,
      'late success must not overwrite a closed selection'
    );
    await page.unroute('**/api/v1/voucher-platforms');
    await form.getByRole('button', { name: /取\s*消/ }).click();
    await form.waitFor({ state: 'hidden' });
    console.log(
      'PASS mobile account search/cancel/selection, safe focus, simulated keyboard, transfer exclusion, platform creation guards/failure/late response'
    );

    const parent = await api('POST', '/categories', {
      name: prefix,
      kind: 'expense',
    });
    cleanup(() => api('DELETE', `/categories/${parent.id}`));
    const child = await api('POST', '/categories', {
      name: '同名子项',
      kind: 'expense',
      parent_id: parent.id,
    });
    cleanup(() => api('DELETE', `/categories/${child.id}`));
    await page.goto(base + '/records');
    await page.getByRole('button', { name: /筛选/ }).first().click();
    const filter = dialog('筛选');
    sheet = await openSheet(
      filter.getByRole('button', { name: '分类', exact: true }),
      '分类'
    );
    await sheet.getByRole('button', { name: prefix, exact: true }).click();
    assert(
      !new URL(page.url()).searchParams.has('category_id'),
      'browsing a group must not select its virtual ID'
    );
    await sheet.getByRole('button', { name: '同名子项', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await until(
      () => page.url(),
      (url) =>
        new URL(url).searchParams.get('category_id') === String(child.id),
      'leaf category URL'
    );
    sheet = await openSheet(
      filter.getByRole('button', { name: '分类', exact: true }),
      '分类'
    );
    await sheet.getByLabel('搜索选项').fill('同名');
    await sheet
      .getByRole('button', { name: `${prefix} / 同名子项`, exact: true })
      .waitFor();
    await sheet.getByRole('button', { name: '清空分类', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert(await filter.isVisible());
    assert(!new URL(page.url()).searchParams.has('category_id'));
    await filter.locator('.ant-drawer-close').click();
    await page.goto(base + '/vouchers');
    sheet = await openSheet(
      page.getByRole('button', { name: '全部状态', exact: true }),
      '券状态'
    );
    assert.equal(await sheet.getByRole('textbox').count(), 0);
    assert.equal(
      await sheet
        .getByRole('button', { name: '全部状态', exact: true })
        .getAttribute('aria-pressed'),
      'true'
    );
    await sheet.getByRole('button', { name: '待使用', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await page.getByRole('button', { name: /登记购券$/ }).click();
    const voucher = dialog('登记购券');
    sheet = await openSheet(
      voucher.getByLabel('支付账户', { exact: true }),
      '支付账户'
    );
    await sheet.getByRole('button', { name: '测试现金', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(
      await voucher.getByLabel('支付账户', { exact: true }).innerText(),
      '测试现金'
    );
    sheet = await openSheet(
      voucher.getByLabel('支付账户', { exact: true }),
      '支付账户'
    );
    await shot(page, 'selection-voucher-mobile-light');
    await closeSheet(sheet);
    await voucher.getByRole('button', { name: /取\s*消/ }).click();
    console.log(
      'PASS nested filter/category leaf/search/clear, enum empty string, high voucher modal selection, light/dark mobile screenshots'
    );

    await page.goto(base + '/liabilities');
    await page.getByRole('button', { name: /新增负债账户/ }).click();
    const personal = dialog('新增负债账户');
    sheet = await openSheet(
      personal.getByLabel('还款月份', { exact: true }),
      '还款月份'
    );
    await sheet.getByRole('button', { name: '出账当月', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(
      await personal.getByLabel('还款月份', { exact: true }).innerText(),
      '出账当月'
    );
    await personal.getByRole('button', { name: /取\s*消/ }).click();
    await page.goto(base + '/recurring');
    await page.getByRole('button', { name: /新建周期/ }).click();
    const recurring = dialog('新建周期模板');
    sheet = await openSheet(
      recurring.getByRole('button', { name: '账户', exact: true }),
      '账户'
    );
    await sheet.getByRole('button', { name: '测试现金', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(
      await recurring.locator('.selection-trigger').innerText(),
      '测试现金'
    );
    await recurring.getByRole('button', { name: /取\s*消/ }).click();
    const rule = await api('POST', '/recurring', {
      amount: '7.00',
      freq: 'monthly',
      account_id: cash.id,
      category_id: child.id,
      start_date: '2030-01-01',
      note: prefix,
    });
    cleanup(() => api('DELETE', `/recurring/${rule.id}`));
    await page.reload();
    await page
      .locator('.rule-row')
      .filter({ hasText: prefix })
      .getByRole('button', { name: /编\s*辑/ })
      .click();
    const editRule = dialog('编辑周期模板');
    sheet = await openSheet(editRule.locator('.selection-trigger'), '账户');
    await sheet.getByRole('button', { name: '测试现金', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await editRule.getByRole('button', { name: /保\s*存/ }).click();
    await editRule.waitFor({ state: 'hidden' });
    assert.equal(
      (await api('GET', '/recurring')).find((r) => r.id === rule.id).account_id,
      cash.id
    );
    await page.goto(base + '/mine');
    await page.getByRole('button', { name: '用户管理', exact: true }).click();
    const adminRow = page
      .locator('.acct-row')
      .filter({ hasText: 'voucher-admin' });
    await adminRow.waitFor();
    assert(
      await adminRow.locator('.selection-trigger').isDisabled(),
      'last administrator stays locked'
    );
    await page.getByRole('button', { name: /新建用户/ }).click();
    const userForm = dialog('新建用户');
    sheet = await openSheet(
      userForm.getByLabel('角色', { exact: true }),
      '角色'
    );
    assert.equal(await sheet.getByRole('textbox').count(), 0);
    await sheet.getByRole('button', { name: '只读成员', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    assert.equal(
      await userForm.getByLabel('角色', { exact: true }).innerText(),
      '只读成员'
    );
    await userForm.getByRole('button', { name: /取\s*消/ }).click();
    const user = await api('POST', '/users', {
      username: `selection_${Date.now()}`,
      password: 'selection-test-only',
      role: 'viewer',
    });
    cleanup(() =>
      api('PATCH', `/users/${user.id}/status`, { status: 'disabled' })
    );
    await page.reload();
    await page.getByRole('button', { name: '用户管理', exact: true }).click();
    const userRow = page
      .locator('.acct-row')
      .filter({ hasText: user.username });
    sheet = await openSheet(userRow.locator('.selection-trigger'), '角色');
    await sheet.getByRole('button', { name: '普通成员', exact: true }).click();
    await sheet.waitFor({ state: 'hidden' });
    await until(
      () => api('GET', '/users'),
      (users) => users.find((u) => u.id === user.id)?.role === 'member',
      'immediate mobile role update'
    );
    console.log(
      'PASS repayment numeric zero, recurring create/edit account choice and saved ID, immediate mobile role update and last-administrator protection'
    );

    for (const width of [360, 390, 767, 768, 991, 992]) {
      await page.setViewportSize({ width, height: 844 });
      await page.goto(base + '/vouchers');
      if (width < 768) {
        sheet = await openSheet(
          page.getByRole('button', { name: '全部状态', exact: true }),
          '券状态'
        );
        await closeSheet(sheet);
      } else {
        const select = page.locator('.filter-bar .ant-select');
        await select.click();
        await page.locator('.ant-select-dropdown:visible').waitFor();
        assert.equal(await page.locator('.selection-sheet:visible').count(), 0);
        await page.keyboard.press('Escape');
        if (width === 992) await shot(page, 'selection-desktop-light');
      }
    }
    assert.deepEqual(errors, []);
    assert.equal(
      (await api('GET', '/accounts')).find((a) => a.id === cash.id).balance,
      cash.balance,
      'selection and cancellation never change money'
    );
    console.log(
      'PASS six breakpoints and desktop preservation; no financial writes or page errors'
    );
  } catch (error) {
    await shot(page, 'selection-failure').catch(() => {});
    throw error;
  } finally {
    await context.close();
  }
});
