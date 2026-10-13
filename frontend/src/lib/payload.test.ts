import { describe, expect, it } from 'vitest';
import { parsePayload, notifText } from '../features/notifications/payload';
import { historySnapshot } from '../features/liabilities/history';

describe('historical JSON compatibility', () => {
  it('does not trust malformed payload shapes', () => {
    for (const value of ['{broken', 'null', '[]', '42'])
      expect(parsePayload(value)).toEqual({});
    expect(
      parsePayload('{"account_id":"not-an-id","resolved":"yes","debt":{}}')
    ).toEqual({});
    expect(notifText('future_kind', {})).toBe('请查看提醒记录');
  });
  it('keeps known resolved debt information', () => {
    const payload = parsePayload(
      '{"account_name":"测试信用","account_id":2,"resolved":true,"initial_debt":"100"}'
    );
    expect(notifText('repayment_due', payload)).toContain('已还清');
    expect(payload.account_id).toBe(2);
  });
  it('projects history fields and ignores invalid nested rows', () => {
    const snapshot = historySnapshot(
      JSON.stringify({
        amount: '10.00',
        plan_snapshot: {
          id: 1,
          version: 2,
          terms: [
            null,
            {
              id: 3,
              number: 1,
              principal: '10',
              fee_amount: '0',
              fee_date: '2026-10-12',
            },
          ],
        },
      })
    );
    expect(snapshot.amount).toBe('10.00');
    expect(snapshot.plan_snapshot?.terms).toHaveLength(1);
    expect(historySnapshot('null').statements).toEqual([]);
  });
  it('preserves new payment source snapshots and multi-plan history', () => {
    const snapshot = historySnapshot(JSON.stringify({
      term_ids: [3, 'invalid'],
      plan_snapshots: [{ id: 1, version: 2, terms: [] }, { id: 2, version: 3, terms: [] }],
      transaction_snapshots: [
        { id: 7, amount: '20', kind: 'expense', entry_date: '2026-11-01' },
        { id: 8, amount: '1020', kind: 'transfer', entry_date: '2026-11-01' },
        { id: 9, amount: 'invalid', kind: 'transfer', entry_date: '2026-11-01' },
      ],
    }));
    expect(snapshot.term_ids).toEqual([3]);
    expect(snapshot.plan_snapshots).toHaveLength(2);
    expect(snapshot.transactions.map((record) => record.amount)).toEqual(['20', '1020']);
  });
});
