import { ApiError, type ApiBody } from '../api/errors';
import type { TokenResult } from './coordinator';

export type AuthOperation = 'login' | 'refresh' | 'logout';
export interface AuthCommand {
  operation: AuthOperation;
  base: string;
  body: Record<string, unknown>;
  token?: string | null;
  rejectedToken?: string | null;
}
export async function authFetch(command: AuthCommand): Promise<TokenResult | null> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(command.base + '/auth/' + command.operation, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        ...(command.token ? { Authorization: 'Bearer ' + command.token } : {}),
      },
      body: JSON.stringify(command.body),
      signal: controller.signal,
    });
    const body: ApiBody<TokenResult> = await response.json();
    if (!response.ok || body.code !== 0) {
      throw new ApiError(
        body.message || '认证请求失败',
        response.status === 401 ? 'unauthorized' : 'business',
        response.status,
        body.code
      );
    }
    if (command.operation === 'logout') return null;
    const result = body.data;
    if (!result?.access_token || !result.session_id || !['phone', 'tablet', 'desktop'].includes(result.device_type ?? '')) {
      throw new ApiError('认证响应缺少会话信息，请刷新页面重试', 'protocol');
    }
    return { ...result, expires_at: result.expires_at ?? Date.now() / 1000 + (result.expires_in ?? 1800) };
  } finally {
    clearTimeout(timeout);
  }
}

// The queue includes response consumption, not just request dispatch.
export function createAuthQueue() {
  let tail: Promise<unknown> = Promise.resolve();
  return function enqueue<T>(run: () => Promise<T>): Promise<T> {
    const result = tail.then(run);
    tail = result.catch(() => {});
    return result;
  };
}
