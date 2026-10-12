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
