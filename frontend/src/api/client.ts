import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios';

import { API_BASE, LOGIN_PATH } from '../base';
import { useAuth } from '../stores/auth';
import type { User } from '../stores/auth';

/** 统一响应体类型。 */
export interface ApiBody<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

interface TokenResp {
  access_token: string;
  token_type: string;
  expires_in: number;
  user?: User;
}

/** 统一 API 客户端：baseURL 指向 API_BASE（开发期 /api/v1 由 Vite 代理转发；子路径部署为 /jizhang/api/v1）。 */
export const api = axios.create({
  baseURL: API_BASE,
  timeout: 10000,
});

api.interceptors.request.use((config) => {
  const token = useAuth.getState().token;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

/** 单飞刷新：并发 401 只发一次 refresh。 */
let refreshing: Promise<string> | null = null;

async function refreshAccessToken(): Promise<string> {
  const { data } = await axios.post<ApiBody<TokenResp>>(`${API_BASE}/auth/refresh`, null, {
    withCredentials: true,
  });
  if (data.code !== 0 || !data.data?.access_token) {
    throw new Error(data.message || 'refresh failed');
  }
  useAuth.getState().setToken(data.data.access_token);
  return data.data.access_token;
}

export async function ensureFreshToken(): Promise<string> {
  const existing = useAuth.getState().token;
  if (existing) return existing;
  if (!refreshing) {
    refreshing = refreshAccessToken().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

type RetriableConfig = InternalAxiosRequestConfig & { _retried?: boolean };

api.interceptors.response.use(
  (res) => res,
  async (error: AxiosError<ApiBody>) => {
    const config = error.config as RetriableConfig | undefined;
    const isAuthRoute = config?.url?.includes('/auth/');
    const is401 = error.response?.status === 401;

    if (is401 && config && !config._retried && !isAuthRoute) {
      config._retried = true;
      try {
        const token = await ensureFreshToken();
        config.headers.Authorization = `Bearer ${token}`;
        return api(config);
      } catch {
        useAuth.getState().clear();
        if (!window.location.pathname.startsWith(LOGIN_PATH)) {
          window.location.assign(LOGIN_PATH);
        }
      }
    }
    return Promise.reject(error);
  },
);

/** 从响应体中取业务消息。 */
export function errMsg(err: unknown): string {
  if (axios.isAxiosError(err)) {
    return err.response?.data?.message || err.message;
  }
  return err instanceof Error ? err.message : '未知错误';
}
