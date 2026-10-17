import assert from 'node:assert/strict';
import test from 'node:test';
import { once } from 'node:events';
import { createServer } from './server.mjs';
import { productFor, isNAVResponse, closeWithin } from './collector.mjs';

test('a stuck browser cleanup has a hard recovery boundary', async () => {
  let forced = 0;
  await closeWithin(() => new Promise(() => {}), async () => { forced++; }, 10);
  assert.equal(forced, 1);
  await closeWithin(async () => {}, async () => { forced++; }, 10);
  assert.equal(forced, 1);
});

test('only catalog products and exact official request identity are accepted', () => {
  assert.throws(() => productFor('__proto__'));
  assert.throws(() => productFor('https://example.com'));
  const response = (url, fields) => ({ url: () => url, request: () => ({ method: () => 'POST', postData: () => fields }) });
  const path = 'https://www.suyinwealth.com/web/fundNav/getNav?dynamic=not-logged';
  assert(isNAVResponse(response(path, 'fundCode=J07241&dateType=all'), productFor('J07241')));
  assert(!isNAVResponse(response(path, 'fundCode=J07242&dateType=all'), productFor('J07241')));
  assert(!isNAVResponse(response('https://example.com/web/fundNav/getNav', 'fundCode=J07241&dateType=all'), productFor('J07241')));
});

test('service authenticates, disallows arbitrary input, coalesces and cools requests', async () => {
  let calls = 0;
  let finish;
  let at = 1000;
  const key = 'test-only-collector-key-123456';
  const server = createServer({ key, now: () => at, run: async () => { calls++; return new Promise((resolve) => { finish = resolve; }); } });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const base = 'http://127.0.0.1:' + server.address().port;
  const headers = { authorization: 'Bearer ' + key };
  const path = base + '/v1/products/J07241/nav';
  try {
    assert.equal((await fetch(base + '/health')).status, 200);
    assert.equal((await fetch(path, { method: 'POST' })).status, 401);
    assert.equal((await fetch(base + '/v1/products/J07242/nav', { method: 'POST', headers })).status, 400);
    assert.equal((await fetch(path + '?url=other', { method: 'POST', headers })).status, 404);
    assert.equal((await fetch(path, { method: 'POST', headers, body: 'private' })).status, 400);
    const first = fetch(path, { method: 'POST', headers });
    const second = fetch(path, { method: 'POST', headers });
    while (!finish) await new Promise((resolve) => setImmediate(resolve));
    // Keep the first request in flight until the second has reached the server.
    await new Promise((resolve) => setTimeout(resolve, 30));
    finish({ code: 'J07241' });
    assert.equal((await first).status, 200); assert.equal((await second).status, 200);
    assert.equal(calls, 1);
    assert.equal((await fetch(path, { method: 'POST', headers })).status, 429);
    at += 60000;
    const third = fetch(path, { method: 'POST', headers });
    while (calls !== 2) await new Promise((resolve) => setImmediate(resolve));
    finish({ code: 'J07241' }); assert.equal((await third).status, 200);
  } finally { await server.stop(); }
});

test('configuration fails closed and source errors expose no session information', async () => {
  assert.throws(() => createServer({ key: '', run: async () => ({}) }));
  const key = 'test-only-collector-key-123456';
  const server = createServer({ key, run: async () => { throw new Error('session-private-details'); } });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    const response = await fetch('http://127.0.0.1:' + server.address().port + '/v1/products/J07241/nav', { method: 'POST', headers: { authorization: 'Bearer ' + key } });
    assert.equal(response.status, 502); assert.deepEqual(await response.json(), { error: 'collection_failed' });
  } finally { await server.stop(); }
});
