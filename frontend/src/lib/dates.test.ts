import dayjs from 'dayjs';
import { describe, expect, it } from 'vitest';
import { validDate, validRange, presetRange } from './dates';

describe('date-only ranges', () => {
  it('validates actual calendar dates without rolling invalid days forward', () => {
    expect(validDate('2024-02-29')).toBe(true);
    for (const date of [
      '2023-02-29',
      '2026-02-31',
      '2026-13-01',
      '2026-00-01',
      '2026-01-00',
      '2026-1-01',
    ])
      expect(validDate(date)).toBe(false);
    expect(validRange({ start: '2026-10-02', end: '2026-10-01' })).toBe(false);
  });
  it('uses an explicit reference for presets and includes both endpoints', () => {
    const reference = dayjs('2024-03-03');
    expect(presetRange('lastMonth', reference)).toEqual({
      start: '2024-02-01',
      end: '2024-02-29',
    });
    expect(presetRange('last7', reference)).toEqual({
      start: '2024-02-26',
      end: '2024-03-03',
    });
  });
});
