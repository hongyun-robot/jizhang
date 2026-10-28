import axios, { CanceledError } from 'axios';
import { API_BASE } from '../base';
import { useAuth } from '../stores/auth';
import { queryClient } from '../api/queryClient';
import { unwrapBody, isUnauthorized, isTerminalAuth, normalizeError, type ApiBody } from '../api/errors';
import { clearNavigationMemory } from '../navigation/memory';
import { createSession } from './coordinator';
import { createBrowserAuth } from './browser';
import type { SessionUser } from '../domain/identity';

const authApi = axios.create({ baseURL: API_BASE, timeout: 10000, withCredentials: true });
export const browserAuth = createBrowserAuth();
let needsValidation = false;
let validating: Promise<void> | null = null;
const localEnd = (reason?: string) => {
  needsValidation = false;
  useAuth.getState().clear(reason);
  void queryClient.cancelQueries();
  queryClient.clear();
  clearNavigationMemory();
};
const me = async (token: string) => unwrapBody(
  (await authApi.get<ApiBody<SessionUser>>('/auth/me', { headers: { Authorization: 'Bearer ' + token } })).data
);
const coordinator = createSession({
  read: useAuth.getState,
  write: (token, user, result) => {
    useAuth.getState().setSession(token, user ?? result?.user ?? null, result?.session_id, result?.device_type, result?.expires_at);
  },
  end: localEnd,
  refresh: browserAuth.refresh,
  me,
  login: browserAuth.login,
  beforeLogin: browserAuth.beforeLogin,
  beginLogout: browserAuth.beginLogout,
  logout: browserAuth.logout,
});
const end = (reason = '登录已失效，请重新登录') => {
  try { browserAuth.end(useAuth.getState().sessionID, reason); }
  finally { localEnd(reason); }
};
const validate = (): Promise<void> => {
  if (validating) return validating;
  const snapshot = useAuth.getState();
  if (!snapshot.user || !snapshot.token) return Promise.resolve();
  needsValidation = true;
  useAuth.setState({ validationPending: true, validationError: null });
  const promise = (async () => {
    try {
      let user: SessionUser;
      try { user = await me(snapshot.token!); }
      catch (error) {
        if (!isUnauthorized(error) || isTerminalAuth(error)) throw error;
        const token = await coordinator.refreshAfter401(snapshot.token);
        user = await me(token);
      }
      if (useAuth.getState().epoch !== snapshot.epoch) throw new CanceledError('会话已结束');
      useAuth.getState().setUser(user);
      needsValidation = false;
    } catch (error) {
      if (useAuth.getState().epoch !== snapshot.epoch || axios.isCancel(error)) throw error;
      if (isUnauthorized(error)) end(normalizeError(error).message);
      else useAuth.setState({ validationError: normalizeError(error) });
      throw error;
    } finally {
      if (useAuth.getState().epoch === snapshot.epoch) useAuth.setState({ validationPending: false });
      validating = null;
    }
  })();
  validating = promise;
  return promise;
};
export const session = {
  ...coordinator,
  end,
  validate,
  beforeRequest: () => needsValidation ? validate() : Promise.resolve(),
  async retryLogout() {
    const marker = browserAuth.marker();
    await browserAuth.logout({ ...useAuth.getState(), sessionID: marker?.sid ?? null });
  },
};

browserAuth.subscribe(({ marker, result }) => {
  const state = useAuth.getState();
  if (marker.status !== 'active') {
    if (state.sessionID && state.sessionID !== marker.sid) return;
    if (state.token || state.user || !state.booted) localEnd(marker.reason ?? '登录已结束，请重新登录');
    else useAuth.setState({ endReason: marker.reason ?? '登录已结束，请重新登录' });
    return;
  }
  if (result) {
    if (state.sessionID && state.sessionID !== result.session_id) localEnd();
    useAuth.getState().setSession(result.access_token, result.user ?? useAuth.getState().user, result.session_id, result.device_type, result.expires_at);
  } else if (state.sessionID !== marker.sid) {
    localEnd();
    useAuth.setState({ booted: false });
    void coordinator.restore().catch((error) => {
      if (axios.isCancel(error)) return;
      if (isUnauthorized(error)) end(normalizeError(error).message);
      else useAuth.setState({ validationError: normalizeError(error) });
    });
  }
});
if (typeof window !== 'undefined') {
  const foreground = () => {
    if (document.visibilityState === 'hidden') { needsValidation = !!useAuth.getState().user; return; }
    if (useAuth.getState().user) void validate().catch(() => {});
  };
  window.addEventListener('focus', foreground);
  document.addEventListener('visibilitychange', foreground);
}
