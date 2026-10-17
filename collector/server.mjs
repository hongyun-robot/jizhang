import http from 'node:http';
import { createHash, timingSafeEqual } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { collect, disposeBrowser, launch, productFor, CollectionError } from './collector.mjs';

const digest = (text) => createHash('sha256').update(text).digest();

export function createServer({ key, run, now = Date.now, cooldownMs = 60000 }) {
  if (!key || key.trim().length < 24) throw new Error('WEALTH_COLLECTOR_KEY must contain at least 24 characters');
  const secretHash = digest('Bearer ' + key);
  const inFlight = new Map();
  const attempts = new Map();
  let closing = false;
  const server = http.createServer(async (req, res) => {
    const send = (status, data) => {
      if (res.destroyed) return;
      res.writeHead(status, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' });
      res.end(JSON.stringify(data));
    };
    if (req.method === 'GET' && req.url === '/health') return send(closing ? 503 : 200, { status: closing ? 'stopping' : 'ready' });
    if (!timingSafeEqual(digest(req.headers.authorization ?? ''), secretHash)) return send(401, { error: 'unauthorized' });
    if (closing) return send(503, { error: 'stopping' });
    const match = /^\/v1\/products\/([A-Z0-9]+)\/nav$/.exec(req.url ?? '');
    if (req.method !== 'POST' || !match) return send(404, { error: 'not_found' });
    const code = match[1];
    try { productFor(code); } catch { return send(400, { error: 'unknown_product' }); }
    // No request body, URL, JavaScript or browser launch settings are accepted.
    if (req.headers['transfer-encoding'] || Number(req.headers['content-length'] ?? 0) !== 0) {
      req.resume(); return send(400, { error: 'unexpected_body' });
    }
    let pending = inFlight.get(code);
    if (!pending) {
      const started = now();
      const previous = attempts.get(code);
      if (previous !== undefined && started - previous < cooldownMs) {
        return send(429, { error: 'cooldown', retry_after_seconds: Math.ceil((cooldownMs - started + previous) / 1000) });
      }
      if (inFlight.size) return send(429, { error: 'busy' });
      attempts.set(code, started);
      pending = Promise.resolve().then(() => run(code));
      inFlight.set(code, pending);
      pending.finally(() => inFlight.delete(code)).catch(() => {});
    }
    try { send(200, await pending); }
    catch { send(502, { error: 'collection_failed' }); }
  });
  server.requestTimeout = 35000;
  server.headersTimeout = 10000;
  server.keepAliveTimeout = 3000;
  server.stop = async () => {
    closing = true;
    await new Promise((resolve) => { server.close(resolve); server.closeIdleConnections(); });
  };
  return server;
}

async function main() {
  // Validate configuration before launching a browser.
  const key = process.env.WEALTH_COLLECTOR_KEY;
  if (!key || key.trim().length < 24) throw new Error('WEALTH_COLLECTOR_KEY must contain at least 24 characters');
  let browser = await launch();
  const run = async (code) => {
    try { return await collect(browser, code); }
    catch (error) {
      process.stderr.write(JSON.stringify({event:'wealth_collection_retry',code:error instanceof CollectionError?error.code:'browser_error',http_status:Number.isInteger(error.httpStatus)?error.httpStatus:null})+'\n');
      await disposeBrowser(browser).catch(() => {});
      browser = await launch();
      try { return await collect(browser, code); } // At most one fresh anonymous recovery.
      catch (retryError) {
        process.stderr.write(JSON.stringify({event:'wealth_collection_failed',code:retryError instanceof CollectionError?retryError.code:'browser_error',http_status:Number.isInteger(retryError.httpStatus)?retryError.httpStatus:null})+'\n');
        throw retryError;
      }
    }
  };
  const server = createServer({ key, run });
  server.listen(Number(process.env.PORT || 8091), process.env.HOST || '127.0.0.1');
  let stopping = false;
  const stop = async () => {
    if (stopping) return;
    stopping = true;
    const deadline = setTimeout(() => process.exit(1), 35000).unref();
    await server.stop(); await disposeBrowser(browser); clearTimeout(deadline);
  };
  process.on('SIGTERM', () => { void stop(); });
  process.on('SIGINT', () => { void stop(); });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(() => { process.stderr.write('wealth collector startup failed\n'); process.exitCode = 1; });
}
