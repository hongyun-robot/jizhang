import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios';
import { describe, expect, it } from 'vitest';
import { createJsonRequester } from './request';
import { ApiError, unwrapBody } from './errors';

const response = (
  config: InternalAxiosRequestConfig,
  data: unknown,
  status = 200
) => ({ config, data, status, statusText: String(status), headers: {} });
describe('JSON transport contract', () => {
  it('accepts the existing Go empty-write envelope without requiring a data field', async () => {
    const client = axios.create({
      adapter: async (config) => response(config, { code: 0, message: 'ok' }),
    });
    await expect(
      createJsonRequester(client).empty('/transactions/1', { method: 'delete' })
    ).resolves.toBeUndefined();
  });
  it('unwraps data and forwards method, body and abort signal', async () => {
    const controller = new AbortController();
    const client = axios.create({
      adapter: async (config) => {
        expect(config.signal).toBe(controller.signal);
        expect(JSON.parse(config.data)).toEqual({
          request_key: 'attempt',
          amount: '10.00',
        });
        return response(config, { code: 0, data: { id: 7 } });
      },
    });
    expect(
      await createJsonRequester(client)('/transactions', {
        method: 'post',
        data: { request_key: 'attempt', amount: '10.00' },
        signal: controller.signal,
      })
    ).toEqual({ id: 7 });
  });
  it('keeps real business messages and rejects missing success payloads', () => {
    expect(() => unwrapBody({ code: 5, message: '该账期已封账' })).toThrow(
      '该账期已封账'
    );
    expect(() => unwrapBody({ code: 0, message: '' })).toThrow('缺少数据');
    expect(unwrapBody({ code: 0, message: '', data: null })).toBeNull();
  });
  it.each([
    [401, 'unauthorized'],
    [403, 'forbidden'],
    [500, 'business'],
  ] as const)('classifies HTTP %s', async (status, kind) => {
    const client = axios.create({
      adapter: async (config) => {
        throw new AxiosError(
          'HTTP error',
          '',
          config,
          undefined,
          response(config, { code: 5, message: '服务器拒绝' }, status)
        );
      },
    });
    await expect(createJsonRequester(client)('/test')).rejects.toMatchObject({
      kind,
      status,
      message: '服务器拒绝',
    });
  });
  it('preserves cancellation instead of turning it into a business failure', async () => {
    const controller = new AbortController();
    controller.abort();
    const client = axios.create({
      adapter: async (config) => response(config, { code: 0, data: 1 }),
    });
    try {
      await createJsonRequester(client)('/test', { signal: controller.signal });
      throw new Error('not cancelled');
    } catch (error) {
      expect(axios.isCancel(error)).toBe(true);
      expect(error).not.toBeInstanceOf(ApiError);
    }
  });
});
