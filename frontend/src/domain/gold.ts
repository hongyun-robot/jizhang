const scale = 100000000n;
function units(value: string): bigint | null {
  if (!/^\d+(?:\.\d{1,8})?$/.test(value)) return null;
  const [whole, fraction = ''] = value.split('.');
  return BigInt(whole) * scale + BigInt(fraction.padEnd(8, '0'));
}
function formatted(value: bigint, places: number) {
  const divisor = 10n ** BigInt(places);
  return `${value / divisor}.${(value % divisor).toString().padStart(places, '0')}`.replace(/\.?0+$/, '') || '0';
}
export function goldRatio(a: string, b: string): string | null {
  const x = units(a), y = units(b);
  if (x === null || y === null || y === 0n) return null;
  return formatted((x * scale + y / 2n) / y, 8);
}
export function goldTotal(grams: string, price: string): string | null {
  const g = units(grams), p = units(price);
  if (g === null || p === null) return null;
  const divisor = 100000000000000n;
  const cents = (g * p + divisor / 2n) / divisor;
  return `${cents / 100n}.${(cents % 100n).toString().padStart(2, '0')}`;
}
export const goldNumberValid = (value: string, zero = false, places = 8) => {
  const n = units(value);
  return n !== null && (zero ? n >= 0n : n > 0n) && (value.split('.')[1]?.length ?? 0) <= places && n <= 999999999999000000n;
};
