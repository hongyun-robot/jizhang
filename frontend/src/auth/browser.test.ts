import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createBrowserAuth } from './browser';
import { createAuthQueue } from './wire';

const tokens = (sid: string, token: string) => ({
  access_token: token, session_id: sid, device_type: 'desktop' as const,
  expires_in: 1800, user: { id: 1, username: 'member', role: 'member' as const, status: 'active' as const },
});
class Channel {
  static all = new Set<Channel>();
  onmessage?: (event: { data: unknown }) => void;
  constructor() { Channel.all.add(this); }
  postMessage(data: unknown) {
    for (const other of Channel.all) if (other !== this) queueMicrotask(() => other.onmessage?.({ data }));
  }
}
const pause = () => new Promise((resolve) => setTimeout(resolve, 5));
beforeEach(() => {
  const values = new Map<string, string>();
  const queue = createAuthQueue();
  vi.stubGlobal('window', new EventTarget());
  vi.stubGlobal('navigator', { locks: { request: (_name: string, run: () => Promise<unknown>) => queue(run) }, maxTouchPoints: 0 });
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) });
  vi.stubGlobal('BroadcastChannel', Channel);
  Channel.all.clear();
});
afterEach(() => vi.unstubAllGlobals());

describe('browser authentication coordination', () => {
  it('does not reuse an access token whose delayed response is already expired', async () => {
    let refreshes=0;
    vi.stubGlobal('fetch',vi.fn(async (url: string)=>{
      if (url.endsWith('/refresh')) refreshes++;
      return new Response(JSON.stringify({code:0,data:{...tokens('session',url.endsWith('/login')?'expired':'fresh'),expires_at:Date.now()/1000+(url.endsWith('/login')?-1:1800)}}));
    }));
    const a=createBrowserAuth(),b=createBrowserAuth();
    await a.login({username:'member',password:'secret'});
    const result=await b.refresh({token:null,user:null,epoch:0,sessionID:'session'});
    expect(result.access_token).toBe('fresh');
    expect(refreshes).toBe(1);
  });
  it('shares one refresh across tabs and keeps tokens out of persistent storage', async () => {
    let refreshes = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url.endsWith('/refresh')) refreshes++;
      return new Response(JSON.stringify({ code: 0, data: tokens('session', url.endsWith('/login') ? 'old' : 'new') }));
    }));
    const a = createBrowserAuth(), b = createBrowserAuth();
    await a.login({ username: 'member', password: 'secret' });
    await pause();
    const state = { token: 'old', user: null, epoch: 0, sessionID: 'session' };
    const values = await Promise.all([a.refresh(state, 'old'), b.refresh(state, 'old')]);
    expect(values.map((value) => value.access_token)).toEqual(['new', 'new']);
    expect(refreshes).toBe(1);
    const persisted = localStorage.getItem('jz-auth:v2:')!;
    expect(persisted).not.toContain('access_token');
    expect(persisted).not.toContain('secret');
  });
  it('settles old refresh and logout responses before a new login', async () => {
    const order: string[] = [];
    let release: (() => void) | undefined;
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      const operation = url.split('/').at(-1)!;
      order.push(operation);
      if (operation === 'refresh' || operation === 'logout') await new Promise<void>((resolve) => { release = resolve; });
      return new Response(JSON.stringify({ code: 0, data: operation === 'logout' ? undefined : tokens('session', 'token-' + order.length) }));
    }));
    const a = createBrowserAuth(), b = createBrowserAuth();
    await a.login({ username: 'member', password: 'secret' });
    const snapshot = { token: 'old', user: null, epoch: 0, sessionID: 'session' };
    const refreshing = a.refresh(snapshot, 'token-1');
    await vi.waitFor(() => expect(order.at(-1)).toBe('refresh'));
    const login = b.login({ username: 'member', password: 'secret' });
    await pause();
    expect(order).toEqual(['login', 'refresh']);
    release!(); await refreshing; await login;
    a.beginLogout(snapshot);
    const closing = a.logout(snapshot);
    await vi.waitFor(() => expect(order.at(-1)).toBe('logout'));
    const relogin = b.login({ username: 'member', password: 'secret' });
    await pause();
    expect(order.at(-1)).toBe('logout');
    release!(); await closing; await relogin;
    expect(b.marker()?.status).toBe('active');
    expect(order).toEqual(['login', 'refresh', 'login', 'logout', 'login']);
  });
  it('does not let a stale tab mark a new login as failed or ended', async () => {
    let number = 0;
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 0, data: tokens('session-' + (++number), 'token') }))));
    const a = createBrowserAuth();
    await a.login({ username: 'member', password: 'secret' });
    const old = { token: 'old', user: null, epoch: 0, sessionID: 'session-1' };
    await a.login({ username: 'member', password: 'secret' });
    a.end(old.sessionID, 'old failure');
    await expect(a.logout(old)).rejects.toThrow();
    expect(a.marker()?.sid).toBe('session-2');
    expect(a.marker()?.status).toBe('active');
  });
  it('blocks cookie mutations when neither coordination mechanism exists', async () => {
    vi.stubGlobal('navigator', {});
    vi.stubGlobal('SharedWorker', undefined);
    const fetcher = vi.fn(); vi.stubGlobal('fetch', fetcher);
    await expect(createBrowserAuth().login({ username: 'member', password: 'secret' })).rejects.toThrow('浏览器');
    expect(fetcher).not.toHaveBeenCalled();
  });
  it('blocks further writes when the shared proxy cannot initialize', async () => {
    vi.stubGlobal('navigator', {});
    vi.stubGlobal('SharedWorker', class { constructor() { throw new Error('blocked worker'); } });
    const fetcher = vi.fn(); vi.stubGlobal('fetch', fetcher);
    const auth = createBrowserAuth();
    await expect(auth.login({ username: 'member', password: 'secret' })).rejects.toThrow('浏览器');
    await expect(auth.login({ username: 'member', password: 'secret' })).rejects.toThrow('浏览器');
    expect(fetcher).not.toHaveBeenCalled();
  });
  it('can render recovery information when shared storage is inaccessible', async () => {
    vi.stubGlobal('localStorage', { getItem() { throw new Error('denied'); } });
    const fetcher = vi.fn(); vi.stubGlobal('fetch', fetcher);
    const auth = createBrowserAuth();
    expect(auth.marker()).toBeNull();
    await expect(auth.login({ username: 'member', password: 'secret' })).rejects.toThrow('浏览器');
    expect(fetcher).not.toHaveBeenCalled();
  });
  it('retains a pending logout after a failed request and blocks cookie restoration', async () => {
    const fetcher = vi.fn(async (url: string) => {
      if (url.endsWith('/logout')) return new Response(JSON.stringify({ code: 20001, message: 'offline' }), { status: 503 });
      return new Response(JSON.stringify({ code: 0, data: tokens('session', 'token') }));
    });
    vi.stubGlobal('fetch', fetcher);
    const a = createBrowserAuth();
    await a.login({ username: 'member', password: 'secret' });
    const snapshot = { token: 'token', user: null, epoch: 0, sessionID: 'session' };
    a.beginLogout(snapshot);
    await expect(a.logout(snapshot)).rejects.toThrow('offline');
    const reopened = createBrowserAuth();
    expect(reopened.marker()?.status).toBe('logout_failed');
    await expect(reopened.refresh({ token: null, user: null, epoch: 0 })).rejects.toThrow('登录已结束');
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
