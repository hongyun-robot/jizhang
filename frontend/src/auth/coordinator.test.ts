import { describe, expect, it, vi } from 'vitest';
import {
  createSession,
  type SessionSnapshot,
  type TokenResult,
} from './coordinator';

const user = {
  id: 1,
  username: 'member',
  role: 'member' as const,
  status: 'active' as const,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function harness() {
  let state: SessionSnapshot = { token: 'old', user, epoch: 0 };
  const result = deferred<TokenResult>();
  const refresh = vi.fn(() => result.promise);
  const logout = vi.fn(async () => {});
  const me = vi.fn(async () => user);
  const coordinator = createSession({
    read: () => state,
    write: (token, nextUser) => {
      state = { ...state, token, user: nextUser };
    },
    end: () => {
      state = { token: null, user: null, epoch: state.epoch + 1 };
    },
    refresh,
    me,
    logout,
    login: async () => ({ access_token: 'login-token', user }),
  });
  return {
    coordinator,
    result,
    refresh,
    logout,
    me,
    read: () => state,
    reset: () => {
      state = { token: null, user: null, epoch: 0 };
    },
    expire: () => { state={...state,accessExpiresAt:Date.now()/1000-1}; },
  };
}
describe('session concurrency', () => {
  it('refreshes when a newer local token has also expired before a late 401 arrives', async () => {
    const h=harness();h.expire();
    const refreshing=h.coordinator.refreshAfter401('even-older');
    expect(h.refresh).toHaveBeenCalledTimes(1);
    h.result.resolve({access_token:'fresh'});
    expect(await refreshing).toBe('fresh');
  });
  it('refreshes an existing rejected token once and reuses the new token for late 401s', async () => {
    const h = harness();
    const requests = [
      h.coordinator.refreshAfter401('old'),
      h.coordinator.refreshAfter401('old'),
    ];
    expect(h.refresh).toHaveBeenCalledTimes(1);
    h.result.resolve({ access_token: 'new' });
    expect(await Promise.all(requests)).toEqual(['new', 'new']);
    expect(await h.coordinator.refreshAfter401('old')).toBe('new');
    expect(h.refresh).toHaveBeenCalledTimes(1);
  });
  it('prevents a refresh result from restoring an ended session and logs out the rotated cookie', async () => {
    const h = harness();
    const refreshing = h.coordinator.refreshAfter401('old');
    const rejected = expect(refreshing).rejects.toThrow('会话已结束');
    const closing = h.coordinator.logout();
    expect(h.read().token).toBeNull();
    expect(h.logout).not.toHaveBeenCalled();
    h.result.resolve({ access_token: 'late' });
    await rejected;
    await closing;
    expect(h.logout).toHaveBeenCalledTimes(1);
    expect(h.read().user).toBeNull();
  });
  it('restores a missing session once across repeated guard mounts', async () => {
    const h = harness();
    h.reset();
    const requests = [h.coordinator.restore(), h.coordinator.restore()];
    h.result.resolve({ access_token: 'new' });
    await Promise.all(requests);
    expect(h.refresh).toHaveBeenCalledTimes(1);
    expect(h.me).toHaveBeenCalledTimes(1);
    expect(h.read().user).toEqual(user);
  });
  it('does not reuse the old account state when logging in again', async () => {
    const h = harness();
    await h.coordinator.login({ username: 'member', password: 'test' });
    expect(h.read().epoch).toBe(1);
    expect(h.read().token).toBe('login-token');
  });
});
