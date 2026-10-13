import { describe, expect, it } from 'vitest';
import { accountOptions } from './accounts';
import { canModifyTransaction } from './permissions';
import { assetOptions } from '../features/statistics/options';
import { fundMoney } from './funds';
import type { Account, AssetsResp, Transaction } from '../api/types';

describe('fund boundary in existing views', () => {
  it('displays exact opening cost in cents, including half-cent boundaries', () => {
    expect(fundMoney('7099.908578')).toBe('7099.91');
    expect(fundMoney('1.005')).toBe('1.01');
    expect(fundMoney('-1.005')).toBe('-1.01');
    expect(fundMoney('9999999999.98499999')).toBe('9999999999.98');
  });
  it('keeps fund assets out of ordinary payment options, including historical options', () => {
    const accounts: Account[] = [
      { id: 1, name: '现金', type: 'asset', asset_class: 'cash', owner_id: 1, opening_balance: '0', sort: 0, status: 'active' },
      { id: 2, name: '基金', type: 'asset', asset_class: 'fund', owner_id: 1, opening_balance: '0', sort: 0, status: 'active' },
    ];
    expect(accountOptions(accounts).map((v) => v.value)).toEqual([1]);
    expect(accountOptions(accounts, { history: true, selectedId: 2 }).map((v) => v.value)).toEqual([1]);
  });
  it('protects managed fund transfers from ordinary edit/delete', () => {
    const user = { id: 1, username: 'owner', role: 'admin' as const, status: 'active' as const };
    const transaction: Transaction = { id: 1, kind: 'transfer', amount: '100', account_id: 1, transfer_account_id: 2, entry_date: '2026-10-12', period_key: '2026-10', note: '', created_by: 1, source: 'manual', purpose: 'fund_purchase', fund_event_id: 1, created_at: '2026-10-12', updated_at: '2026-10-12' };
    expect(canModifyTransaction(user, transaction)).toBe(false);
  });
  it('preserves a missing historical valuation as a chart gap', () => {
    const data: AssetsResp = { start: '2026-10-09', end: '2026-10-09', ending_total: null, points: [{ date: '2026-10-09', total: null, accounts: [{ account_id: 2, name: '基金', type: 'asset', balance: null, asset_class: 'fund' }] }] };
    const chart = assetOptions(data, { dark: false, isMobile: false, TEAL: '#123', GREEN: '#234', RED: '#345', PALETTE: ['#123'], pieBorder: '#fff', prevBar: '#555' });
    expect(chart.series).toEqual(expect.arrayContaining([expect.objectContaining({ name: '总资产', data: [null] }), expect.objectContaining({ name: '基金', data: [null] })]));
  });
});
