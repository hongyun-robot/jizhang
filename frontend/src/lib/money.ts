export const MAX_AMOUNT_CENTS = 999999999999n;

export function parseCents(value: string | null | undefined): bigint | null {
  if (!value || !/^-?\d+(\.\d{1,2})?$/.test(value)) return null;
  const negative = value.startsWith('-');
  const [whole, fraction = ''] = (negative ? value.slice(1) : value).split('.');
  const result = BigInt(whole) * 100n + BigInt(fraction.padEnd(2, '0'));
  return negative ? -result : result;
}
export function cents(value: string | undefined): bigint {
  if (value === undefined || value === '') return 0n;
  const parsed = parseCents(value);
  if (parsed === null) throw new Error('金额格式无效，最多两位小数');
  return parsed;
}
/** Draft-only fallback; validation must run before submitting financial input. */
export function previewCents(value: string | undefined): bigint {
  return parseCents(value) ?? 0n;
}
export function money(value: bigint): string {
  const negative = value < 0n;
  const absolute = negative ? -value : value;
  return `${negative ? '-' : ''}${absolute / 100n}.${String(absolute % 100n).padStart(2, '0')}`;
}
export function formatMoney(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '0.00';
  if (typeof value === 'number')
    return Number.isFinite(value)
      ? value.toLocaleString('zh-CN', {
          minimumFractionDigits: 2,
          maximumFractionDigits: 2,
        })
      : String(value);
  const parsed = parseCents(value);
  if (parsed === null) return value;
  const [whole, part] = money(parsed).split('.');
  return whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + '.' + part;
}
export function sumMoney(values: string[]): string {
  return money(values.reduce((sum, value) => sum + cents(value), 0n));
}
export function previewSum(values: string[]): string {
  return values.every((value) => parseCents(value) !== null)
    ? sumMoney(values)
    : '—';
}
export function amountError(
  value: string,
  options: { zero?: boolean; negative?: boolean } = {}
): string | null {
  const parsed = parseCents(value);
  const absolute = parsed === null ? 0n : parsed < 0n ? -parsed : parsed;
  if (
    parsed === null ||
    absolute > MAX_AMOUNT_CENTS ||
    (!options.negative && parsed < 0n) ||
    (!options.zero && parsed === 0n)
  )
    return '请输入有效金额，最多两位小数且不超过 9999999999.99';
  return null;
}
export function spreadMoney(
  value: string,
  count: number,
  positive = false
): string[] {
  const total = parseCents(value);
  if (
    !Number.isInteger(count) ||
    count < 1 ||
    count > 120 ||
    total === null ||
    total < 0n ||
    total > MAX_AMOUNT_CENTS
  )
    return [];
  const base = total / BigInt(count);
  if (positive && base < 1n) return [];
  return Array.from({ length: count }, (_, index) =>
    money(index === count - 1 ? total - base * BigInt(count - 1) : base)
  );
}
