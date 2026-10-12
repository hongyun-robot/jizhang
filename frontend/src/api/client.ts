import axios, { CanceledError, type InternalAxiosRequestConfig } from 'axios';
import { API_BASE } from '../base';
import { useAuth } from '../stores/auth';
import { session } from '../auth/session';
import { isUnauthorized, normalizeError } from './errors';

export { errMsg } from './errors';
export type { ApiBody } from './errors';

type SessionConfig = InternalAxiosRequestConfig & {
  _epoch?: number;
  _token?: string | null;
  _retried?: boolean;
};
export const api = axios.create({ baseURL: API_BASE, timeout: 10000 });
api.interceptors.request.use((input) => {
  const config = input as SessionConfig;
  const state = useAuth.getState();
  if (config._epoch !== undefined && config._epoch !== state.epoch)
    throw new CanceledError('会话已结束');
  config._epoch = state.epoch;
  config._token = state.token;
  if (state.token) config.headers.Authorization = `Bearer ${state.token}`;
  return config;
});
api.interceptors.response.use(
  (response) => {
    const config = response.config as SessionConfig;
    if (config._epoch !== useAuth.getState().epoch)
      throw new CanceledError('会话已结束');
    return response;
  },
  async (error: unknown) => {
    const config = axios.isAxiosError(error)
      ? (error.config as SessionConfig | undefined)
      : undefined;
    if (
      config?._epoch !== undefined &&
      config._epoch !== useAuth.getState().epoch
    )
      throw new CanceledError('会话已结束');
    if (config && isUnauthorized(error) && !config.url?.startsWith('/auth/')) {
      if (config._retried) {
        session.end();
        throw normalizeError(error);
      }
      config._retried = true;
      try {
        const token = await session.refreshAfter401(config._token ?? null);
        config.headers.Authorization = `Bearer ${token}`;
        return await api(config);
      } catch (caught) {
        if (
          isUnauthorized(caught) &&
          config._epoch === useAuth.getState().epoch
        )
          session.end();
        throw normalizeError(caught);
      }
    }
    throw normalizeError(error);
  }
);
