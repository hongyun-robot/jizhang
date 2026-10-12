import type { Transaction, TxnKind } from '../../api/types';
export const kindTag = (kind: TxnKind) =>
  ({ expense: '支出', income: '收入', refund: '退款', transfer: '转账' })[kind];
export const tone = (kind: TxnKind): 'expense' | 'income' | 'plain' =>
  kind === 'expense'
    ? 'expense'
    : kind === 'income' || kind === 'refund'
      ? 'income'
      : 'plain';
export const amountOf = (transaction: Transaction): string =>
  transaction.kind === 'income' || transaction.kind === 'refund'
    ? transaction.amount
    : `-${transaction.amount}`;
