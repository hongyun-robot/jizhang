import type { AxiosInstance, Method, AxiosRequestConfig } from 'axios';
import { api } from './client';
import { checkBody, normalizeError, unwrapBody, type ApiBody } from './errors';

export function createJsonRequester(client: AxiosInstance) {
  async function send<T>(
    url: string,
    options: AxiosRequestConfig
  ): Promise<ApiBody<T>> {
    try {
      const response = await client.request<ApiBody<T>>({ url, ...options });
      return response.data;
    } catch (error) {
      throw normalizeError(error);
    }
  }
  async function request<T>(
    url: string,
    options: AxiosRequestConfig = {}
  ): Promise<T> {
    return unwrapBody(await send<T>(url, options));
  }
  return Object.assign(request, {
    empty: async (url: string, options: AxiosRequestConfig): Promise<void> => {
      checkBody(await send<void>(url, options));
    },
  });
}
export const requestJson = createJsonRequester(api);
export const readApi = <T>(url: string, signal?: AbortSignal): Promise<T> =>
  requestJson<T>(url, { signal });
export const writeApi = <T>(
  method: Method,
  url: string,
  data?: unknown
): Promise<T> => requestJson<T>(url, { method, data });
export const writeEmptyApi = (
  method: Method,
  url: string,
  data?: unknown
): Promise<void> => requestJson.empty(url, { method, data });
