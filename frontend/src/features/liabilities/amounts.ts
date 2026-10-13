import type { Allocation, CreditEvent, DebtItem } from '../../api/liability';
import type { Transaction } from '../../api/types';
import { money, cents, sumMoney } from '../../lib/money';

export interface BillItemRow {
  key: string;
  planId?: number;
  termId?: number;
  kind: string;
  amount: string;
  fee: string;
  remaining: string;
  remainingFee: string;
  sources: number[];
}

/** Derive display amounts from active debt items and allocations, never forecasts. */
export function liabilityAmounts(detail: {
  items: readonly DebtItem[];
  credits: readonly Pick<CreditEvent, 'id' | 'source_id'>[];
  transactions: readonly Pick<Transaction, 'id' | 'kind'>[];
  allocations: readonly Allocation[];
}) {
  const items = new Map(detail.items.map((item) => [item.id, item]));
  const credits = new Map(detail.credits.map((credit) => [credit.id, credit]));
  const transactions = new Map(detail.transactions.map((txn) => [txn.id, txn]));
  const refunds = new Map<number, bigint>();
  const paidFees = new Map<number, bigint>();
  const billPaidFees = new Map<number, bigint>();
  for (const allocation of detail.allocations) {
    const item = items.get(allocation.debt_item_id);
    const credit = credits.get(allocation.credit_event_id);
    if (!item || !credit) continue;
    if (transactions.get(credit.source_id)?.kind === 'refund') {
      refunds.set(item.id, (refunds.get(item.id) ?? 0n) + cents(allocation.amount));
    } else if (item.kind === 'fee') {
      paidFees.set(credit.id, (paidFees.get(credit.id) ?? 0n) + cents(allocation.amount));
      billPaidFees.set(item.statement_id, (billPaidFees.get(item.statement_id) ?? 0n) + cents(allocation.amount));
    }
  }
  const net = (item: DebtItem) => money(cents(item.amount) - (refunds.get(item.id) ?? 0n));
  const billItems = (billId: number): BillItemRow[] => {
    const rows = new Map<string, BillItemRow>();
    for (const item of detail.items) {
      if (item.statement_id !== billId) continue;
      const key = item.term_id ? `term:${item.term_id}` : `item:${item.id}`;
      let row = rows.get(key);
      if (!row) {
        row = { key, planId: item.plan_id ?? undefined, termId: item.term_id ?? undefined, kind: item.kind, amount: '0', remaining: '0', fee: '0', remainingFee: '0', sources: [] };
        rows.set(key, row);
      }
      if (item.kind === 'principal') row.kind = 'principal';
      row.amount = sumMoney([row.amount, net(item)]);
      row.remaining = sumMoney([row.remaining, item.remaining]);
      if (item.kind === 'fee') {
        row.fee = sumMoney([row.fee, net(item)]);
        row.remainingFee = sumMoney([row.remainingFee, item.remaining]);
      }
      if (!row.sources.includes(item.source_id)) row.sources.push(item.source_id);
    }
    return [...rows.values()];
  };
  return {
    billItems,
    billFee: (billId: number) => sumMoney(detail.items.filter((item) => item.statement_id === billId && item.kind === 'fee').map(net)),
    billRemainingFee: (billId: number) => sumMoney(detail.items.filter((item) => item.statement_id === billId && item.kind === 'fee').map((item) => item.remaining)),
    billPaidFee: (billId: number) => money(billPaidFees.get(billId) ?? 0n),
    paymentFee: (creditId: number) => money(paidFees.get(creditId) ?? 0n),
    termRemainingFee: (termId: number, feeTxnId: number | null) => sumMoney(detail.items.filter((item) => item.kind === 'fee' && (item.term_id === termId || (!!feeTxnId && item.source_id === feeTxnId))).map((item) => item.remaining)),
  };
}
