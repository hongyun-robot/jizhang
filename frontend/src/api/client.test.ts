import { afterEach, describe, expect, it, vi } from 'vitest';
import { AxiosError, type InternalAxiosRequestConfig } from 'axios';
import { api } from './client';
import { session } from '../auth/session';
import { useAuth } from '../stores/auth';
import { createJsonRequester } from './request';

const originalAdapter = api.defaults.adapter;
const user = {
  id: 1,
  username: 'member',
  role: 'member' as const,
  status: 'active' as const,
};
const response = (config: InternalAxiosRequestConfig, status: number) => ({
  config,
  status,
  statusText: String(status),
  headers: {},
  data: {
    code: status === 200 ? 0 : 10002,
    message: 'test',
    data: { saved: true },
  },
});
afterEach(() => {
  api.defaults.adapter = originalAdapter;
  session.end();
  vi.restoreAllMocks();
});

describe('authenticated request retry', () => {
  it.each([413, 403, 500])('decodes a Blob JSON failure (%s) before normalization', async (status) => {
    useAuth.getState().setSession('old', user);
    api.defaults.adapter = async (config) => {
      throw new AxiosError('download failed', '', config, undefined, {
        ...response(config, status), headers: { 'content-type': 'application/json' },
        data: new Blob([JSON.stringify({ code: 10001, message: '最多 100,000 条，请选择 CSV' })], { type: 'application/json' }),
      });
    };
    await expect(api.get('/transactions/export', { responseType: 'blob' })).rejects.toMatchObject({ status, code: 10001, message: '最多 100,000 条，请选择 CSV' });
  });
  it('ends a terminal session received as Blob without refreshing', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi.spyOn(session, 'refreshAfter401');
    api.defaults.adapter = async (config) => {
      throw new AxiosError('ended', '', config, undefined, {
        ...response(config, 401), headers: { 'content-type': 'application/json' },
        data: new Blob([JSON.stringify({ code: 10008, message: '会话结束' })]),
      });
    };
    await expect(api.get('/transactions/export', { responseType: 'blob' })).rejects.toMatchObject({ code: 10008 });
    expect(refresh).not.toHaveBeenCalled();
    expect(useAuth.getState().user).toBeNull();
  });
  it('refreshes an ordinary Blob 401 once', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi.spyOn(session, 'refreshAfter401').mockImplementation(async () => {
      useAuth.getState().setToken('new'); return 'new';
    });
    api.defaults.adapter = async (config) => {
      if (config.headers.Authorization === 'Bearer old') throw new AxiosError('expired', '', config, undefined, {
        ...response(config, 401), headers: { 'content-type': 'application/json' },
        data: new Blob([JSON.stringify({ code: 10007, message: 'expired' })]),
      });
      return response(config, 200);
    };
    await api.get('/transactions/export', { responseType: 'blob' });
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it('handles malformed Blob errors and isolates account changes during decoding', async () => {
    useAuth.getState().setSession('old', user);
    api.defaults.adapter = async (config) => {
      throw new AxiosError('bad gateway', '', config, undefined, {
        ...response(config, 502), headers: { 'content-type': 'application/json' }, data: new Blob(['{invalid']),
      });
    };
    await expect(api.get('/transactions/export', { responseType: 'blob' })).rejects.toMatchObject({ status: 502 });
    let ready!: () => void;
    let finish!: (value: string) => void;
    const started = new Promise<void>((resolve) => { ready = resolve; });
    class DelayedBlob extends Blob {
      text() { ready(); return new Promise<string>((resolve) => { finish = resolve; }); }
    }
    api.defaults.adapter = async (config) => {
      throw new AxiosError('ended', '', config, undefined, {
        ...response(config, 401), headers: { 'content-type': 'application/json' }, data: new DelayedBlob(),
      });
    };
    const pending = api.get('/transactions/export', { responseType: 'blob' });
    const rejected = expect(pending).rejects.toThrow('会话已结束');
    await started;
    session.end(); useAuth.getState().setSession('other', { ...user, id: 2 });
    finish(JSON.stringify({ code: 10008, message: 'old session' }));
    await rejected;
    expect(useAuth.getState().user?.id).toBe(2);
  });
  it('ends a replaced session without refreshing or logging out other devices', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi.spyOn(session, 'refreshAfter401');
    const logout = vi.spyOn(session, 'logout');
    api.defaults.adapter = async (config) => {
      const rejected = response(config, 401);
      rejected.data.code = 10009;
      throw new AxiosError('replaced', '', config, undefined, rejected);
    };
    await expect(createJsonRequester(api)('/accounts')).rejects.toMatchObject({ code: 10009 });
    expect(refresh).not.toHaveBeenCalled();
    expect(logout).not.toHaveBeenCalled();
    expect(useAuth.getState().user).toBeNull();
  });
  it('keeps the session on a retryable authentication service failure', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi.spyOn(session, 'refreshAfter401');
    api.defaults.adapter = async (config) => {
      const rejected = response(config, 503);
      rejected.data.code = 20001;
      throw new AxiosError('unavailable', '', config, undefined, rejected);
    };
    await expect(createJsonRequester(api)('/transactions')).rejects.toMatchObject({ status: 503 });
    expect(refresh).not.toHaveBeenCalled();
    expect(useAuth.getState().user).toEqual(user);
  });
  it('retries once with the same write body and operation key', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi
      .spyOn(session, 'refreshAfter401')
      .mockImplementation(async () => {
        useAuth.getState().setToken('new');
        return 'new';
      });
    const bodies: string[] = [];
    api.defaults.adapter = async (config) => {
      bodies.push(config.data);
      if (config.headers.Authorization === 'Bearer old')
        throw new AxiosError(
          'expired',
          '',
          config,
          undefined,
          response(config, 401)
        );
      return response(config, 200);
    };
    await createJsonRequester(api)('/transactions', {
      method: 'post',
      data: { amount: '10', request_key: 'attempt' },
    });
    expect(bodies).toHaveLength(2);
    expect(bodies[0]).toBe(bodies[1]);
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it('does not refresh on 403', async () => {
    useAuth.getState().setSession('old', user);
    const refresh = vi.spyOn(session, 'refreshAfter401');
    api.defaults.adapter = async (config) => {
      throw new AxiosError(
        'forbidden',
        '',
        config,
        undefined,
        response(config, 403)
      );
    };
    await expect(
      createJsonRequester(api)('/transactions')
    ).rejects.toMatchObject({ kind: 'forbidden' });
    expect(refresh).not.toHaveBeenCalled();
  });
  it('ends the session if a refreshed request remains unauthorized', async () => {
    useAuth.getState().setSession('old', user);
    vi.spyOn(session, 'refreshAfter401').mockImplementation(async () => {
      useAuth.getState().setToken('new');
      return 'new';
    });
    let calls = 0;
    api.defaults.adapter = async (config) => {
      calls++;
      throw new AxiosError(
        'expired',
        '',
        config,
        undefined,
        response(config, 401)
      );
    };
    await expect(createJsonRequester(api)('/accounts')).rejects.toMatchObject({
      kind: 'unauthorized',
    });
    expect(calls).toBe(2);
    expect(useAuth.getState().user).toBeNull();
  });
  it('rejects a delayed response after an account change', async () => {
    useAuth.getState().setSession('old', user);
    let finish!: () => void;
    let started!: () => void;
    const ready = new Promise<void>((resolve) => {
      started = resolve;
    });
    api.defaults.adapter = async (config) => {
      started();
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
      return response(config, 200);
    };
    const reading = createJsonRequester(api)('/liabilities');
    const rejected = expect(reading).rejects.toThrow('会话已结束');
    await ready;
    session.end();
    useAuth.getState().setSession('other', { ...user, id: 2 });
    finish();
    await rejected;
    expect(useAuth.getState().user?.id).toBe(2);
  });
});
