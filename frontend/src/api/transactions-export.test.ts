import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from './client';
import { exportTxns } from './transactions';
import { session } from '../auth/session';
import { useAuth } from '../stores/auth';

const originalAdapter = api.defaults.adapter;
const anchor = { href: '', download: '', click: vi.fn(), remove: vi.fn() };
beforeEach(() => {
  useAuth.getState().setSession('test', { id: 1, username: 'member', role: 'member', status: 'active' });
  vi.stubGlobal('document', { createElement: () => anchor, body: { appendChild: vi.fn() } });
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test');
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
  anchor.click.mockClear(); anchor.remove.mockClear();
});
afterEach(() => { api.defaults.adapter = originalAdapter; session.end(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe('transaction downloads', () => {
  it.each(['xlsx', 'csv'] as const)('downloads all filtered rows in %s with the right type', async (format) => {
    api.defaults.adapter = async (config) => {
      const url = new URL(config.url ?? '', 'http://test');
      expect(url.searchParams.get('format')).toBe(format);
      expect(url.searchParams.get('order')).toBe('custom');
      expect(url.searchParams.get('keyword')).toBe('午饭');
      expect(url.searchParams.has('page')).toBe(false);
      expect(url.searchParams.has('page_size')).toBe(false);
      expect(config.timeout).toBe(format === 'xlsx' ? 55000 : 30000);
      return { config, status: 200, statusText: 'OK', headers: {
        'content-type': format === 'xlsx' ? 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' : 'text/csv; charset=utf-8',
        'content-disposition': `attachment; filename="transactions-test.${format}"`,
      }, data: new Blob(['file']) };
    };
    await exportTxns({ keyword: '午饭', page: 2, page_size: 20 }, format);
    expect(anchor.download).toBe(`transactions-test.${format}`);
    expect(anchor.click).toHaveBeenCalledTimes(1);
    expect(anchor.remove).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:test');
  });
  it('does not save JSON business failures or wrong content types as files', async () => {
    for (const mime of ['application/json', 'text/html']) {
      api.defaults.adapter = async (config) => ({ config, status: 200, statusText: 'OK', headers: { 'content-type': mime }, data: new Blob([JSON.stringify({ code: 10001, message: '导出失败' })]) });
      await expect(exportTxns({})).rejects.toThrow(mime === 'application/json' ? '导出失败' : '导出文件格式无效');
    }
    expect(anchor.click).not.toHaveBeenCalled();
    expect(URL.createObjectURL).not.toHaveBeenCalled();
  });
  it('does not download a late response after switching accounts', async () => {
    let ready!: () => void;
    let finish!: () => void;
    const started = new Promise<void>((resolve) => { ready = resolve; });
    api.defaults.adapter = async (config) => {
      ready(); await new Promise<void>((resolve) => { finish = resolve; });
      return { config, status: 200, statusText: 'OK', headers: { 'content-type': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }, data: new Blob(['file']) };
    };
    const pending = exportTxns({});
    const rejected = expect(pending).rejects.toThrow('会话已结束');
    await started;
    session.end(); useAuth.getState().setSession('other', { id: 2, username: 'other', role: 'viewer', status: 'active' });
    finish(); await rejected;
    expect(anchor.click).not.toHaveBeenCalled();
  });
});
