import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, login, shot, until, fixtureApi } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const prefix = `个人目录-${Date.now()}`;
await withFixture(base, async (browser, cleanup) => {
  const clients = [];
  for (const username of ['voucher-admin', 'voucher-recorder', 'voucher-member', 'voucher-viewer']) {
    const request = await fixtureApi(base, username);
    const input = { name: prefix + '-同名账户', type: 'asset', opening_balance: '12', request_key: randomUUID(), owner_id: 999 };
    const account = await request('POST', '/accounts', input);
    assert.notEqual(account.owner_id, 999);
    assert.equal((await request('POST', '/accounts', input)).id, account.id);
    await request('POST', '/accounts', { ...input, name: '不同资料' }, 409);
    cleanup(() => request('DELETE', `/accounts/${account.id}`));
    const category = await request('POST', '/categories', { kind: 'expense', name: prefix + '-同名分类' });
    cleanup(() => request('DELETE', `/categories/${category.id}`));
    const child = await request('POST', '/categories', { kind: 'expense', name: '子分类', parent_id: category.id });
    cleanup(() => request('DELETE', `/categories/${child.id}`));
    await request('PUT', `/categories/${category.id}`, { name: prefix + '-已修改', sort: 9 });
    const ownAccounts = await request('GET', '/accounts');
    assert(ownAccounts.every((row) => row.owner_id === account.owner_id));
    if (username === 'voucher-viewer') {
      await request('POST', '/transactions', { kind: 'expense', amount: '1', account_id: account.id, category_id: child.id, entry_date: '2026-10-12' }, 403);
      await request('PUT', '/budgets/2026-10', { total: '100' });
      cleanup(() => request('PUT', '/budgets/2026-10', {}));
    }
    clients.push({ username, request, account, category, child });
  }
  assert.equal(new Set(clients.map((client) => client.account.id)).size, 4);
  assert.equal(new Set(clients.map((client) => client.category.id)).size, 4);
  for (const client of clients) {
    const other = clients.find((item) => item !== client);
    await client.request('PUT', `/accounts/${other.account.id}`, { name: '越权', type: 'asset', opening_balance: '12' }, 403);
    await client.request('DELETE', `/accounts/${other.account.id}`, undefined, 403);
    await client.request('PUT', `/categories/${other.category.id}`, { name: '越权' }, 403);
    await client.request('POST', '/categories', { kind: 'expense', name: '越权子分类', parent_id: other.category.id }, 403);
  }
  const merchant = prefix + '-个人商家';
  await clients[0].request('POST', '/voucher-platforms', { name: merchant });
  for (const [index, client] of clients.entries()) assert.equal((await client.request('GET', '/voucher-platforms')).some((row) => row.name === merchant), index === 0);
  for (const endpoint of ['/accounts', '/categories']) assert.equal((await fetch(base + '/api/v1' + endpoint)).status, 401);
  console.log('PASS four-role personal CRUD, cross-account denial, account creation replay, viewer funds restriction, private merchants and anonymous denial');

  const viewerContext = await browser.newContext({ viewport: { width: 390, height: 900 } });
  const viewerPage = await viewerContext.newPage();
  await login(viewerPage, base, 'voucher-viewer');
  await viewerPage.goto(base + '/mine');
  await viewerPage.getByText('账户管理', { exact: true }).click();
  await viewerPage.locator('.page-head').getByRole('button', { name: /新建账户$/ }).click();
  const viewerEditor = viewerPage.getByRole('dialog', { name: '新建账户', exact: true });
  await viewerEditor.getByLabel('名称', { exact: true }).fill(prefix + '-只读账户');
  await viewerEditor.getByRole('button', { name: /保\s*存/ }).click();
  await viewerEditor.waitFor({ state: 'hidden' });
  const viewerAccount = (await clients[3].request('GET', '/accounts')).find((row) => row.name === prefix + '-只读账户');
  assert(viewerAccount);
  cleanup(() => clients[3].request('DELETE', `/accounts/${viewerAccount.id}`));
  await viewerPage.goto(base + '/records');
  assert.equal(await viewerPage.locator('.page-head').getByRole('button', { name: /记一笔$/ }).count(), 0);
  await viewerContext.close();

  for (const width of [1280, 360, 390, 767, 768]) {
    const context = await browser.newContext({ viewport: { width, height: 900 } });
    if (width < 768) await context.addInitScript(() => {
      const viewport = new EventTarget();
      Object.assign(viewport, { height: 900, width: window.innerWidth, offsetTop: 0, offsetLeft: 0, scale: 1 });
      Object.defineProperty(window, 'visualViewport', { value: viewport, configurable: true });
      window.__catalogKeyboard = (height) => { viewport.height = height; viewport.dispatchEvent(new Event('resize')); };
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await login(page, base, 'voucher-member');
    await page.goto(base + '/mine');
    await page.getByText('分类管理', { exact: true }).click();
    await page.locator('.page h2').filter({ hasText: '分类' }).waitFor();
    await shot(page, `personal-categories-${width}`);
    await page.goto(base + '/records');
    await page.locator('.page-head').getByRole('button', { name: /记一笔$/ }).click();
    const form = page.getByRole('dialog', { name: '记一笔', exact: true });
    await form.waitFor();
    const amount = form.getByPlaceholder('0.00');
    await amount.fill('23');
    const openAccount = async () => {
      if (width < 768) {
        await form.locator('.selection-trigger:not(.voucher-platform-trigger)').first().click();
        const sheet = page.getByRole('dialog', { name: '选择账户', exact: true });
        await sheet.waitFor();
        assert.equal(await sheet.getByLabel('搜索选项').evaluate((node) => document.activeElement === node), false);
        await sheet.getByRole('button', { name: '新建账户', exact: true }).click();
      } else {
        if (!(await page.getByRole('button', { name: '新建账户', exact: true }).isVisible())) await form.locator('.ant-select').last().click();
        await page.getByRole('button', { name: '新建账户', exact: true }).click();
      }
      const editor = page.getByRole('dialog', { name: '新建账户', exact: true });
      await editor.waitFor();
      if (width < 768) assert.equal(await editor.locator('input').evaluateAll((nodes) => nodes.some((node) => document.activeElement === node)), false);
      return editor;
    };
    let editor = await openAccount();
    await shot(page, `personal-account-editor-${width}`);
    const uiName = prefix + '-字段-' + width;
    await editor.getByLabel('名称', { exact: true }).fill(uiName);
    if (width === 390) {
      await page.evaluate(() => window.__catalogKeyboard(420));
      await page.waitForTimeout(300);
      const saveBounds = await editor.getByRole('button', { name: /保\s*存/ }).boundingBox();
      await shot(page, 'personal-account-keyboard-390');
      assert(saveBounds.y + saveBounds.height <= 420, 'keyboard obscured account save');
      await page.evaluate(() => window.__catalogKeyboard(900));
    }
    await shot(page, `personal-account-create-${width}`);
    if (width < 768) await editor.press('Escape');
    else await editor.getByRole('button', { name: /取\s*消/ }).click();
    await editor.waitFor({ state: 'hidden' });
    assert.equal(await amount.inputValue(), '23');
    assert(!(await clients[2].request('GET', '/accounts')).some((row) => row.name === uiName));
    if (width < 768) await page.getByRole('dialog', { name: '选择账户', exact: true }).locator('.ant-drawer-close').click();
    editor = await openAccount();
    await editor.getByLabel('名称', { exact: true }).fill(uiName);
    await editor.getByRole('button', { name: /保\s*存/ }).click();
    await editor.waitFor({ state: 'hidden' });
    assert.equal(await amount.inputValue(), '23');
    await until(() => clients[2].request('GET', '/accounts'), (rows) => rows.some((row) => row.name === uiName), 'created personal account');
    const saved = (await clients[2].request('GET', '/accounts')).find((row) => row.name === uiName);
    cleanup(() => clients[2].request('DELETE', `/accounts/${saved.id}`));
    assert((await form.innerText()).includes(uiName));
    if (width < 768) assert.equal(await page.getByRole('dialog', { name: '选择账户', exact: true }).count(), 0);
    await form.getByRole('button', { name: /取\s*消/ }).click();
    await form.waitFor({ state: 'hidden' });
    assert((await clients[2].request('GET', '/accounts')).some((row) => row.id === saved.id));
    assert.equal(saved.opening_balance, '0');
    if (width === 390) {
      await page.locator('.page-head').getByRole('button', { name: /记一笔$/ }).click();
      await amount.fill('47');
      editor = await openAccount();
      const retryName = prefix + '-重试';
      await editor.getByLabel('名称', { exact: true }).fill(retryName);
      let attempts = 0;
      const keys = [];
      await page.route('**/api/v1/accounts', async (route) => {
        if (route.request().method() !== 'POST') return route.continue();
        keys.push(route.request().postDataJSON().request_key);
        attempts++;
        if (attempts === 1) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 10000, message: '模拟建户失败' }) });
        const response = await route.fetch();
        if (attempts === 2) return route.abort('failed');
        return route.fulfill({ response });
      });
      await editor.getByRole('button', { name: /保\s*存/ }).click();
      await page.getByText('模拟建户失败', { exact: true }).waitFor();
      assert.equal(await editor.getByLabel('名称', { exact: true }).inputValue(), retryName);
      await editor.getByRole('button', { name: /保\s*存/ }).click();
      await until(() => clients[2].request('GET', '/accounts'), (rows) => rows.some((row) => row.name === retryName), 'saved account with lost response');
      await until(() => editor.getByRole('button', { name: /保\s*存/ }).getAttribute('class'), (value) => !value.includes('ant-btn-loading'), 'retry available');
      await page.waitForTimeout(100);
      await editor.getByRole('button', { name: /保\s*存/ }).click();
      await until(() => attempts, (value) => value === 3, 'third creation attempt');
      await editor.waitFor({ state: 'hidden' });
      await page.unroute('**/api/v1/accounts');
      const retryRows = (await clients[2].request('GET', '/accounts')).filter((row) => row.name === retryName);
      assert.equal(retryRows.length, 1);
      assert.equal(keys.length, 3);
      assert.equal(new Set(keys).size, 1);
      cleanup(() => clients[2].request('DELETE', `/accounts/${retryRows[0].id}`));
      assert.equal(await amount.inputValue(), '47');
      await form.getByRole('button', { name: /取\s*消/ }).click();

      await page.locator('.page-head').getByRole('button', { name: /记一笔$/ }).click();
      await amount.fill('61');
      editor = await openAccount();
      const lateName = prefix + '-迟到';
      await editor.getByLabel('名称', { exact: true }).fill(lateName);
      let release;
      let committed;
      const committedPromise = new Promise((resolve) => { committed = resolve; });
      const responseGate = new Promise((resolve) => { release = resolve; });
      await page.route('**/api/v1/accounts', async (route) => {
        if (route.request().method() !== 'POST') return route.continue();
        const response = await route.fetch();
        committed();
        await responseGate;
        return route.fulfill({ response });
      });
      await editor.getByRole('button', { name: /保\s*存/ }).click();
      await committedPromise;
      await editor.getByRole('button', { name: /取\s*消/ }).click();
      await editor.waitFor({ state: 'hidden' });
      await page.getByRole('dialog', { name: '选择账户', exact: true }).locator('.ant-drawer-close').click();
      await form.getByRole('button', { name: /取\s*消/ }).click();
      await page.locator('.page-head').getByRole('button', { name: /记一笔$/ }).click();
      await amount.fill('79');
      release();
      await until(() => clients[2].request('GET', '/accounts'), (rows) => rows.some((row) => row.name === lateName), 'late account persisted');
      await page.waitForTimeout(500);
      assert.equal(await amount.inputValue(), '79');
      assert(!(await form.innerText()).includes(lateName));
      assert.equal(await page.getByRole('dialog', { name: '新建账户', exact: true }).count(), 0);
      const lateAccount = (await clients[2].request('GET', '/accounts')).find((row) => row.name === lateName);
      cleanup(() => clients[2].request('DELETE', `/accounts/${lateAccount.id}`));
      await page.unroute('**/api/v1/accounts');
      await form.getByRole('button', { name: /取\s*消/ }).click();
    }
    assert.equal(errors.length, 0, errors.join('\n'));
    await context.close();
  }
  console.log('PASS desktop/mobile account creation, cancellation, parent draft, numeric ID persistence, personal categories and selection breakpoint');
}, 'chromium');
