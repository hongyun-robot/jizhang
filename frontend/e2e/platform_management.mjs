import assert from 'node:assert/strict';
import { withFixture, login, until, shot } from './support/fixture.mjs';

const base = process.env.PLATFORM_E2E_BASE ?? 'http://127.0.0.1:5601';
const prefix = `商家管理-${Date.now()}`;
await withFixture(base, async (browser, cleanup) => {
  const clients = {};
  for (const role of ['admin', 'recorder', 'member', 'viewer']) {
    const auth = await fetch(base + '/api/v1/auth/login', {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ username: `voucher-${role}`, password: 'voucher-test-only' }),
    });
    assert.equal(auth.status, 200);
    const token = (await auth.json()).data.access_token;
    clients[role] = async (method, endpoint, input, status = 200) => {
      const response = await fetch(base + '/api/v1' + endpoint, {
        method, headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' },
        ...(input === undefined ? {} : { body: JSON.stringify(input) }),
      });
      const body = await response.json();
      assert.equal(response.status, status, `${method} ${endpoint}: ${JSON.stringify(body)}`);
      return body.data;
    };
  }
  const directory = {};
  const accountsBefore = await clients.member('GET', '/accounts');
  const statsBefore = await clients.member('GET', '/stats/compare');
  for (const [role, api] of Object.entries(clients)) {
    const row = await api('POST', '/voucher-platforms', { name: ` ${prefix}-同名 ` });
    directory[role] = row;
    cleanup(() => api('DELETE', `/voucher-platforms/${row.id}`, undefined));
    const replay = await api('POST', '/voucher-platforms', { name: row.name });
    assert.equal(row.id, replay.id);
    const renamed = await api('PUT', `/voucher-platforms/${row.id}`, { name: `${prefix}-${role}` });
    assert.equal(renamed.id, row.id);
    await api('PUT', `/voucher-platforms/${row.id}`, { name: '微信' }, 409);
    await api('PUT', `/voucher-platforms/${row.id}`, { name: ' ' }, 400);
    const context = await browser.newContext({ viewport: { width: 390, height: 900 } });
    const page = await context.newPage();
    await login(page, base, `voucher-${role}`);
    await page.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).click();
    await page.getByRole('button', { name: '平台 / 商家管理', exact: true }).click();
    await page.getByRole('heading', { name: '平台 / 商家管理', exact: true }).waitFor();
    assert.equal(await page.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).getAttribute('aria-current'), 'page');
    await page.getByText(`${prefix}-${role}`, { exact: true }).waitFor();
    for (const other of Object.keys(clients).filter((item) => item !== role))
      assert.equal(await page.getByText(`${prefix}-${other}`, { exact: true }).count(), 0);
    await context.close();
  }
  assert.equal(new Set(Object.values(directory).map((row) => row.id)).size, 4);
  await clients.admin('PUT', `/voucher-platforms/${directory.member.id}`, { name: '越权' }, 403);
  await clients.member('DELETE', `/voucher-platforms/${directory.admin.id}`, undefined, 403);
  const viewerAccounts = await clients.viewer('GET', '/accounts');
  const viewerForest = await clients.viewer('GET', '/categories');
  const viewerCategory = viewerForest.flatMap((wrapper) => wrapper.children ?? [])
    .flatMap((root) => root.children?.length ? root.children : [root]).find((row) => row.kind === 'expense');
  await clients.viewer('POST', '/transactions', { kind: 'expense', amount: '1', entry_date: '2026-10-14',
    account_id: viewerAccounts.find((row) => row.type === 'asset').id, category_id: viewerCategory.id }, 403);
  await clients.member('POST', '/voucher-platforms', { name: '伪造', owner_id: 1 }, 400);
  for (const method of ['GET', 'POST', 'PUT', 'DELETE']) {
    const response = await fetch(base + '/api/v1/voucher-platforms' + (['PUT', 'DELETE'].includes(method) ? '/1' : ''), {
      method, ...(method === 'GET' || method === 'DELETE' ? {} : {
        headers: { 'content-type': 'application/json' }, body: JSON.stringify({ name: '未登录' }),
      }),
    });
    assert.equal(response.status, 401);
  }
  assert.deepEqual(await clients.member('GET', '/accounts'), accountsBefore);
  assert.deepEqual(await clients.member('GET', '/stats/compare'), statsBefore);
  console.log('PASS personal API: four roles, independent identities, owner spoofing, anonymous denial, financial permissions and Mine navigation');

  const memberForest = await clients.member('GET', '/categories');
  const memberCategory = memberForest.flatMap((wrapper) => wrapper.children ?? [])
    .flatMap((root) => root.children?.length ? root.children : [root]).find((row) => row.kind === 'expense');
  const reference = await clients.member('POST', '/transactions', { kind: 'expense', amount: '1', entry_date: '2026-10-14',
    account_id: accountsBefore.find((row) => row.type === 'asset').id, category_id: memberCategory.id,
    platform: `${prefix}-member`, note: prefix + '-引用保护' });
  cleanup(() => clients.member('DELETE', `/transactions/${reference.id}`));
  await clients.member('DELETE', `/voucher-platforms/${directory.member.id}`, undefined, 409);
  const referenceContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const referencePage = await referenceContext.newPage();
  await login(referencePage, base, 'voucher-member');
  await referencePage.goto(base + '/platforms');
  const referenceRow = referencePage.locator('.platform-row').filter({ hasText: `${prefix}-member` });
  await referenceRow.getByRole('button', { name: `删除 ${prefix}-member`, exact: true }).click();
  await referencePage.locator('.ant-popconfirm:visible').getByRole('button', { name: /确\s*定/ }).click();
  await referencePage.getByText('该平台 / 商家仍被本人记录使用，不能删除', { exact: true }).waitFor();
  await referenceRow.waitFor();
  await referenceContext.close();
  console.log('PASS referenced-name API/UI rejection preserves the directory and financial record');

  for (const width of [1280, 360, 767, 768, 390]) {
    const context = await browser.newContext({ viewport: { width, height: 900 } });
    if (width < 768) await context.addInitScript(() => {
      const viewport = new EventTarget();
      Object.assign(viewport, { height: 900, width: innerWidth, offsetTop: 0, offsetLeft: 0, scale: 1 });
      Object.defineProperty(window, 'visualViewport', { value: viewport, configurable: true });
      window.__platformKeyboard = (height) => { viewport.height = height; viewport.dispatchEvent(new Event('resize')); };
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await login(page, base, 'voucher-member');
    await page.goto(base + '/mine');
    if ([767, 390].includes(width)) await page.getByText('暗色', { exact: true }).click();
    await page.getByRole('button', { name: '平台 / 商家管理', exact: true }).click();
    const name = `${prefix}-${width}-${'长名称'.repeat(18)}`;
    let posts = 0;
    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().endsWith('/api/v1/voucher-platforms') && request.postDataJSON().name === name) posts++;
    });
    await page.getByRole('button', { name: '新建平台 / 商家', exact: true }).click();
    const editor = page.getByRole('dialog', { name: '新建平台 / 商家', exact: true });
    await editor.waitFor();
    if (width < 768) assert.equal(await editor.getByLabel('名称', { exact: true }).evaluate((node) => document.activeElement === node), false);
    await editor.getByLabel('名称', { exact: true }).fill('商'.repeat(129));
    await editor.getByRole('button', { name: /确\s*定/ }).click();
    await editor.getByText('名称须为 1 至 128 字', { exact: true }).waitFor();
    await editor.getByLabel('名称', { exact: true }).fill(name);
    if (width < 768) {
      await page.evaluate(() => window.__platformKeyboard(520));
      await until(() => editor.getByRole('button', { name: /确\s*定/ }).boundingBox(), (box) => box && box.y >= 0 && box.y + box.height <= 520, 'keyboard-safe save button');
    }
    await editor.getByRole('button', { name: /确\s*定/ }).evaluate((button) => { button.click(); button.click(); });
    await editor.waitFor({ state: 'hidden' });
    assert.equal(posts, 1, 'duplicate clicks created multiple requests');
    if (width < 768) await page.evaluate(() => window.__platformKeyboard(900));
    const search = page.getByRole('textbox', { name: '搜索平台 / 商家', exact: true });
    await search.fill(`${prefix}-${width}`);
    const row = page.locator('.platform-row').filter({ hasText: name });
    await row.waitFor();
    const rowBox = await row.boundingBox();
    assert(rowBox.x >= 0 && rowBox.x + rowBox.width <= width, `${width}px: row clipped`);
    for (const button of await row.getByRole('button').all()) {
      const box = await button.boundingBox();
      assert(box && box.x >= 0 && box.x + box.width <= width);
      if (width < 768) assert(box.height >= 40);
    }
    await page.getByText('已保存', { exact: true }).waitFor({ state: 'hidden' });
    await shot(page, `platform-management-${width}`);
    await row.getByRole('button', { name: `重命名 ${name}`, exact: true }).click();
    const rename = page.getByRole('dialog', { name: '重命名平台 / 商家', exact: true });
    await rename.getByLabel('名称', { exact: true }).fill('微信');
    await rename.getByRole('button', { name: /确\s*定/ }).click();
    await rename.getByText('本人目录中已存在同名平台 / 商家', { exact: true }).waitFor();
    assert.equal(await rename.getByLabel('名称', { exact: true }).inputValue(), '微信');
    const renamed = name + '-已改名';
    await rename.getByLabel('名称', { exact: true }).fill(renamed);
    await rename.getByRole('button', { name: /确\s*定/ }).click();
    await rename.waitFor({ state: 'hidden' });
    await page.getByText(renamed, { exact: true }).waitFor();
    const renamedRow = page.locator('.platform-row').filter({ hasText: renamed });
    await renamedRow.getByRole('button', { name: `删除 ${renamed}`, exact: true }).click();
    let confirmation = page.locator('.ant-popconfirm:visible');
    await confirmation.getByRole('button', { name: /取\s*消/ }).click();
    await renamedRow.waitFor();
    await renamedRow.getByRole('button', { name: `删除 ${renamed}`, exact: true }).click();
    confirmation = page.locator('.ant-popconfirm:visible');
    await confirmation.getByRole('button', { name: /确\s*定/ }).click();
    await page.getByText(renamed, { exact: true }).waitFor({ state: 'hidden' });
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log('PASS management UI: five viewport widths, light/dark, long names, validation, duplicate clicks, keyboard layout, rename and delete confirmation');

  const failureContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const failurePage = await failureContext.newPage();
  await login(failurePage, base, 'voucher-member');
  await failurePage.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'GET') await route.fulfill({ status: 500, json: { code: 20000, message: '测试目录加载失败' } });
    else await route.continue();
  });
  await failurePage.goto(base + '/platforms');
  await failurePage.getByText('测试目录加载失败', { exact: true }).waitFor();
  assert.equal(await failurePage.getByText('暂无平台 / 商家', { exact: true }).count(), 0);
  await failurePage.unroute('**/api/v1/voucher-platforms');
  await failurePage.getByRole('button', { name: /重\s*试/ }).click();
  await failurePage.getByRole('list', { name: '本人平台 / 商家列表', exact: true }).waitFor();
  await failurePage.getByRole('textbox', { name: '搜索平台 / 商家', exact: true }).fill('__无匹配的个人商家__');
  await failurePage.getByText('无匹配平台 / 商家', { exact: true }).waitFor();
  await failurePage.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'GET') await route.fulfill({ json: { code: 0, data: [] } });
    else await route.continue();
  });
  await failurePage.reload();
  await failurePage.getByText('暂无平台 / 商家', { exact: true }).waitFor();
  await failurePage.unroute('**/api/v1/voucher-platforms');
  await failurePage.reload();
  await failurePage.getByRole('list', { name: '本人平台 / 商家列表', exact: true }).waitFor();
  await failurePage.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'POST') await route.fulfill({ status: 500, json: { code: 20000, message: '测试保存失败' } });
    else await route.continue();
  });
  await failurePage.getByRole('button', { name: '新建平台 / 商家', exact: true }).click();
  const failedEditor = failurePage.getByRole('dialog', { name: '新建平台 / 商家', exact: true });
  await failedEditor.getByLabel('名称', { exact: true }).fill(prefix + '-失败草稿');
  await failedEditor.getByRole('button', { name: /确\s*定/ }).click();
  await failedEditor.getByText('测试保存失败', { exact: true }).waitFor();
  assert.equal(await failedEditor.getByLabel('名称', { exact: true }).inputValue(), prefix + '-失败草稿');
  await failedEditor.getByRole('button', { name: /取\s*消/ }).click();
  await failureContext.close();
  console.log('PASS failures: initial read error, retry, empty/search distinction and failed save preserves draft');

  const draftContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const draftPage = await draftContext.newPage();
  await login(draftPage, base, 'voucher-member');
  await draftPage.goto(base + '/platforms');
  const submittedName = prefix + '-已提交名称';
  const nextDraft = prefix + '-后续未提交草稿';
  let releaseDraft;
  let draftHeld = false;
  let draftDelivered = false;
  const draftGate = new Promise((resolve) => { releaseDraft = resolve; });
  await draftPage.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'POST' && route.request().postDataJSON().name === submittedName) {
      const response = await route.fetch();
      draftHeld = true;
      await draftGate;
      await route.fulfill({ response });
      draftDelivered = true;
    } else await route.continue();
  });
  try {
    await draftPage.getByRole('button', { name: '新建平台 / 商家', exact: true }).click();
    const draftEditor = draftPage.getByRole('dialog', { name: '新建平台 / 商家', exact: true });
    await draftEditor.getByLabel('名称', { exact: true }).fill(submittedName);
    await draftEditor.getByRole('button', { name: /确\s*定/ }).click();
    await until(() => draftHeld, Boolean, 'submitted name committed');
    const saved = (await clients.member('GET', '/voucher-platforms')).find((row) => row.name === submittedName);
    assert(saved);
    cleanup(() => clients.member('DELETE', `/voucher-platforms/${saved.id}`));
    await draftEditor.getByLabel('名称', { exact: true }).fill(nextDraft);
    releaseDraft();
    await until(() => draftDelivered, Boolean, 'old save delivered');
    await until(() => draftEditor.getByRole('button', { name: /确\s*定/ }).isEnabled(), Boolean, 'save settles without discarding new draft');
    assert.equal(await draftEditor.getByLabel('名称', { exact: true }).inputValue(), nextDraft);
    assert(!(await clients.member('GET', '/voucher-platforms')).some((row) => row.name === nextDraft));
    await draftEditor.getByRole('button', { name: /取\s*消/ }).click();
  } finally {
    releaseDraft();
    await draftContext.close();
  }
  console.log('PASS delayed save preserves a later unsubmitted name draft');

  const returnContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const returnPage = await returnContext.newPage();
  await returnPage.goto(base + '/platforms');
  await returnPage.getByPlaceholder('用户名').fill('voucher-viewer');
  await returnPage.getByPlaceholder('密码').fill('voucher-test-only');
  await returnPage.getByRole('button', { name: /登\s*录/ }).click();
  await returnPage.waitForURL(base + '/platforms');
  await returnPage.getByRole('heading', { name: '平台 / 商家管理', exact: true }).waitFor();
  assert.equal(await returnPage.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).getAttribute('aria-current'), 'page');
  await returnContext.close();

  const lateContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const latePage = await lateContext.newPage();
  await login(latePage, base, 'voucher-member');
  await latePage.goto(base + '/platforms');
  const lateName = prefix + '-旧账号迟到创建';
  let release;
  let held = false;
  let delivered = false;
  const gate = new Promise((resolve) => { release = resolve; });
  await latePage.route('**/api/v1/voucher-platforms', async (route) => {
    if (route.request().method() === 'POST' && route.request().postDataJSON().name === lateName) {
      const response = await route.fetch();
      held = true;
      await gate;
      await route.fulfill({ response });
      delivered = true;
    } else await route.continue();
  });
  try {
    await latePage.getByRole('button', { name: '新建平台 / 商家', exact: true }).click();
    const lateEditor = latePage.getByRole('dialog', { name: '新建平台 / 商家', exact: true });
    await lateEditor.getByLabel('名称', { exact: true }).fill(lateName);
    await lateEditor.getByRole('button', { name: /确\s*定/ }).click();
    await until(() => held, Boolean, 'committed response held');
    const committed = (await clients.member('GET', '/voucher-platforms')).find((row) => row.name === lateName);
    assert(committed);
    cleanup(() => clients.member('DELETE', `/voucher-platforms/${committed.id}`));
    await lateEditor.getByRole('button', { name: /取\s*消/ }).click();
    await lateEditor.waitFor({ state: 'hidden' });
    await latePage.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).click();
    await latePage.getByRole('button', { name: /退出登录$/ }).click();
    await latePage.getByPlaceholder('用户名').fill('voucher-viewer');
    await latePage.getByPlaceholder('密码').fill('voucher-test-only');
    await latePage.getByRole('button', { name: /登\s*录/ }).click();
    await latePage.waitForURL((url) => !url.pathname.endsWith('/login'));
    await latePage.locator('.bottom-tabs').getByRole('button', { name: /我的$/ }).click();
    await latePage.getByRole('button', { name: '平台 / 商家管理', exact: true }).click();
    await latePage.getByRole('list', { name: '本人平台 / 商家列表', exact: true }).waitFor();
    release();
    await until(() => delivered, Boolean, 'late response delivered');
    assert.equal(await latePage.getByText(lateName, { exact: true }).count(), 0);
    assert.equal(await latePage.getByText('已保存', { exact: true }).count(), 0);
    assert(!(await clients.viewer('GET', '/voucher-platforms')).some((row) => row.name === lateName));
  } finally {
    release();
    await lateContext.close();
  }
  console.log('PASS login return and committed late mutation after logout/account switch');
});
