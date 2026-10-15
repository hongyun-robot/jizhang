import { describe, expect, it } from 'vitest';
import { moveItem, orderItems } from './ordering';
import { accountOptions } from '../domain/accounts';
import { categoryIndex } from '../domain/categories';
import type { Account } from '../api/types';

describe('directory ordering and business options', () => {
  it('reorders the newest entity objects and retains newly received members', () => {
    const current = [{ id: 1, balance: '120' }, { id: 2, balance: '50' }, { id: 3, balance: '10' }];
    expect(orderItems(current, [2, 1])).toEqual([current[1], current[0], current[2]]);
    expect(orderItems(current, [2, 999, 1])).toEqual([current[1], current[0], current[2]]);
    expect(moveItem([1, 2, 3], 2, 0)).toEqual([3, 1, 2]);
  });
  it('keeps relative account order while excluding funds, disabled and ineligible types', () => {
    const base: Account = { id: 1, name: '现金', type: 'asset', opening_balance: '0', status: 'active', sort: 0, owner_id: 1 };
    const rows: Account[] = [{ ...base, id: 4, name: '信用', type: 'liability' }, { ...base, id: 3, name: '基金', asset_class: 'fund' }, { ...base, id: 2, name: '停用', status: 'disabled' }, base, { ...base, id: 5, name: '另一现金' }];
    expect(accountOptions(rows).map((row) => row.value)).toEqual([4, 1, 5]);
    expect(accountOptions(rows, { assetOnly: true }).map((row) => row.value)).toEqual([1, 5]);
    expect(accountOptions(rows, { history: true, selectedId: 2 }).map((row) => row.value)).toEqual([4, 2, 1, 5]);
  });
  it('uses saved root and child order without submitting virtual grouping IDs', () => {
    const index = categoryIndex([{ id: 0, name: '', kind: 'expense', sort: 0, children: [{ id: 3, name: '交通', kind: 'expense', sort: 0 }, { id: 1, name: '餐饮', kind: 'expense', sort: 1, children: [{ id: 4, name: '晚餐', kind: 'expense', sort: 0 }, { id: 2, name: '早餐', kind: 'expense', sort: 1 }] }] }]);
    expect(index.roots.map((row) => row.id)).toEqual([3, 1]);
    expect(index.options[1].children?.map((row) => row.value)).toEqual([4, 2]);
    expect(index.info.get(4)?.full).toBe('餐饮/晚餐');
  });
});
