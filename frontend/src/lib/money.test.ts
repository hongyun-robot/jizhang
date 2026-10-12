import { describe, expect, it } from 'vitest';
import {
  amountError,
  cents,
  money,
  parseCents,
  formatMoney,
  spreadMoney,
  sumMoney,
} from './money';

describe('financial cents', () => {
  it('rejects malformed input explicitly and keeps write bounds separate from large totals', () => {
    for (const value of ['NaN', '1.001', '1..2', '.', ''])
      expect(parseCents(value)).toBeNull();
    expect(() => cents('1..2')).toThrow('金额格式');
    expect(amountError('9999999999.99')).toBeNull();
    expect(amountError('10000000000')).toBeTruthy();
    expect(amountError('0')).toBeTruthy();
    expect(amountError('0', { zero: true })).toBeNull();
    expect(amountError('-1', { negative: true })).toBeNull();
    expect(formatMoney('9007199254740993.01')).toBe('9,007,199,254,740,993.01');
    expect(spreadMoney('0.01', 2, true)).toEqual([]);
    expect(spreadMoney('0.01', 2)).toEqual(['0.00', '0.01']);
  });
  it('keeps positive and negative decimal cents exact', () => {
    expect(cents('12.3')).toBe(1230n);
    expect(cents('-0.01')).toBe(-1n);
    expect(money(-1n)).toBe('-0.01');
    expect(sumMoney(['0.10', '0.20', '-0.01'])).toBe('0.29');
  });
  it('puts the remainder in the last installment', () => {
    expect(spreadMoney('10', 3)).toEqual(['3.33', '3.33', '3.34']);
    expect(sumMoney(spreadMoney('100', 120))).toBe('100.00');
    expect(spreadMoney('100', 121)).toEqual([]);
  });
});
