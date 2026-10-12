import { describe, expect, it } from 'vitest';
import { plannedBills, ruleError } from './liability';

const rule = {
  id: 1,
  version: 1,
  effective_from: '2000-01-01',
  statement_day: 31,
  repayment_day: 10,
  repayment_month_offset: 1,
};

describe('bill date previews', () => {
  it('clamps short months and respects leap years', () => {
    expect(plannedBills([rule], '2024-02-01', 2)).toEqual([
      { close: '2024-02-29', due: '2024-03-10' },
      { close: '2024-03-31', due: '2024-04-10' },
    ]);
    expect(plannedBills([rule], '2027-02-01', 1)[0]?.close).toBe('2027-02-28');
  });
  it('rejects same-month repayment that fails in February', () => {
    expect(ruleError(28, 31, 0)).toBeTruthy();
    expect(ruleError(20, 25, 0)).toBeNull();
    expect(plannedBills([], '2026-10-01', 3)).toEqual([]);
  });
  it('uses the version effective for each cycle and rejects invalid previews', () => {
    const next = {
      ...rule,
      version: 2,
      effective_from: '2026-11-01',
      statement_day: 20,
      repayment_day: 25,
      repayment_month_offset: 0,
    };
    expect(plannedBills([next, rule], '2026-10-01', 2)).toEqual([
      { close: '2026-10-31', due: '2026-11-10' },
      { close: '2026-11-20', due: '2026-11-25' },
    ]);
    expect(ruleError(32, 10, 1)).toBeTruthy();
    expect(plannedBills([rule], '2026-02-31', 3)).toEqual([]);
    expect(plannedBills([rule], '1999-12-31', 3)).toEqual([]);
  });
});
