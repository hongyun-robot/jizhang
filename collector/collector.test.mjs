import assert from 'node:assert/strict';
import test from 'node:test';
import { collect, launch, disposeBrowser, productFor } from './collector.mjs';

for (const registrationDelay of [0, 80]) for (const bodyDelay of [0, 100]) {
test(`history range, registration ${registrationDelay}ms and rendered body ${bodyDelay}ms`, { skip: !process.env.WEALTH_BROWSER_EXE }, async () => {
  const browser = await launch();
  const product = productFor('J07241');
  let historyRequests = 0;
  const fixtureBrowser = {
    newBrowserCDPSession: () => browser.newBrowserCDPSession(),
    close: () => disposeBrowser(browser),
    async newContext(options) {
      const context = await browser.newContext(options);
      await context.route('https://www.suyinwealth.com/**', async (route) => {
        const url = new URL(route.request().url());
        if (url.pathname === '/lccs/cpxq/J07241') {
          return route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<!doctype html><html><body>
            <h1>${product.name}</h1><p id="registration">${registrationDelay ? '' : product.registration_code}</p>
            <script>setTimeout(()=>document.querySelector('#registration').textContent='${product.registration_code}',${registrationDelay})</script>
            <script>const until=Date.now()+${bodyDelay};Object.defineProperty(document.body,'innerText',{get(){return Date.now()<until?'Loading':document.body.textContent}});</script>
            <button id="allbtn" onclick="fetch('/web/fundNav/getNav',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'fundCode=J07241&dateType=all&rows=9999&page=1'})">成立以来</button>
            <div>成立以来</div></body></html>` });
        }
        if (url.pathname === '/web/fundNav/getNav') {
          const fields = new URLSearchParams(route.request().postData());
          assert.equal(fields.get('fundCode'), product.code);
          assert.equal(fields.get('dateType'), 'all');
          historyRequests++;
          return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ code: '0000', data: [{ cfm_date: '2026-10-15', nav: '1.02142', tot_nav: '1.02142', prd_code: product.source_code }] }) });
        }
        return route.abort();
      });
      return context;
    },
  };
  try {
    const result = await collect(fixtureBrowser, product.code, { timeoutMs: 5000 });
    assert.equal(historyRequests, 1);
    assert.equal(result.response.data[0].nav, '1.02142');
    assert.equal(result.code, product.code);
  } finally { await disposeBrowser(browser); }
});
}
