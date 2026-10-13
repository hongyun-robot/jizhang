import { money } from '../lib/money';

// Stored opening cost keeps quantity × unit-cost precision. Round its display
// to currency cents without converting the intermediate decimal to a float.
export function fundMoney(value: string): string {
  const parsed = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!parsed) return value;
  const [, sign, whole, part = ''] = parsed;
  let result = BigInt(whole) * 100n + BigInt(part.padEnd(2, '0').slice(0, 2));
  if (part.length > 2 && part[2] >= '5') result++;
  return money(sign ? -result : result);
}
