import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { updatePlatformCache } from './platformCache';
import { queryKeys } from './keys';
import { useAuth } from '../stores/auth';

afterEach(() => { vi.restoreAllMocks(); useAuth.getState().clear(); });

describe('personal platform directory cache', () => {
  it('preserves saved order when names change or a new row is appended', async () => {
    const client = new QueryClient();
    const epoch = useAuth.getState().epoch;
    const directory = queryKeys.key('voucher-platforms', epoch);
    client.setQueryData(directory, [{ id: 1, name: 'Z常用', sort: 0 }, { id: 2, name: 'A后来', sort: 1 }]);
    await updatePlatformCache(client, epoch, (rows) => [...rows.map((row) => row.id === 1 ? { ...row, name: 'ZZ改名' } : row), { id: 3, name: '0新建', sort: 2 }]);
    expect(client.getQueryData(directory)).toEqual([{ id: 1, name: 'ZZ改名', sort: 0 }, { id: 2, name: 'A后来', sort: 1 }, { id: 3, name: '0新建', sort: 2 }]);
  });
  it('discards a mutation completing while its old session is being cleared', async () => {
    const client = new QueryClient();
    const epoch = useAuth.getState().epoch;
    let release!: () => void;
    vi.spyOn(client, 'cancelQueries').mockImplementationOnce(() =>
      new Promise<void>((resolve) => { release = resolve; })
    );
    const update = vi.fn(() => [{ id: 10, name: '甲的私有商家', sort: 0 }]);
    const saving = updatePlatformCache(client, epoch, update);
    useAuth.getState().clear();
    client.clear();
    const newKey = queryKeys.key('voucher-platforms', useAuth.getState().epoch);
    client.setQueryData(newKey, [{ id: 20, name: '乙的商家' }]);
    release();
    await saving;
    expect(update).not.toHaveBeenCalled();
    expect(client.getQueryData(queryKeys.key('voucher-platforms', epoch))).toBeUndefined();
    expect(client.getQueryData(newKey)).toEqual([{ id: 20, name: '乙的商家' }]);
  });
  it('invalidates only the originating directory after a successful update', async () => {
    const client = new QueryClient();
    const epoch = useAuth.getState().epoch;
    const directory = queryKeys.key('voucher-platforms', epoch);
    const accounts = queryKeys.key('accounts', epoch);
    client.setQueryData(directory, [{ id: 1, name: '旧名称' }]);
    client.setQueryData(accounts, [{ balance: '100' }]);
    await updatePlatformCache(client, epoch, () => [{ id: 1, name: '新名称', sort: 0 }]);
    expect(client.getQueryData(directory)).toEqual([{ id: 1, name: '新名称', sort: 0 }]);
    expect(client.getQueryState(accounts)?.isInvalidated).toBe(false);
    expect(client.getQueryData(accounts)).toEqual([{ balance: '100' }]);
  });
});
