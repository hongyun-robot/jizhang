import { QueryClient } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { invalidateFinancialViews } from './cache';
import { queryKeys } from './keys';

describe('financial views', () => {
  it('refreshes both list shapes and related views without touching another session', async () => {
    const client = new QueryClient();
    const current = [
      queryKeys.key('txns', 1, 'page=1'),
      queryKeys.key('txns-inf', 1, 'kind=expense', 1, 20),
      queryKeys.key('accounts', 1),
      queryKeys.key('budgets', 1),
      queryKeys.key('closings', 1, 'preview'),
      queryKeys.key('wealth', 1, 'position', 10),
    ];
    for (const key of current) client.setQueryData(key, {});
    const previous = queryKeys.key('txns', 0, 'page=1');
    client.setQueryData(previous, {});
    await invalidateFinancialViews(client, 1);
    for (const key of current)
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryState(previous)?.isInvalidated).toBe(false);
  });
  it('separates infinite page size, starting page and regular list data', () => {
    expect(queryKeys.key('txns-inf', 1, 'same', 1, 20)).not.toEqual(
      queryKeys.key('txns-inf', 1, 'same', 1, 50)
    );
    expect(queryKeys.key('txns-inf', 1, 'same', 2, 20)).not.toEqual(
      queryKeys.key('txns-inf', 1, 'same', 1, 20)
    );
    expect(queryKeys.key('txns', 1, 'same')).not.toEqual(
      queryKeys.key('txns-inf', 1, 'same')
    );
  });
});
