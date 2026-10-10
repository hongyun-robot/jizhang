import dayjs from 'dayjs';
import type { BillRule } from '../api/liability';

export function ruleError(day?: number, due?: number, offset?: number): string | null {
  if (!day || !due || offset === undefined) return '请填写完整账单规则';
  if (offset === 0) {
    for (let month = 0; month < 12; month++) {
      const length = dayjs('2027-01-01').month(month).daysInMonth();
      if (Math.min(due, length) <= Math.min(day, length)) return '当月还款日必须在每个月都晚于账单日；月底组合请改为次月还款';
    }
  }
  return null;
}

export function plannedBills(rules: BillRule[], first: string, count: number): { close: string; due: string }[] {
  if (!first || !rules.length || count < 1 || count > 120) return [];
  const out: { close: string; due: string }[] = []; let cursor = dayjs(first);
  for (let i = 0; i < count; i++) {
    const date = cursor.format('YYYY-MM-DD'); const applicable = rules.filter((r) => r.effective_from <= date).sort((a, b) => b.version - a.version)[0];
    if (!applicable) return [];
    const closing = (month: typeof cursor) => month.startOf('month').date(Math.min(applicable.statement_day, month.daysInMonth()));
    let end = closing(cursor); if (cursor.isAfter(end, 'day')) end = closing(cursor.add(1, 'month'));
    const dueMonth = end.startOf('month').add(applicable.repayment_month_offset, 'month');
    const due = dueMonth.date(Math.min(applicable.repayment_day, dueMonth.daysInMonth()));
    out.push({ close: end.format('YYYY-MM-DD'), due: due.format('YYYY-MM-DD') }); cursor = end.add(1, 'day');
  }
  return out;
}
