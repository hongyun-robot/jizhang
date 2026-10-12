import dayjs, { type Dayjs } from 'dayjs';
import type { DateRange } from '../api/types';

export type PresetKey =
  | 'thisMonth'
  | 'lastMonth'
  | 'last7'
  | 'last30'
  | 'custom';
export function validDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const [year, month, day] = value.split('-').map(Number);
  const date = new Date(0);
  date.setUTCHours(0, 0, 0, 0);
  date.setUTCFullYear(year, month - 1, day);
  return (
    year > 0 &&
    date.getUTCFullYear() === year &&
    date.getUTCMonth() === month - 1 &&
    date.getUTCDate() === day
  );
}
export function validRange(range: DateRange): boolean {
  return (
    validDate(range.start) && validDate(range.end) && range.start <= range.end
  );
}
export function presetRange(
  key: Exclude<PresetKey, 'custom'>,
  reference: Dayjs = dayjs()
): DateRange {
  const end = (date: Dayjs) => date.format('YYYY-MM-DD');
  if (key === 'thisMonth' || key === 'lastMonth') {
    const month =
      key === 'lastMonth' ? reference.subtract(1, 'month') : reference;
    return {
      start: end(month.startOf('month')),
      end: end(month.endOf('month')),
    };
  }
  return {
    start: end(reference.subtract(key === 'last7' ? 6 : 29, 'day')),
    end: end(reference),
  };
}
export function matchPreset(
  value: DateRange,
  reference: Dayjs = dayjs()
): PresetKey {
  for (const key of ['thisMonth', 'lastMonth', 'last7', 'last30'] as const) {
    const range = presetRange(key, reference);
    if (range.start === value.start && range.end === value.end) return key;
  }
  return 'custom';
}
