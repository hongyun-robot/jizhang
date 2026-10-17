import { money } from '../lib/money';

export function wealthToday(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now);
  const value = (type: string) => parts.find((part) => part.type === type)?.value;
  return `${value('year')}-${value('month')}-${value('day')}`;
}

// Stored opening cost keeps quantity × unit-cost precision. Round its display
// to currency cents without converting the intermediate decimal to a float.
export function wealthMoney(value: string): string {
  const parsed = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!parsed) return value;
  const [, sign, whole, part = ''] = parsed;
  let result = BigInt(whole) * 100n + BigInt(part.padEnd(2, '0').slice(0, 2));
  if (part.length > 2 && part[2] >= '5') result++;
  return money(sign ? -result : result);
}
