import { describe, expect, it } from 'vitest';
import { goldNumberValid, goldRatio, goldTotal } from './gold';
import { accountOptions } from './accounts';

describe('gold accounting inputs', () => {
  it('converts grams and unit cost with decimal rounding without float drift', () => {
    expect(goldTotal('10', '800')).toBe('8000.00');
    expect(goldRatio('8000', '10')).toBe('800');
    expect(goldTotal('0.01', '893.04')).toBe('8.93');
    expect(goldTotal('0.00000001', '0.00000001')).toBe('0.00');
    expect(goldRatio('8010', '10')).toBe('801');
  });
  it('rejects invalid quantities and money precision', () => {
    for (const value of ['NaN', 'Infinity', '-1', '1e3', '1.000000001', '']) expect(goldNumberValid(value)).toBe(false);
    expect(goldNumberValid('0')).toBe(false);
    expect(goldNumberValid('0', true, 2)).toBe(true);
    expect(goldNumberValid('1.001', false, 2)).toBe(false);
    expect(goldRatio('10', '0')).toBeNull();
  });
  it('gold is visible as an asset but excluded from ordinary payment', () => {
    const base = { name: '本人', type: 'asset' as const, sort: 0, status: 'active' as const, opening_balance: '0' };
    expect(accountOptions([{ ...base, id: 1, asset_class: 'cash' }, { ...base, id: 2, asset_class: 'gold', balance: null }])).toEqual([{ value: 1, label: '本人' }]);
  });
});
