// Read-only live diagnostic. Prints cookie/parameter metadata, never their values.
import { chromium } from 'playwright-core';
import { createHash } from 'node:crypto';
import { productFor, closeWithin, isNAVResponse } from './collector.mjs';

if (process.env.WEALTH_LIVE_SMOKE !== '1') throw new Error('Explicit public read-only diagnostic required');
const product = productFor('J07241');
const mode = process.argv[2] ?? 'baseline';
if (!['baseline', 'automation-flag', 'ua-only', 'normal-ua'].includes(mode)) throw new Error('Unknown diagnostic mode');
const owner = await chromium.launchServer({
  executablePath: process.env.WEALTH_BROWSER_EXE || undefined,
  channel: process.env.WEALTH_BROWSER_EXE ? undefined : 'chromium',
  headless: true, chromiumSandbox: process.platform === 'linux', host: '127.0.0.1',
  ...(['automation-flag', 'normal-ua'].includes(mode) ? { args: ['--disable-blink-features=AutomationControlled'] } : {}),
});
const browser = await chromium.connect(owner.wsEndpoint(), { timeout: 10000 });
const versionSession=await browser.newBrowserCDPSession();
const versionInfo=await versionSession.send('Browser.getVersion');
await versionSession.detach();
const context = await browser.newContext({
  locale: 'zh-CN', timezoneId: 'Asia/Shanghai',
  ...(['ua-only', 'normal-ua'].includes(mode) ? { userAgent: versionInfo.userAgent.replace('HeadlessChrome/','Chrome/') } : {}),
});
const page = await context.newPage();
const cdp = await context.newCDPSession(page);
const requests = new Map();
const extra = [];
const responseWork = [];
let settle;
const settled = new Promise((resolve) => { settle = resolve; });
const metadataURL = (value) => {
  const url = new URL(value);
  return { origin: url.origin, path: url.pathname, query: [...url.searchParams].map(([name, v]) => ({ name, length: v.length })) };
};
const cookieMetadata = (text) => String(text ?? '').split(/;\s*/).filter(Boolean).map((s) => {
  const at = s.indexOf('=');
  return { name: s.slice(0, at), length: s.slice(at + 1).length };
});
const report = { mode, at: new Date().toISOString(), browser: browser.version(), requests: [], responses: [], cookies: [], reached_product: false };
await cdp.send('Network.enable');
cdp.on('Network.requestWillBeSent', (e) => {
  const u = new URL(e.request.url);
  if (u.origin !== 'https://www.suyinwealth.com') return;
  const fields = e.request.postData ? [...new URLSearchParams(e.request.postData)].map(([name, value]) => ({ name, length: value.length })) : [];
  const row = { id: e.requestId, type: e.type, method: e.request.method, url: metadataURL(e.request.url), body_fields: fields };
  requests.set(e.requestId, row); report.requests.push(row);
});
cdp.on('Network.requestWillBeSentExtraInfo', (e) => {
  extra.push({ id: e.requestId, cookie_header: cookieMetadata(e.headers.Cookie ?? e.headers.cookie), associated_cookies: e.associatedCookies?.map((c) => ({ name: c.cookie.name, length: c.cookie.value.length, blocked: c.blockedReasons })) });
});
page.on('response', (r) => {
  const u = new URL(r.url());
  if (u.origin !== 'https://www.suyinwealth.com') return;
  responseWork.push((async () => {
    const headers = await r.allHeaders();
    const setCookies = await r.headerValues('set-cookie');
    const row = { status: r.status(), url: metadataURL(r.url()), type: r.request().resourceType(), server: headers.server ?? null, content_type: headers['content-type'] ?? null, cookie_names: setCookies.map((v) => v.split('=', 1)[0]) };
    if (r.request().resourceType() === 'document' || r.request().resourceType() === 'script') {
      const body = await r.body().catch(() => Buffer.alloc(0));
      const text = body.toString('utf8');
      row.body_bytes = body.length;
      row.body_sha256 = createHash('sha256').update(body).digest('hex');
      row.challenge_markers = { ts_nsd: /\$_ts\.nsd\s*=/.test(text), ts_cd: /\$_ts\.cd\s*=/.test(text), meta_names: [...text.matchAll(/<meta[^>]*name=["']([^"']+)/gi)].map((m) => m[1]), script_paths: [...text.matchAll(/<script[^>]*src=["']([^"']+)/gi)].map((m) => { try { return new URL(m[1], r.url()).pathname; } catch { return 'invalid'; } }) };
    }
    report.responses.push(row);
  })());
  if (r.request().isNavigationRequest() && r.request().frame() === page.mainFrame() && [400, 403, 429].includes(r.status())) settle();
});
let deadline;
try {
  report.environment = await page.evaluate(() => ({ webdriver: navigator.webdriver, userAgent: navigator.userAgent, platform: navigator.platform, language: navigator.language, hardwareConcurrency: navigator.hardwareConcurrency, plugins: navigator.plugins.length, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, screen: {width: screen.width,height: screen.height}, outer: {width:outerWidth,height:outerHeight} }));
  await page.goto(product.page_url, { waitUntil: 'domcontentloaded', timeout: 12000 });
  await Promise.race([
    settled,
    page.getByText(product.name, { exact: false }).first().waitFor({ state: 'visible', timeout: 8000 }).then(() => { report.reached_product = true; }).catch(() => {}),
    new Promise((resolve) => { deadline = setTimeout(resolve, 9000); }),
  ]);
  if (report.reached_product) {
    const response = page.waitForResponse((r) => isNAVResponse(r, product), { timeout: 8000 });
    response.catch(() => {});
    await page.locator('#allbtn').click();
    const r = await response;
    const data = await r.json();
    const latest = Array.isArray(data.data) ? [...data.data].sort((a,b)=>String(b.cfm_date).localeCompare(String(a.cfm_date)))[0] : null;
    report.nav_result = { status: r.status(), code: data.code, count: Array.isArray(data.data) ? data.data.length : 0, latest: latest ? { date: latest.cfm_date, unit_nav: latest.nav } : null };
    if (process.argv.includes('--http-replay')) {
      const originalHeaders = await r.request().allHeaders();
      const headers = Object.fromEntries(['accept','content-type','user-agent','referer','origin','x-requested-with','accept-language'].filter(k=>originalHeaders[k]).map(k=>[k,originalHeaders[k]]));
      const payload = r.request().postData();
      const results = [];
      for (const [label, url] of [['generated_signature', r.url()], ['without_signature', 'https://www.suyinwealth.com'+product.source_path]]) {
        try {
          const response = await context.request.post(url,{headers,data:payload,timeout:10000,maxRedirects:0});
          const type=response.headers()['content-type']??'';
          const json=type.includes('application/json')?await response.json():null;
          results.push({label,status:response.status(),content_type:type,business_code:json?.code??null,count:json?.data?.length??0});
        } catch (error) { results.push({label,error:error.name}); }
      }
      report.http_replay=results;
      const cookies=await context.cookies(product.page_url);
      report.direct_http=[];
      for (const [label, selected] of [['all_cookies',cookies],['challenge_cookies',cookies.filter(c=>c.name.startsWith('FSSBB'))],['no_cookies',[]]]) {
        try {
          const cookieHeader=selected.map(c=>c.name+'='+c.value).join('; ');
          const response=await fetch('https://www.suyinwealth.com'+product.source_path,{method:'POST',headers:{...headers,...(cookieHeader?{cookie:cookieHeader}:{})},body:payload,redirect:'manual',signal:AbortSignal.timeout(10000)});
          const type=response.headers.get('content-type')??'';
          const json=type.includes('application/json')?await response.json():null;
          report.direct_http.push({label,cookie_names:selected.map(c=>c.name),status:response.status,content_type:type,business_code:json?.code??null,count:json?.data?.length??0});
        } catch(error) {report.direct_http.push({label,error:error.name});}
      }
    }
  }
  report.cookies = (await context.cookies(product.page_url)).map((c) => ({ name: c.name, length: c.value.length, domain: c.domain, path: c.path, secure: c.secure, httpOnly: c.httpOnly }));
  await Promise.allSettled(responseWork);
  report.requests = report.requests.filter((r)=>r.type==='Document'||r.url.path.includes('/4QbVtADbnLVIc/')||r.url.path===product.source_path);
  report.responses = report.responses.filter((r)=>r.type==='document'||r.url.path.includes('/4QbVtADbnLVIc/')||r.url.path===product.source_path);
  const relevantIds=new Set(report.requests.map(r=>r.id));
  report.request_extra = extra.filter((e) => relevantIds.has(e.id));
  console.log(JSON.stringify(report, null, 2));
} finally {
  clearTimeout(deadline);
  await closeWithin(() => context.close(), () => owner.kill());
  await owner.kill();
}
