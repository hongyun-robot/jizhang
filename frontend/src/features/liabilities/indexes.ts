import type { LiabilityDetail } from '../../api/liability';
import { liabilityAmounts } from './amounts';

function grouped<T>(items: T[], key: (item: T) => number): Map<number, T[]> {
  const result = new Map<number, T[]>();
  for (const item of items) {
    const value = key(item),
      list = result.get(value) ?? [];
    list.push(item);
    result.set(value, list);
  }
  return result;
}
export function liabilityIndexes(detail: LiabilityDetail) {
  return {
    amounts: liabilityAmounts(detail),
    terms: new Map(detail.plans.flatMap((plan) => plan.terms.map((term) => [term.id, term]))),
    bills: new Map(detail.statements.map((bill) => [bill.id, bill])),
    txns: new Map(
      detail.transactions.map((transaction) => [transaction.id, transaction])
    ),
    items: new Map(detail.items.map((item) => [item.id, item])),
    itemsByBill: grouped(detail.items, (item) => item.statement_id),
    itemsBySource: grouped(detail.items, (item) => item.source_id),
    allocationsByCredit: grouped(
      detail.allocations,
      (allocation) => allocation.credit_event_id
    ),
  };
}
