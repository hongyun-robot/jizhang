import { CanceledError } from 'axios';
import { API_BASE, ROOT } from '../base';
import { ApiError } from '../api/errors';
import { authFetch, type AuthCommand } from './wire';
import type { SessionSnapshot, TokenResult } from './coordinator';

type Mode = 'locks' | 'worker' | 'none';
export interface AuthMarker {
  id: string;
  sid: string | null;
  status: 'active' | 'ended' | 'logout_pending' | 'logout_failed';
  mode: Mode;
  reason?: string;
}
interface AuthEvent { marker: AuthMarker; result?: TokenResult; request?: boolean }
const stateKey = 'jz-auth:v2:' + ROOT;
const name = stateKey + ':network';
const unavailable = () => new ApiError('浏览器无法协调登录，请使用较新的浏览器或安全地址后重试', 'network');
const ended = () => new ApiError('登录已结束，请重新登录', 'unauthorized', 401, 10008);
const eventID = () => typeof crypto.randomUUID === 'function' ? crypto.randomUUID() :
  Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, '0')).join('');

export function createBrowserAuth() {
  let memory: AuthMarker | null = null;
  let cached: { result: TokenResult; expires: number } | null = null;
  let worker: SharedWorker | null = null;
  let workerFailed = false;
  const waiting = new Map<string, { resolve: (value: TokenResult | null) => void; reject: (error: Error) => void }>();
  const listeners = new Set<(event: AuthEvent) => void>();
  const browser = typeof window !== 'undefined';
  let channel: BroadcastChannel | null = null;
  if (browser && typeof BroadcastChannel !== 'undefined') {
    try { channel = new BroadcastChannel(name); } catch { /* Use storage events and the shared proxy. */ }
  }
  const mode = (): Mode => browser && navigator.locks && (channel || typeof SharedWorker === 'undefined') ? 'locks' : browser && typeof SharedWorker !== 'undefined' ? 'worker' : 'none';
  const marker = (): AuthMarker | null => {
    if (!browser) return memory;
    try {
      const raw = localStorage.getItem(stateKey);
      if (!raw) return null;
      const value = JSON.parse(raw) as AuthMarker;
      return typeof value.id === 'string' && ['active', 'ended', 'logout_pending', 'logout_failed'].includes(value.status) ? value : null;
    } catch { throw unavailable(); }
  };
  const send = (event: AuthEvent) => channel?.postMessage(event);
  const put = (value: AuthMarker, result?: TokenResult) => {
    memory = value;
    if (browser) {
      try { localStorage.setItem(stateKey, JSON.stringify(value)); } catch { throw unavailable(); }
    }
    if (result) cached = { result, expires: (result.expires_at ?? Date.now() / 1000 + (result.expires_in ?? 1800)) * 1000 };
    else if (value.status !== 'active') cached = null;
    send({ marker: value, result });
  };
  const currentCache = (sid: string | null, rejected?: string | null) =>
    cached && cached.expires > Date.now() && cached.result.session_id === sid && cached.result.access_token !== rejected ? cached.result : null;
  const receive = (event: AuthEvent) => {
    const current = marker();
    if (!current || event.marker.id !== current.id || event.marker.sid !== current.sid) return;
    if (event.request) {
      const value = currentCache(current.sid);
      if (value) send({ marker: current, result: value });
      return;
    }
    if (event.result) cached = { result: event.result, expires: (event.result.expires_at ?? Date.now() / 1000 + (event.result.expires_in ?? 1800)) * 1000 };
    else if (current.status !== 'active') cached = null;
    for (const listener of listeners) listener(event);
  };
  if (channel) channel.onmessage = (event: MessageEvent<AuthEvent>) => receive(event.data);
  if (browser) window.addEventListener('storage', (event) => {
    if (event.key !== stateKey || !event.newValue) return;
    const current = marker();
    if (current) receive({ marker: current });
  });
  const proxy = (command: AuthCommand): Promise<TokenResult | null> => {
    if (workerFailed) return Promise.reject(unavailable());
    try {
      if (!worker) {
        worker = new SharedWorker(new URL('./transport.worker.ts', import.meta.url), { name, type: 'module' });
        worker.port.start();
        worker.port.onmessage = (event) => {
          const value = waiting.get(event.data.id);
          if (!value) return;
          waiting.delete(event.data.id);
          if (event.data.error) {
            const error = event.data.error;
            value.reject(new ApiError(error.message, error.kind, error.status, error.code));
          } else value.resolve(event.data.result);
        };
        worker.onerror = () => {
          workerFailed = true;
          for (const value of waiting.values()) value.reject(unavailable());
          waiting.clear();
        };
      }
      const id = eventID();
      return new Promise((resolve, reject) => {
        waiting.set(id, { resolve, reject });
        worker!.port.postMessage({ id, command });
      });
    } catch { workerFailed = true; return Promise.reject(unavailable()); }
  };
  const transport = (command: AuthCommand) => mode() === 'worker' ? proxy(command) : authFetch(command);
  const serial = async <T,>(run: () => Promise<T>): Promise<T> => {
    const selected = mode();
    const current = marker();
    if (selected === 'none' || (current?.mode !== undefined && current.mode !== selected && current.status === 'active')) throw unavailable();
    if (selected === 'locks') return navigator.locks.request(name, run);
    // The SharedWorker serializes the actual credentialed fetch and its complete response.
    return run();
  };
  const waitForLogout = async () => {
    const deadline = Date.now() + 12000;
    while (marker()?.status === 'logout_pending') {
      if (Date.now() > deadline) throw new ApiError('退出仍在处理中，请稍后重试登录', 'network');
      await new Promise((resolve) => setTimeout(resolve, 30));
    }
  };
  const refresh = (snapshot: SessionSnapshot, rejected?: string | null) => serial(async () => {
    const start = marker();
    if (start && start.status !== 'active') throw ended();
    const sid = snapshot.sessionID ?? start?.sid ?? null;
    if (snapshot.sessionID && start?.sid && snapshot.sessionID !== start.sid) throw ended();
    let existing = currentCache(sid, rejected);
    if (!existing && start && channel) {
      send({ marker: start, request: true });
      await new Promise((resolve) => setTimeout(resolve, 30));
      existing = currentCache(sid, rejected);
    }
    if (existing) return existing;
    let result: TokenResult | null = null;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        result = await transport({ operation: 'refresh', base: API_BASE, body: { protocol_version: 2, session_id: sid ?? '' }, rejectedToken: rejected });
        break;
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== 10010 || attempt !== 0) throw error;
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
    }
    if (!result) throw unavailable();
    const latest = marker();
    if (latest?.id !== start?.id) throw new CanceledError('会话已结束');
    put(start ?? { id: eventID(), sid: result.session_id ?? null, status: 'active', mode: mode() }, result);
    return result;
  });
  return {
    marker: () => { try { return marker(); } catch { return memory; } },
    beforeLogin: waitForLogout,
    subscribe(listener: (event: AuthEvent) => void) { listeners.add(listener); return () => listeners.delete(listener); },
    refresh,
    async login(input: { username: string; password: string }) {
      await waitForLogout();
      return serial(async () => {
        if (marker()?.status === 'logout_pending') throw new CanceledError('正在退出所有设备');
        const start = marker();
        const result = await transport({ operation: 'login', base: API_BASE, body: {
          ...input, protocol_version: 2,
          max_touch_points: Math.min(64, Math.max(0, Math.trunc(navigator.maxTouchPoints || 0))),
        } });
        if (!result) throw unavailable();
        if (marker()?.id !== start?.id) throw new CanceledError('会话已结束');
        put({ id: eventID(), sid: result.session_id ?? null, status: 'active', mode: mode() }, result);
        return result;
      });
    },
    beginLogout(snapshot: SessionSnapshot) {
      const current = marker();
      const sid = snapshot.sessionID ?? current?.sid ?? null;
      if (current?.sid && sid !== current.sid) return;
      put({ id: eventID(), sid, status: 'logout_pending', mode: mode(), reason: '正在退出所有设备' });
    },
    async logout(snapshot: SessionSnapshot) {
      const start = marker();
      try {
        await serial(async () => {
          const current = marker();
          const sid = snapshot.sessionID ?? start?.sid ?? null;
          if (current?.id !== start?.id || current?.sid !== sid) throw ended();
          await transport({ operation: 'logout', base: API_BASE, token: snapshot.token, body: { protocol_version: 2, session_id: sid ?? '' } });
          if (marker()?.id === start?.id) put({ id: eventID(), sid: null, status: 'ended', mode: mode(), reason: '已退出所有设备' });
        });
      } catch (error) {
        if (start && start.sid === (snapshot.sessionID ?? start.sid) && marker()?.id === start.id) put({ ...start, status: 'logout_failed', reason: '当前浏览器已退出，尚未确认其他设备退出，可重试' });
        throw error;
      }
    },
    end(sid: string | null | undefined, reason: string) {
      const current = marker();
      if (!current || current.status !== 'active' || (sid && current.sid !== sid)) return;
      put({ id: eventID(), sid: current.sid, status: 'ended', mode: current.mode, reason });
    },
  };
}
