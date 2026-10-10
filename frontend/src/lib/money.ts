// Exact cents for financial previews. Money rendering remains shared with the app.
export function cents(value: string | undefined): bigint {
  if (!value || !/^-?\d+(\.\d{1,2})?$/.test(value)) return 0n;
  const negative = value.startsWith('-'); const [whole, part = ''] = value.replace('-', '').split('.');
  const result = BigInt(whole) * 100n + BigInt(part.padEnd(2, '0'));
  return negative ? -result : result;
}
export function money(value: bigint): string { const negative = value < 0n; const absolute = negative ? -value : value; return `${negative ? '-' : ''}${absolute / 100n}.${String(absolute % 100n).padStart(2, '0')}`; }
export function sumMoney(values: string[]): string { return money(values.reduce((sum, x) => sum + cents(x), 0n)); }
export function spreadMoney(value: string, count: number): string[] { if (!Number.isInteger(count) || count < 1 || count > 120) return []; const total = cents(value); const base = total / BigInt(count); return Array.from({ length: count }, (_, i) => money(i === count - 1 ? total - base * BigInt(count - 1) : base)); }
