import type { Voucher, VoucherAction } from '../../api/types';
import { cents, money } from '../../lib/money';
export const ACTION: Record<VoucherAction, string> = {
  purchase: '购券',
  redeem: '核销',
  refund: '退未用部分',
  writeoff: '作废',
};
export function stateLabel(v: Voucher): string {
  if (v.status === 'settled')
    return v.settlement === 'used'
      ? '已全部核销'
      : v.settlement === 'refunded'
        ? '已退款结清'
        : '已作废结清';
  return v.status === 'failed'
    ? '处理失败'
    : v.status === 'expired'
      ? '过期待处理'
      : '待使用';
}
export function autoLabel(v: Voucher): string {
  return !v.auto_refund_enabled
    ? '自动退款关闭'
    : v.auto_refund_suspended
      ? '自动退款已暂停'
      : '到期自动退款';
}
export function operationCost(v: Voucher, count: number): string {
  if (count === v.remaining_units) return v.remaining_amount;
  const fen =
    cents(v.next_unit_amount) *
    BigInt(Number.isSafeInteger(count) && count > 0 ? count : 0);
  return money(fen);
}
