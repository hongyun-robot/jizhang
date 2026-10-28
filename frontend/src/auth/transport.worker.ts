import { authFetch, createAuthQueue, type AuthCommand } from './wire';
import { API_BASE } from '../base';
import { ApiError } from '../api/errors';
import type { TokenResult } from './coordinator';

const enqueue = createAuthQueue();
let failed = false;
let cached: { result: TokenResult; expires: number } | null = null;
const scope = self as unknown as { onconnect: (event: MessageEvent) => void };
scope.onconnect = (event) => {
  const port = event.ports[0];
  port.onmessage = (message: MessageEvent<{ id: string; command: AuthCommand }>) => {
    const { id, command } = message.data;
    void enqueue(async () => {
      try {
        if (failed) throw new Error('认证协调已中断，请关闭此站点标签页后重新打开');
        if (command.base !== API_BASE || !['login', 'refresh', 'logout'].includes(command.operation)) {
          throw new Error('无效的认证请求');
        }
        if (command.operation === 'refresh' && cached && cached.expires > Date.now() &&
          cached.result.session_id === command.body.session_id && cached.result.access_token !== command.rejectedToken) {
          port.postMessage({ id, result: cached.result });
          return;
        }
        const result = await authFetch(command);
        cached = result ? { result, expires: (result.expires_at ?? Date.now() / 1000 + (result.expires_in ?? 1800)) * 1000 } : null;
        port.postMessage({ id, result });
      } catch (caught) {
        const error = caught instanceof Error ? caught : new Error('认证服务暂不可用');
        if (!(error instanceof ApiError)) failed = true;
        port.postMessage({ id, error: {
          message: error.message,
          kind: error instanceof ApiError ? error.kind : 'network',
          status: error instanceof ApiError ? error.status : undefined,
          code: error instanceof ApiError ? error.code : undefined,
        } });
      }
    });
  };
  port.start();
};
