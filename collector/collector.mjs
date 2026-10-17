import { readFileSync } from 'node:fs';

// Generated from backend/internal/wealthdata/products.json by go generate.
const manifest = JSON.parse(readFileSync(new URL('./products.json', import.meta.url), 'utf8'));
if (manifest.schema_version !== 1 || !Array.isArray(manifest.products)) throw new Error('invalid product manifest');
const products = {};
for (const product of manifest.products.filter((p) => p.adapter === 'suyin')) {
  const page = new URL(product.page_url);
  if (page.origin !== 'https://www.suyinwealth.com' || page.pathname !== `/lccs/cpxq/${product.code}` || page.search || page.hash || product.currency !== 'CNY' || product.valuation_basis !== 'cny_wealth_unit_nav' || Object.hasOwn(products, product.code)) throw new Error('invalid collector product identity');
  products[product.code] = Object.freeze({ ...product, source_path: '/web/fundNav/getNav' });
}
export const PRODUCTS = Object.freeze(products);

export class CollectionError extends Error {
  constructor(code, httpStatus) { super(code); this.code = code; this.httpStatus = httpStatus; }
}

const browserOwners = new WeakMap();

export async function closeWithin(close, force, timeoutMs = 2000) {
  let timer;
  try {
    const graceful = Promise.resolve().then(close).then(() => true, () => false);
    const done = await Promise.race([graceful, new Promise((resolve) => { timer = setTimeout(() => resolve(false), timeoutMs); })]);
    if (!done) await force();
  } finally { clearTimeout(timer); }
}

export async function disposeBrowser(browser) {
  const owner = browserOwners.get(browser);
  if (owner) {
    browserOwners.delete(browser);
    await owner.kill();
  } else {
    await browser.close();
  }
}

export function productFor(code) {
  if (!Object.hasOwn(PRODUCTS, code)) throw new CollectionError('unknown_product');
  return PRODUCTS[code];
}

export function isNAVResponse(response, product) {
  const url = new URL(response.url());
  const request = response.request();
  const fields = new URLSearchParams(request.postData() ?? '');
  return url.origin === 'https://www.suyinwealth.com' && url.pathname === product.source_path &&
    request.method() === 'POST' && fields.get('fundCode') === product.code && fields.get('dateType') === 'all';
}

// Every attempt starts with a fresh anonymous context. No desktop profile,
// storage state or personal bank credentials are ever accepted.
export async function collect(browser, code, { timeoutMs = 25000, now = () => new Date() } = {}) {
  const product = productFor(code);
  let context;
  let timer;
  let rejectNavigation;
  const navigationFailure = new Promise((_, reject) => { rejectNavigation = reject; });
  try {
    return await Promise.race([
      (async () => {
        const session = await browser.newBrowserCDPSession();
        let userAgent;
        try {
          const version = await session.send('Browser.getVersion');
          userAgent = version.userAgent.replace('HeadlessChrome/', 'Chrome/');
        } finally { await session.detach(); }
        context = await browser.newContext({ locale: 'zh-CN', timezoneId: 'Asia/Shanghai', userAgent });
        const page = await context.newPage();
        page.setDefaultTimeout(timeoutMs);
        page.on('response', (response) => {
          // 412 can be the normal initial anonymous challenge. A subsequent
          // 400/403/429 navigation means that anonymous navigation was rejected.
          if (response.request().isNavigationRequest() && response.request().frame() === page.mainFrame() && [400, 403, 429].includes(response.status())) {
            rejectNavigation(new CollectionError('source_navigation_rejected', response.status()));
          }
        });
        await page.goto(product.page_url, { waitUntil: 'domcontentloaded', timeout: timeoutMs });
        await page.getByText(product.name, { exact: false }).first().waitFor({ state: 'visible' });
        await page.getByText(product.registration_code, { exact: false }).first().waitFor({ state: 'visible' });
        // The two visible text locators can settle during different product
        // render frames. Verify both in the same rendered-body snapshot before
        // interpreting a transient frame as a different product.
        await page.waitForFunction(({ name, registration }) => {
          const text = document.body?.innerText.replace(/\s/g, '') ?? '';
          return text.includes(name) && text.includes(registration);
        }, { name: product.name, registration: product.registration_code }, { timeout: timeoutMs });
        const body = (await page.locator('body').innerText()).replace(/\s/g, '');
        if (!body.includes(product.name) || !body.includes(product.registration_code)) {
          throw new CollectionError('product_identity_mismatch');
        }
        const pending = page.waitForResponse((r) => isNAVResponse(r, product), { timeout: timeoutMs });
        pending.catch(() => {}); // Closing a timed-out context also rejects this waiter.
        await page.locator('#allbtn').click();
        const response = await pending;
        if (response.status() !== 200 || !response.headers()['content-type']?.includes('application/json')) {
          throw new CollectionError('source_unavailable');
        }
        const raw = await response.text();
        if (Buffer.byteLength(raw) > 4 * 1024 * 1024) throw new CollectionError('source_too_large');
        let result;
        try { result = JSON.parse(raw); } catch { throw new CollectionError('invalid_json'); }
        if (result.code !== '0000' || !Array.isArray(result.data) || !result.data.length) {
          throw new CollectionError('invalid_business_response');
        }
        return {
          code: product.code, name: product.name, registration_code: product.registration_code,
          currency: product.currency, request_fund_code: product.code,
          source_code: product.source_code, source_url: product.page_url,
          fetched_at: now().toISOString(), response: result,
        };
      })(),
      navigationFailure,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new CollectionError('collection_timeout')), timeoutMs);
      }),
    ]);
  } finally {
    clearTimeout(timer);
    if (context) await closeWithin(() => context.close(), () => disposeBrowser(browser));
    else await disposeBrowser(browser);
  }
}

export async function launch() {
  const { chromium } = await import('playwright-core');
  const owner = await chromium.launchServer({
    executablePath: process.env.WEALTH_BROWSER_EXE || undefined,
    channel: process.env.WEALTH_BROWSER_EXE ? undefined : 'chromium',
    headless: true,
    chromiumSandbox: process.platform === 'linux',
    args: ['--disable-blink-features=AutomationControlled'],
    host: '127.0.0.1',
  });
  try {
    const browser = await chromium.connect(owner.wsEndpoint(), { timeout: 10000 });
    browserOwners.set(browser, owner);
    return browser;
  } catch (error) { await owner.kill(); throw error; }
}
