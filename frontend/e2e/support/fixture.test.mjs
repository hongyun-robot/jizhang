import { test } from 'node:test';
import assert from 'node:assert/strict';
import { assertFixture, fixtureApi, registerFixturePage } from './fixture.mjs';
import { currentAuthToken, latestBearer, trackPageAuth } from './auth-tokens.mjs';

test('fixture guard rejects remote targets before accessing them', async () => {
  await assert.rejects(assertFixture('https://example.com'), /local isolated fixture/);
});

test('legacy API helpers select the current UI token for the same account', async () => {
  const base='http://127.0.0.1:19920';
  let listener;
  const page={on:(_event,run)=>{listener=run;}};
  trackPageAuth(page,base);
  const original='header.'+Buffer.from(JSON.stringify({uid:19})).toString('base64url')+'.signature';
  await listener({url:()=>base+'/api/v1/auth/login',status:()=>200,json:async()=>({data:{access_token:'current-ui-access',user:{id:19,username:'legacy-user'}}})});
  assert.equal(currentAuthToken(base,'legacy-user'),'current-ui-access');
  assert.equal(latestBearer(base,original),'current-ui-access');
  assert.equal(latestBearer(base,'invalid-token'),'invalid-token');
});

test('fixture requests reuse a live page and its token after deferred cleanup', async () => {
  const original = globalThis.fetch;
  const base = 'http://127.0.0.1:19919';
  let logins = 0;
  let current = '';
  globalThis.fetch = async (url, options = {}) => {
    if (url.endsWith('/__test__/info')) return Response.json({ fixture: 'voucher-browser' });
    if (url.endsWith('/auth/login')) { current = 'token-' + (++logins); return Response.json({ code: 0, data: { access_token: current, session_id: current } }); }
    assert.equal(options.headers.authorization, 'Bearer ' + current, 'cleanup must use the latest UI token');
    return Response.json({ code: 0, data: ['native'] });
  };
  try {
    const api = await fixtureApi(base);
    assert.deepEqual(await api('GET', '/accounts'), ['native']);
    let closed = false;
    let responseListener;
    const page = {
      isClosed: () => closed,
      on: (_event, listener) => { responseListener = listener; },
      request: { fetch: async () => ({ status: () => 200, json: async () => ({ code: 0, data: ['page'] }) }) },
    };
    registerFixturePage(page, base);
    current = 'ui-token';
    await responseListener({ url: () => base + '/api/v1/auth/login', status: () => 200, json: async () => ({ data: { access_token: current, session_id: 'ui', user: { username: 'voucher-admin' } } }) });
    assert.deepEqual(await api('GET', '/accounts'), ['page']);
    assert.equal(logins, 1, 'UI assertions must not create another desktop session');
    closed = true;
    assert.deepEqual(await api('DELETE', '/test-resource'), ['native']);
    assert.equal(logins, 1, 'cleanup must not reuse the replaced preparation token');
  } finally { globalThis.fetch = original; }
});
