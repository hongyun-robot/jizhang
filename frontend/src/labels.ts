import type { TxnPurpose } from './api/types';

/** 角色中文标签：侧栏用户位 / 首页问候 / 我的页共用，避免三处各写一份。 */
export const ROLE_LABEL: Record<string, string> = {
  admin: '管理员',
  recorder: '记账员',
  member: '成员',
  viewer: '只读',
};

export const VOUCHER_PURPOSE_LABEL: Partial<Record<TxnPurpose, string>> = {
  voucher_purchase: '购券',
  voucher_redemption: '券核销',
  voucher_refund: '券退款',
  voucher_writeoff: '券作废',
  fund_purchase: '基金买入付款',
  fund_redemption: '基金赎回到账',
  fund_dividend: '基金分红到账',
  gold_purchase: '建行积存金买入',
  gold_redemption: '建行积存金卖出',
};
