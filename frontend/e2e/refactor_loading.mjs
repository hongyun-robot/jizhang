import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';
import { withFixture, fixtureApi, login, shot, until } from './support/fixture.mjs';

const base = process.env.REFACTOR_E2E_BASE ?? 'http://127.0.0.1:5601';
const dist = path.resolve(process.env.REFACTOR_DIST ?? 'dist');
await withFixture(base, async (browser, cleanup) => {
  // All chart registrations need nonempty input; a fresh fixture has no current expense.
  const api = await fixtureApi(base);
  const asset = (await api('GET', '/accounts')).find((account) => account.type === 'asset' && account.status === 'active');
  const category = (await api('GET', '/categories')).flatMap((wrapper) => wrapper.children ?? []).flatMap((root) => root.children?.length ? root.children : [root]).find((entry) => entry.kind === 'expense');
  const date = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date());
  const expense = await api('POST', '/transactions', { kind: 'expense', amount: '0.01', account_id: asset.id, category_id: category.id, entry_date: date, note: 'loading-' + crypto.randomUUID() });
  cleanup(() => api('DELETE', `/transactions/${expense.id}`));
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light' });
  const page = await context.newPage();
  const scripts = new Set();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('request', (request) => { if (request.resourceType() === 'script') scripts.add(new URL(request.url()).pathname.split('/').at(-1)); });
  await login(page, base);
  await page.locator('.hero').waitFor();
  await page.waitForTimeout(300);
  assert(![...scripts].some((file) => file.startsWith('Stats-') || file.startsWith('UserManager-')), 'startup excludes chart and users chunks');
  const loaded = [...scripts].filter((file) => fs.existsSync(path.join(dist, 'assets', file)));
  const gzip = loaded.reduce((sum, file) => sum + gzipSync(fs.readFileSync(path.join(dist, 'assets', file))).length, 0);
  assert(gzip <= 859740 * 0.7, 'initial JS/modulepreload below 70% baseline');
  const allJS = fs.readdirSync(path.join(dist, 'assets')).filter((file) => file.endsWith('.js'));
  const metrics = { base, initialFiles: loaded, initialGzipBytes: gzip, baselineGzipBytes: 859740, ratio: gzip / 859740, allJSBytes: allJS.reduce((sum, file) => sum + fs.statSync(path.join(dist, 'assets', file)).size, 0), allJSGzipBytes: allJS.reduce((sum, file) => sum + gzipSync(fs.readFileSync(path.join(dist, 'assets', file))).length, 0) };
  fs.mkdirSync('e2e/shots/refactor-frontend', { recursive: true });
  fs.writeFileSync('e2e/shots/refactor-frontend/loading' + (new URL(base).pathname.includes('jizhang') ? '-subpath' : '') + '.json', JSON.stringify(metrics, null, 2));
  console.log('PASS initial loading:', gzip, 'gzip bytes;', (metrics.ratio * 100).toFixed(1) + '% baseline');

  await page.getByRole('menuitem', { name: /我的/ }).click();
  assert(![...scripts].some((file) => file.startsWith('UserManager-')));
  await page.getByRole('button', { name: /用户管理/ }).click();
  await page.locator('.ant-table-tbody tr').first().waitFor();
  assert([...scripts].some((file) => file.startsWith('UserManager-')));
  await page.route('**/assets/Stats-*.js', (route) => route.abort());
  await page.getByRole('menuitem', { name: /统计/ }).click();
  await page.getByText('页面加载失败', { exact: true }).waitFor();
  assert.equal(page.url(), base + '/stats');
  await page.unroute('**/assets/Stats-*.js');
  await page.getByRole('button', { name: '重新加载', exact: true }).click();
  await page.locator('.page h2').filter({ hasText: '统计' }).waitFor();
  await page.locator('canvas').first().waitFor();
  assert((await page.getByRole('img').count()) >= 4);
  assert((await page.getByText('查看数据表', { exact: true }).count()) >= 3);
  console.log('PASS lazy users, recoverable chart chunk failure, original URL and chart accessibility');

  for (const width of [360, 767, 768, 991, 992, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ['/', '/records', '/stats', '/liabilities']) {
      await page.goto(base + route, { waitUntil: 'domcontentloaded' });
      try { await page.locator('.page h2').first().waitFor(); }
      catch (error) { console.error('LOADING PAGE', page.url(), await page.locator('body').innerText()); await shot(page, 'loading-failure'); throw error; }
      await page.waitForTimeout(150);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${width}px ${route} horizontal overflow`);
      assert.equal(await page.locator('.bottom-tabs:visible').count(), width < 992 ? 1 : 0);
      assert.equal(await page.locator('.app-sider:visible').count(), width >= 992 ? 1 : 0);
      if (width < 992) {
        const box = await page.locator('.bottom-tabs button').first().boundingBox();
        assert(box && box.height >= 44, 'navigation touch target');
      }
      if ([360, 1280].includes(width)) await shot(page, `final-${width}-${route === '/' ? 'home' : route.slice(1)}`);
    }
  }
  await page.goto(base + '/mine');
  await page.getByText('暗色', { exact: true }).click();
  await until(() => page.getAttribute('html', 'data-theme'), (mode) => mode === 'dark', 'dark theme');
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--primary').trim()), '#2dd4bf');
  await page.reload();
  assert.equal(await page.getAttribute('html', 'data-theme'), 'dark');
  await page.goto(base + '/stats'); await page.locator('canvas').first().waitFor();
  await shot(page, 'final-dark-stats');
  await page.goto(base + '/mine'); await page.getByText('跟随系统', { exact: true }).click();
  await page.emulateMedia({ colorScheme: 'light' });
  await until(() => page.getAttribute('html', 'data-theme'), (mode) => mode === 'light', 'system light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await until(() => page.getAttribute('html', 'data-theme'), (mode) => mode === 'dark', 'system dark');
  await page.getByText('浅色', { exact: true }).click();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(base + '/records'); await page.locator('.page h2').waitFor();
  assert.equal(await page.locator('.page').evaluate((element) => getComputedStyle(element).animationName), 'none');
  assert(errors.every((message) => /import|module|fetch|load/i.test(message)), 'unexpected runtime errors: ' + errors.join('; '));
  await context.close();
  console.log('PASS six breakpoint matrix, theme persistence/system updates, reduced motion and refresh');
});
