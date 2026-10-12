import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';

const options = Object.fromEntries(process.argv.slice(2).map((argument) => argument.replace(/^--/, '').split('=')));
const root = path.resolve(options.dist ?? 'dist');
const prefix = (options.base ?? '').replace(/\/$/, '');
const api = options.api ?? 'http://127.0.0.1:18084';
assert(new URL(api).hostname === '127.0.0.1', 'preview proxy requires a local fixture');
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff': 'font/woff', '.woff2': 'font/woff2', '.ico': 'image/x-icon' };
http.createServer((request, response) => {
  const pathname = new URL(request.url, 'http://localhost').pathname;
  if (!pathname.startsWith(prefix + '/')) { response.writeHead(404); response.end(); return; }
  const relative = pathname.slice(prefix.length);
  if (relative.startsWith('/api/')) {
    const proxy = http.request(api + request.url.slice(prefix.length), { method: request.method, headers: request.headers }, (incoming) => {
      const headers = { ...incoming.headers };
      if (headers['set-cookie']) headers['set-cookie'] = headers['set-cookie'].map((cookie) => cookie.replace(/Path=\/api\//i, 'Path=' + prefix + '/api/'));
      response.writeHead(incoming.statusCode, headers);
      incoming.pipe(response);
    });
    proxy.on('error', () => { response.writeHead(502); response.end(); });
    request.pipe(proxy);
    return;
  }
  let file = path.resolve(root, '.' + decodeURIComponent(relative));
  if (file !== root && !file.startsWith(root + path.sep)) { response.writeHead(404); response.end(); return; }
  if (!fs.existsSync(file) || !fs.statSync(file).isFile()) file = path.join(root, 'index.html');
  response.setHeader('Content-Type', types[path.extname(file)] ?? 'application/octet-stream');
  fs.createReadStream(file).pipe(response);
}).listen(Number(options.port ?? 5603), '127.0.0.1', () => console.log('PREVIEW_READY ' + prefix));
