import { expect, it } from 'vitest';
import { wealthToday } from './wealth';

it('defaults wealth dates to the Shanghai calendar across UTC midnight boundaries', () => {
  expect(wealthToday(new Date('2026-10-15T15:59:59Z'))).toBe('2026-10-15');
  expect(wealthToday(new Date('2026-10-15T16:00:00Z'))).toBe('2026-10-16');
});
