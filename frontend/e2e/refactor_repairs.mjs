import assert from 'node:assert/strict';
import { withFixture, fixtureApi, login, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
await withFixture(base, async (browser, cleanup) => {
  const api = await fixtureApi(base);
  const asset = (await api('GET', '/accounts')).find(
    (account) => account.type === 'asset' && account.status === 'active'
  );
  const context = await browser.newContext({
    viewport: { width: 1280, height: 900 },
  });
  const page = await context.newPage();
  await login(page, base);

  for (const kind of ['transaction', 'recurring']) {
    for (const outcome of ['success', 'failure']) {
      const prefix = `repair-${kind}-${outcome}-${crypto.randomUUID()}`;
      const endpoint = kind === 'transaction' ? '/transactions' : '/recurring';
      await page.goto(
        base + (kind === 'transaction' ? '/records' : '/recurring')
      );
      const dialog = page.getByRole('dialog', {
        name: kind === 'transaction' ? '记一笔' : '新建周期模板',
        exact: true,
      });
      const open = () =>
        page
          .locator('.page-head')
          .getByRole('button', {
            name: kind === 'transaction' ? /记一笔/ : /新建周期/,
          })
          .click();
      const note = () =>
        dialog.getByPlaceholder(
          kind === 'transaction'
            ? '备注（可选）'
            : '备注（可选），将写入生成的流水'
        );
      const save = () =>
        dialog
          .getByRole('button', {
            name: kind === 'transaction' ? /记\s*账/ : /保\s*存/,
            exact: true,
          })
          .click();
      const fill = async (name) => {
        await dialog.getByPlaceholder('0.00').fill('2');
        await dialog.locator('.cat-leaves .ant-tag').first().click();
        const account =
          kind === 'transaction'
            ? dialog
                .locator('.ant-select')
                .filter({ has: page.getByText('账户', { exact: true }) })
            : dialog.locator('.ant-select');
        await account.click();
        await page
          .locator(
            '.ant-select-dropdown:visible .ant-select-item-option-content'
          )
          .filter({ hasText: asset.name })
          .first()
          .click();
        if (kind === 'recurring') {
          await dialog.getByPlaceholder('首次生成日期').fill('2099-01-01');
          await dialog.getByPlaceholder('首次生成日期').press('Enter');
        }
        await note().fill(name);
      };
      let release;
      let started = false;
      let old;
      let requests = 0;
      const held = new Promise((resolve) => {
        release = resolve;
      });
      const pattern = '**/api/v1' + endpoint;
      await page.route(pattern, async (route) => {
        if (route.request().method() !== 'POST' || ++requests > 1) {
          await route.continue();
          return;
        }
        if (outcome === 'success') {
          const response = await route.fetch();
          assert.equal(response.status(), 200);
          old = (await response.json()).data;
          started = true;
          await held;
          await route.fulfill({ response });
        } else {
          started = true;
          await held;
          await route.fulfill({
            status: 500,
            json: { code: 20000, message: 'repair-old-failure' },
          });
        }
      });
      cleanup(async () => {
        release();
        if (old?.id) await api('DELETE', endpoint + '/' + old.id);
      });
      await open();
      await fill(prefix + '-old');
      await save();
      await until(() => started, Boolean, 'old attempt started');
      await dialog.getByRole('button', { name: /取\s*消/ }).click();
      await dialog.waitFor({ state: 'hidden' });
      await open();
      await note().fill(prefix + '-new-draft');
      const response = page.waitForResponse(
        (reply) =>
          reply.request().method() === 'POST' &&
          new URL(reply.url()).pathname === '/api/v1' + endpoint
      );
      release();
      await response;
      await until(
        () =>
          page
            .locator(
              '.ant-modal-footer .ant-btn-primary.ant-btn-loading:visible'
            )
            .count(),
        (count) => count === 0,
        'old attempt finished'
      );
      assert(
        await dialog.isVisible(),
        'old response must not close the new editor'
      );
      assert.equal(await note().inputValue(), prefix + '-new-draft');
      assert.equal(
        await page.getByText('repair-old-failure', { exact: true }).count(),
        0,
        'old errors must not appear in the new editor'
      );
      await page.unroute(pattern);
      await fill(prefix + '-normal');
      const normal = page.waitForResponse(
        (reply) =>
          reply.request().method() === 'POST' &&
          new URL(reply.url()).pathname === '/api/v1' + endpoint
      );
      await save();
      const normalResponse = await normal;
      assert.equal(normalResponse.status(), 200);
      const created = (await normalResponse.json()).data;
      cleanup(() => api('DELETE', endpoint + '/' + created.id));
      await dialog.waitFor({ state: 'hidden' });
      console.log(
        `PASS ${kind}: old ${outcome} keeps the new draft, normal save still closes`
      );
    }
  }

  const budget = await api('GET', '/budgets');
  cleanup(() =>
    api('PUT', '/budgets/' + budget.period_key, {
      total: budget.total.budget,
      items: budget.categories
        .filter((category) => category.budget)
        .map((category) => ({
          category_id: category.category_id,
          amount: category.budget,
        })),
    })
  );
  await api('PUT', '/budgets/' + budget.period_key, {
    total: '5000',
    items: [],
  });
  for (const resource of ['compare', 'budgets']) {
    const pattern =
      resource === 'compare'
        ? '**/api/v1/stats/compare*'
        : '**/api/v1/budgets*';
    const message = 'repair-sidebar-' + resource;
    const sidebar = page.locator('.sider-brief');
    const fail = (route) =>
      route.fulfill({ status: 500, json: { code: 20000, message } });
    await page.route(pattern, fail);
    await page.goto(base + '/records');
    await sidebar.getByText(message, { exact: true }).waitFor();
    if (resource === 'compare')
      assert(!(await sidebar.innerText()).includes('读取中…'));
    await page.unroute(pattern);
    await sidebar.getByRole('button', { name: /重\s*试/ }).click();
    await sidebar
      .getByText(message, { exact: true })
      .waitFor({ state: 'hidden' });
    await sidebar.locator('.brief-value .money').waitFor();
    const value = await sidebar.locator('.brief-value').innerText();
    const progress = await sidebar.locator('.brief-meta').last().innerText();
    await page.route(pattern, fail);
    await page.evaluate(() => {
      dispatchEvent(new Event('offline'));
      dispatchEvent(new Event('online'));
    });
    await sidebar.getByText(message, { exact: true }).waitFor();
    await sidebar.getByText('数据更新失败', { exact: true }).waitFor();
    assert.equal(await sidebar.locator('.brief-value').innerText(), value);
    if (resource === 'budgets')
      assert.equal(
        await sidebar.locator('.brief-meta').last().innerText(),
        progress
      );
    await page.unroute(pattern);
    await sidebar.getByRole('button', { name: /重\s*试/ }).click();
    await sidebar
      .getByText(message, { exact: true })
      .waitFor({ state: 'hidden' });
    console.log(
      `PASS sidebar ${resource}: initial error, retry and readable stale data`
    );
  }
  await context.close();
});
