// Monetary suggestions use integers throughout. The bank confirmation is final.
function scaled(value: string | undefined, places: number): bigint | undefined {
  if (!value || !/^\d+(\.\d+)?$/.test(value)) return;
  const [whole = '', fraction = ''] = value.split('.');
  if (fraction.length > places || whole.length > 12) return;
  return BigInt(whole) * 10n ** BigInt(places) + BigInt(fraction.padEnd(places, '0'));
}
function rounded(numerator: bigint, denominator: bigint): bigint {
  return numerator / denominator + (numerator % denominator * 2n >= denominator ? 1n : 0n);
}
const decimal2 = (cents: bigint) => `${cents / 100n}.${String(cents % 100n).padStart(2, '0')}`;
export function suggestedWealthShares(amount: string | undefined, nav: string | undefined, fee = '0'): string | undefined {
  const cash = scaled(amount, 2), price = scaled(nav, 8), cost = scaled(fee, 2);
  if (cash == null || !price || cost == null || cash <= cost) return;
  return decimal2(rounded((cash - cost) * 100000000n, price));
}
export function wealthOpeningUnitCost(totalCost: string | undefined, shares: string | undefined): string | undefined {
  const cents = scaled(totalCost, 2), quantity = scaled(shares, 8);
  if (cents == null || !quantity) return;
  const value = rounded(cents * 100000000000000n, quantity);
  return `${value / 100000000n}.${String(value % 100000000n).padStart(8, '0')}`;
}
export function suggestedWealthProceeds(shares: string | undefined, nav: string | undefined, failedPrincipal = '0'): string | undefined {
  const quantity = scaled(shares, 8), price = scaled(nav, 8), principal = scaled(failedPrincipal, 2);
  if (quantity == null || !price || principal == null) return;
  return decimal2(rounded(quantity * price, 100000000000000n) + principal);
}
import type { WealthOrder } from '../api/wealth';


export function wealthOrderBalances(orders: WealthOrder[], shares: string, tradeDate?: string, previousDate?: string, nav?: string) {
  let current = 0n, previous = 0n, reservedShares = 0n;
  for (const order of orders) {
    if (order.status !== 'pending') continue;
    if (order.kind === 'request_redeem') { reservedShares += scaled(order.suggested_shares, 8) || 0n; continue; }
    const remaining = (scaled(order.amount, 2) || 0n) - (scaled(order.reserved_amount, 2) || 0n);
    if (order.data.trade_date === tradeDate) current += remaining;
    if (order.data.trade_date === previousDate) previous += remaining;
  }
  const available = (scaled(shares, 8) || 0n) - reservedShares;
  const positive = available < 0n ? 0n : available;
  const quantity = `${positive / 100000000n}.${String(positive % 100000000n).padStart(8, '0')}`;
  return {current: decimal2(current), previous: decimal2(previous), confirmedShares: quantity, confirmedAmount: suggestedWealthProceeds(quantity, nav)};
}
