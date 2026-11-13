import type { DebtItem, LiabilityBill, LiabilityDetail } from '../../api/liability';
import type { Transaction } from '../../api/types';
type OrdinaryData = {
  account: { id: number };
  statements: Pick<LiabilityBill, 'id' | 'kind' | 'issued_at' | 'due_date' | 'cycle_end'>[];
  items: DebtItem[];
  transactions: Pick<Transaction, 'id' | 'account_id' | 'kind' | 'entry_date' | 'installment_plan_id' | 'liability_term_id' | 'liability_operation_id' | 'purpose' | 'balance_effect'>[];
};
import { previewCents as cents, money } from '../../lib/money';

/** Initial draft hints only; the server recomputes eligibility on preview/save. */
export function ordinaryCapacity(detail: OrdinaryData, date: string) {
  const sources = new Map(detail.transactions.map((t) => [t.id, t]));
  const bills = new Map(detail.statements.filter((b) => !b.issued_at && b.kind === 'monthly').map((b) => [b.id, b]));
  const amounts = new Map<number, bigint>();
  for (const item of detail.items) {
    const t = sources.get(item.source_id);
    if (!t || !bills.has(item.statement_id) || item.kind !== 'principal' || item.plan_id || item.term_id ||
        item.source_txn_id !== t.id || t.kind !== 'expense' || t.account_id !== detail.account.id ||
        t.installment_plan_id || t.liability_term_id || t.liability_operation_id ||
        (!!t.purpose && t.purpose !== 'regular' && t.purpose !== 'voucher_purchase') ||
        t.entry_date.slice(0, 10) > date || cents(item.remaining) <= 0n) continue;
    amounts.set(item.statement_id, (amounts.get(item.statement_id) ?? 0n) + cents(item.remaining));
  }
  return {
    amounts,
    total: money([...amounts.values()].reduce((sum, amount) => sum + amount, 0n)),
    bills: [...bills.values()].filter((b) => (amounts.get(b.id) ?? 0n) > 0n)
      .sort((a, b) => a.due_date.localeCompare(b.due_date) || a.cycle_end.localeCompare(b.cycle_end) || a.id - b.id),
  };
}

/** Draft hint; the server validates dates, term fees and the final allocation. */
export function combinedCapacity(detail: Pick<LiabilityDetail, 'items' | 'transactions' | 'plans'>, date: string) {
  const sources = new Map(detail.transactions.map((source) => [source.id, source]));
  const occurred = (id: number) => !!sources.get(id) && sources.get(id)!.entry_date.slice(0, 10) <= date;
  let total = detail.items.filter((item) => !item.term_id && !item.plan_id && occurred(item.source_id))
    .reduce((sum, item) => sum + cents(item.remaining), 0n);
  for (const plan of detail.plans) {
    if (!occurred(plan.source_txn_id)) continue;
    for (const term of plan.terms) {
      total += cents(term.remaining);
      total += detail.items.filter((item) => item.term_id === term.id && item.kind === 'fee')
        .reduce((sum, item) => sum + cents(item.remaining), 0n);
      if (!term.fee_txn_id && term.fee_status !== 'cancelled') total += cents(term.fee_amount);
    }
  }
  return money(total);
}
