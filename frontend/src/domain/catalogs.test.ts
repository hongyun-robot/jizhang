import { describe, expect, it } from 'vitest';
import { categoryIndex, categoryPath } from './categories';
import { accountOptions } from './accounts';
import type { Account } from '../api/types';

describe('catalog presentation', () => {
  it('indexes grouped and ungrouped selectable categories consistently', () => {
    const index = categoryIndex([
      {
        id: 0,
        name: '支出',
        kind: 'expense',
        sort: 0,
        children: [
          {
            id: 1,
            name: '餐饮',
            kind: 'expense',
            sort: 0,
            children: [{ id: 2, name: '午餐', kind: 'expense', sort: 0 }],
          },
          { id: 3, name: '交通', kind: 'expense', sort: 1 },
        ],
      },
    ]);
    expect(index.info.get(2)).toEqual({
      name: '午餐',
      full: '餐饮/午餐',
      kind: 'expense',
      parent: 1,
    });
    expect(categoryPath(index.options, 1)).toEqual([1]);
    expect(categoryPath(index.options, 2)).toEqual([1, 2]);
    expect(categoryPath(index.options, 3)).toEqual([3]);
    expect(index.rootsFor('income')).toEqual([]);
  });
  it('preserves a selected disabled historical account and excludes another owner', () => {
    const asset: Account = {
      id: 1,
      name: '现金',
      type: 'asset',
      status: 'active',
      sort: 0,
      opening_balance: '0',
    };
    const credit: Account = {
      ...asset,
      id: 2,
      name: '信用',
      type: 'liability',
      owner_id: 1,
      status: 'disabled',
    };
    const accounts = [
      asset,
      credit,
      { ...credit, id: 3, owner_id: 2, status: 'active' as const },
    ];
    expect(
      accountOptions(accounts, { ownerId: 1 }).map((row) => row.value)
    ).toEqual([1]);
    expect(
      accountOptions(accounts, {
        ownerId: 1,
        history: true,
        selectedId: 2,
      }).map((row) => row.value)
    ).toEqual([1, 2]);
    expect(
      accountOptions(accounts, { ownerId: 1, assetOnly: true }).map(
        (row) => row.value
      )
    ).toEqual([1]);
  });
});
