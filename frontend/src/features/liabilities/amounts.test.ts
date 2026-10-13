import { describe, expect, it } from 'vitest';
import type { DebtItem } from '../../api/liability';
import { liabilityAmounts } from './amounts';
import { sumMoney } from '../../lib/money';

const debt = (overrides: Partial<DebtItem> = {}): DebtItem => ({
  id: 1, source_id: 100, source_txn_id: 100, kind: 'principal',
  statement_id: 1, plan_id: 1, term_id: 11,
  amount: '332.79', remaining: '132.79', ...overrides,
});

describe('liability table amounts', () => {
  it('groups a term’s principal and realized fee without including future fees or joining other terms', () => {
    const items = [
      debt(),
      debt({ id: 2, source_id: 101, kind: 'fee', amount: '0.08', remaining: '0.05' }),
      debt({ id: 3, term_id: 12, statement_id: 2, amount: '332.77', remaining: '332.77' }),
    ];
    const view = liabilityAmounts({
      items, credits: [{ id: 1, source_id: 200 }],
      transactions: [{ id: 200, kind: 'transfer' }],
      allocations: [
        { id: 1, credit_event_id: 1, debt_item_id: 1, amount: '200' },
        { id: 2, credit_event_id: 1, debt_item_id: 2, amount: '0.03' },
      ],
    });
    expect(view.billItems(1)).toEqual([{
      key: 'term:11', planId: 1, termId: 11, kind: 'principal',
      amount: '332.87', fee: '0.08', remaining: '132.84', remainingFee: '0.05', sources: [100, 101],
    }]);
    expect(view.billFee(1)).toBe('0.08');
    expect(view.billPaidFee(1)).toBe('0.03');
    expect(view.billRemainingFee(1)).toBe('0.05');
    expect(view.paymentFee(1)).toBe('0.03');
    expect(view.termRemainingFee(11, 101)).toBe('0.05');
    expect(view.termRemainingFee(12, null)).toBe('0.00');
    expect(view.billItems(2)[0]?.amount).toBe('332.77');
    expect(view.billFee(2)).toBe('0.00');
  });

  it('keeps fee refunds separate from repayment and preserves net bill totals and source records', () => {
    const data: Parameters<typeof liabilityAmounts>[0] = {
      items: [
        debt({ remaining: '232.79' }),
        debt({ id: 2, source_id: 101, kind: 'fee', amount: '0.08', remaining: '0.03' }),
      ],
      credits: [{ id: 1, source_id: 200 }, { id: 2, source_id: 300 }],
      transactions: [{ id: 200, kind: 'transfer' }, { id: 300, kind: 'refund' }],
      allocations: [
        { id: 1, credit_event_id: 1, debt_item_id: 1, amount: '100' },
        { id: 2, credit_event_id: 1, debt_item_id: 2, amount: '0.02' },
        { id: 3, credit_event_id: 2, debt_item_id: 2, amount: '0.03' },
      ],
    };
    const original = JSON.stringify(data);
    const view = liabilityAmounts(data);
    expect(sumMoney(view.billItems(1).map((row) => row.amount))).toBe('332.84');
    expect(sumMoney(view.billItems(1).map((row) => row.remaining))).toBe('232.82');
    expect(view.billFee(1)).toBe('0.05');
    expect(view.billPaidFee(1)).toBe('0.02');
    expect(view.paymentFee(2)).toBe('0.00');
    expect(view.billItems(1)[0]?.sources).toEqual([100, 101]);
    expect(JSON.stringify(data)).toBe(original);
  });

  it('keeps multiple terms and ordinary purchases distinct in one bill; asset fees create no debt', () => {
    const view = liabilityAmounts({
      items: [debt(), debt({ id: 3, term_id: 12 }), debt({ id: 4, term_id: null, plan_id: null })],
      credits: [], transactions: [], allocations: [],
    });
    expect(view.billItems(1).map((row) => row.key)).toEqual(['term:11', 'term:12', 'item:4']);
    expect(view.termRemainingFee(11, 101)).toBe('0.00');
    expect(view.paymentFee(999)).toBe('0.00');
  });
});
