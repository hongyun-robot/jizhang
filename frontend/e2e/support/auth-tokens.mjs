// Compatibility for older browser scripts that keep an API token before UI login.
// Tokens stay in this test process and are never written to storage or logs.
const byUser = new Map();
const byID = new Map();
const tracked = new WeakSet();
const key = (base, value) => base.replace(/\/$/, '') + ':' + value;
export const currentAuthToken = (base, username) => byUser.get(key(base, username));
export function latestBearer(base, fallback) {
  try {
    const claims = JSON.parse(Buffer.from(String(fallback).split('.')[1], 'base64url').toString('utf8'));
    return byID.get(key(base, claims.uid)) ?? fallback;
  } catch { return fallback; }
}
export function trackPageAuth(page, base) {
  if (tracked.has(page)) return;
  tracked.add(page);
  page.on('response', async (response) => {
    if (!/\/auth\/(login|refresh)$/.test(response.url()) || response.status() !== 200) return;
    const result = (await response.json().catch(() => null))?.data;
    if (!result?.access_token || !result.user) return;
    byUser.set(key(base, result.user.username), result.access_token);
    byID.set(key(base, result.user.id), result.access_token);
  });
}
