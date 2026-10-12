import axios from 'axios';

export interface ApiBody<T = unknown> {
  code: number;
  message: string;
  data?: T;
}
export type ErrorKind =
  | 'unauthorized'
  | 'forbidden'
  | 'business'
  | 'network'
  | 'protocol';
export class ApiError extends Error {
  constructor(
    message: string,
    public readonly kind: ErrorKind,
    public readonly status?: number,
    public readonly code?: number
  ) {
    super(message);
    this.name = 'ApiError';
  }
}
export function unwrapBody<T>(body: ApiBody<T>): T {
  checkBody(body);
  if (body.data === undefined)
    throw new ApiError('服务器响应缺少数据', 'protocol');
  return body.data;
}
export function checkBody(body: ApiBody): void {
  if (!body || typeof body.code !== 'number')
    throw new ApiError('服务器响应格式错误', 'protocol');
  if (body.code !== 0)
    throw new ApiError(
      body.message || '请求未成功',
      'business',
      undefined,
      body.code
    );
}
export function normalizeError(error: unknown): Error {
  if (axios.isCancel(error) || error instanceof ApiError) return error;
  if (axios.isAxiosError<ApiBody>(error)) {
    const status = error.response?.status;
    const kind =
      status === 401
        ? 'unauthorized'
        : status === 403
          ? 'forbidden'
          : status
            ? 'business'
            : 'network';
    return new ApiError(
      error.response?.data?.message || error.message,
      kind,
      status,
      error.response?.data?.code
    );
  }
  return error instanceof Error ? error : new Error('未知错误');
}
export function isUnauthorized(error: unknown): boolean {
  const normalized = normalizeError(error);
  return normalized instanceof ApiError && normalized.kind === 'unauthorized';
}
export function errMsg(error: unknown): string {
  return typeof error === 'string' ? error : normalizeError(error).message;
}
