import { describe, expect, it } from 'vitest';
import { accountOptions, isCashAccount } from './accounts';
import type { Account } from '../api/types';

describe('理财零钱账户', () => {
  const wallet: Account = { id: 9, owner_id: 1, name: '余利宝', type: 'asset', asset_class: 'wealth_cash', opening_balance: '100', status: 'active', sort: 0 };
  it('在普通收支与转账中可选，独立理财持仓仍不可直接支付', () => {
    expect(isCashAccount(wallet)).toBe(true);
    expect(accountOptions([wallet, { ...wallet, id: 10, asset_class: 'wealth' }])).toEqual([{ value: 9, label: '余利宝' }]);
  });
  it('停用后只在已有历史的选择中保留', () => {
    const stopped = { ...wallet, status: 'disabled' as const };
    expect(accountOptions([stopped])).toEqual([]);
    expect(accountOptions([stopped], { history: true, selectedId: 9 })).toEqual([{ value: 9, label: '余利宝（已停用）' }]);
  });
});
