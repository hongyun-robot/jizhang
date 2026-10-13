import { isRecord, numeric, parseRecord, records, text } from '../../lib/json';
import { parseCents } from '../../lib/money';

export interface HistoryTerm {
  id: number;
  number: number;
  principal: string;
  fee_amount: string;
  fee_date: string;
  fee_status?: string;
  status?: string;
}
export interface HistoryPlan {
  id: number;
  version: number;
  terms: HistoryTerm[];
}
function historyPlan(value: unknown): HistoryPlan | undefined {
  if (
    !isRecord(value) ||
    numeric(value.id) === undefined ||
    numeric(value.version) === undefined
  )
    return undefined;
  const id = numeric(value.id)!,
    version = numeric(value.version)!;
  const terms: HistoryTerm[] = [];
  for (const term of records(value.terms)) {
    const termId = numeric(term.id),
      number = numeric(term.number),
      principal = text(term.principal),
      fee = text(term.fee_amount),
      date = text(term.fee_date);
    if (
      termId === undefined ||
      number === undefined ||
      principal === undefined ||
      fee === undefined ||
      date === undefined ||
      parseCents(principal) === null ||
      parseCents(fee) === null
    )
      continue;
    terms.push({
      id: termId,
      number,
      principal,
      fee_amount: fee,
      fee_date: date,
      fee_status: text(term.fee_status),
      status: text(term.status),
    });
  }
  return { id, version, terms };
}
export function historySnapshot(raw: string) {
  const source = parseRecord(raw);
  const amount = text(source.amount);
  const rule = isRecord(source.rule) ? source.rule : {};
  const statement = numeric(rule.statement_day),
    repayment = numeric(rule.repayment_day),
    offset = numeric(rule.repayment_month_offset),
    effective = text(rule.effective_from);
  const statements = records(source.statements).flatMap((item) => {
    const id = numeric(item.id),
      close = text(item.cycle_end),
      due = text(item.due_date);
    return id === undefined || close === undefined || due === undefined
      ? []
      : [{ id, cycle_end: close, due_date: due }];
  });
  const assignments = records(source.assignments).flatMap((item) => {
    const id = numeric(item.item_id),
      sourceId = numeric(item.source_id),
      from = numeric(item.from_statement_id),
      to = numeric(item.to_statement_id),
      value = text(item.amount);
    return id === undefined ||
      sourceId === undefined ||
      from === undefined ||
      to === undefined ||
      value === undefined ||
      parseCents(value) === null
      ? []
      : [
          {
            item_id: id,
            source_id: sourceId,
            from_statement_id: from,
            to_statement_id: to,
            amount: value,
          },
        ];
  });
  return {
    amount: amount && parseCents(amount) !== null ? amount : undefined,
    entry_date: text(source.entry_date),
    rule:
      statement === undefined ||
      repayment === undefined ||
      offset === undefined ||
      effective === undefined
        ? undefined
        : {
            statement_day: statement,
            repayment_day: repayment,
            repayment_month_offset: offset,
            effective_from: effective,
          },
    statements,
    assignments,
    plan_snapshot: historyPlan(source.plan_snapshot),
    plan_snapshots: records(source.plan_snapshots).flatMap((item) => {
      const plan = historyPlan(item);
      return plan ? [plan] : [];
    }),
    term_ids: Array.isArray(source.term_ids) ? source.term_ids.flatMap((value) => {
      const id = numeric(value);
      return id === undefined ? [] : [id];
    }) : [],
    transactions: records(source.transaction_snapshots).flatMap((item) => {
      const id = numeric(item.id), value = text(item.amount), kind = text(item.kind), date = text(item.entry_date);
      return id === undefined || value === undefined || parseCents(value) === null || (kind !== 'expense' && kind !== 'transfer') || date === undefined
        ? [] : [{ id, amount: value, kind, entry_date: date }];
    }),
    plans: records(source.plans).flatMap((item) => {
      const plan = historyPlan(item);
      return plan ? [plan] : [];
    }),
  };
}
