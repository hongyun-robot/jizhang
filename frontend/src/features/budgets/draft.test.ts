import { describe, expect, it } from 'vitest';
import { receiveBudget, savedBudget, type BudgetDraft } from './draft';

const dirty: BudgetDraft = {
  key: '2026-10',
  fields: { total: '9000' },
  dirty: true,
};
describe('budget edit lifecycle', () => {
  it('retains dirty fields on remote refresh', () => {
    expect(receiveBudget(dirty, '2026-10', { total: '8000' })).toBe(dirty);
  });
  it('uses server values after saving the same period', () => {
    expect(savedBudget(dirty, '2026-10', { total: '9000' })).toEqual({
      key: '2026-10',
      fields: { total: '9000' },
      dirty: false,
    });
  });
  it('initializes an explicitly selected new period without copying the previous draft', () => {
    expect(receiveBudget(dirty, '2026-11', { total: '5000' })).toEqual({
      key: '2026-11',
      fields: { total: '5000' },
      dirty: false,
    });
  });
  it('ignores an old save response for a different editor', () => {
    expect(savedBudget(dirty, '2026-09', { total: '1000' })).toBe(dirty);
  });
});
