import { CanceledError } from 'axios';
import type { SessionUser } from '../domain/identity';

export interface SessionSnapshot {
  token: string | null;
  user: SessionUser | null;
  epoch: number;
  sessionID?: string | null;
  accessExpiresAt?: number | null;
}
export interface TokenResult {
  access_token: string;
  user?: SessionUser;
  session_id?: string;
  device_type?: 'phone' | 'tablet' | 'desktop';
  expires_in?: number;
  expires_at?: number;
}
interface Dependencies {
  read: () => SessionSnapshot;
  write: (token: string, user: SessionUser | null, result?: TokenResult) => void;
  end: () => void;
  refresh: (snapshot: SessionSnapshot, rejectedToken?: string | null) => Promise<TokenResult>;
  me: (token: string) => Promise<SessionUser>;
  login: (input: {
    username: string;
    password: string;
  }) => Promise<TokenResult>;
  logout: (snapshot: SessionSnapshot) => Promise<unknown>;
  beginLogout?: (snapshot: SessionSnapshot) => void;
  beforeLogin?: () => Promise<void>;
}

export function createSession(deps: Dependencies) {
  let refreshing: { epoch: number; promise: Promise<string> } | null = null;
  let restoring: { epoch: number; promise: Promise<void> } | null = null;
  let closing: Promise<void> | null = null;
  const check = (epoch: number) => {
    if (deps.read().epoch !== epoch) throw new CanceledError('会话已结束');
  };
  const refresh = (rejectedToken?: string | null): Promise<string> => {
    const state = deps.read();
    if (closing) return Promise.reject(new CanceledError('正在退出登录'));
    if (state.token && state.token !== rejectedToken && (!state.accessExpiresAt || state.accessExpiresAt * 1000 > Date.now()))
      return Promise.resolve(state.token);
    if (refreshing?.epoch === state.epoch) return refreshing.promise;
    const promise = deps
      .refresh(state, rejectedToken)
      .then((result) => {
        check(state.epoch);
        deps.write(result.access_token, deps.read().user, result);
        return result.access_token;
      })
      .finally(() => {
        if (refreshing?.promise === promise) refreshing = null;
      });
    refreshing = { epoch: state.epoch, promise };
    return promise;
  };
  return {
    refreshAfter401: (rejectedToken: string | null) => refresh(rejectedToken),
    async restore(): Promise<void> {
      if (deps.read().user) return;
      const epoch = deps.read().epoch;
      if (restoring?.epoch === epoch) return restoring.promise;
      const promise = (async () => {
        const token = await refresh();
        const user = await deps.me(token);
        check(epoch);
        deps.write(token, user);
      })().finally(() => {
        if (restoring?.promise === promise) restoring = null;
      });
      restoring = { epoch, promise };
      return promise;
    },
    async login(input: { username: string; password: string }): Promise<void> {
      if (closing) await closing;
      await deps.beforeLogin?.();
      deps.end();
      if (refreshing) await refreshing.promise.catch(() => {});
      const epoch = deps.read().epoch;
      const result = await deps.login(input);
      const user = result.user ?? (await deps.me(result.access_token));
      check(epoch);
      deps.write(result.access_token, user, result);
    },
    end: deps.end,
    logout(): Promise<void> {
      if (closing) return closing;
      const snapshot = deps.read();
      const inFlight = refreshing?.promise;
      try { deps.beginLogout?.(snapshot); }
      catch (error) { deps.end(); return Promise.reject(error); }
      deps.end();
      closing = (async () => {
        if (inFlight) await inFlight.catch(() => {});
        try {
          await deps.logout(snapshot);
        } finally {
          closing = null;
        }
      })();
      return closing;
    },
  };
}
