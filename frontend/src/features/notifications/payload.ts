import type { NotifPayload } from '../../api/types';
import { parseRecord, text, numeric } from '../../lib/json';

export function parsePayload(raw: string): NotifPayload {
  const source = parseRecord(raw);
  const payload: NotifPayload = {};
  for (const key of [
    'dedup_key',
    'period_key',
    'category_name',
    'budget',
    'consumed',
    'over_amount',
    'account_name',
    'debt',
    'repayment_date',
    'initial_debt',
  ] as const) {
    const value = text(source[key]);
    if (value !== undefined) payload[key] = value;
  }
  for (const key of [
    'account_id',
    'statement_id',
    'days',
    'generated',
  ] as const) {
    const value = numeric(source[key]);
    if (value !== undefined) payload[key] = value;
  }
  if (source.scope === 'category' || source.scope === 'total')
    payload.scope = source.scope;
  if (typeof source.resolved === 'boolean') payload.resolved = source.resolved;
  if (typeof source.overdue === 'boolean') payload.overdue = source.overdue;
  return payload;
}
export function notifText(kind: string, payload: NotifPayload): string {
  switch (kind) {
    case 'budget_overdue':
      return `${payload.scope === 'category' ? `「${payload.category_name ?? '分类'}」` : '总预算'}已超支 ¥${payload.over_amount ?? '0.00'}（预算 ¥${payload.budget ?? '0'}，已用 ¥${payload.consumed ?? '0'}，账期 ${payload.period_key ?? ''}）`;
    case 'repayment_due': {
      if (payload.resolved)
        return `${payload.account_name ?? '信用账户'}账单已还清（原提醒金额 ¥${payload.initial_debt ?? payload.debt ?? '0.00'}）`;
      return `${payload.account_name ?? '信用卡'}欠款 ¥${payload.debt ?? '0.00'}，${payload.overdue ? `已过还款日 ${payload.repayment_date ?? '待核对'}` : `距还款日 ${payload.days ?? '—'} 天（${payload.repayment_date ?? '待核对'}）`}`;
    }
    case 'recurring_patched':
      return `已补记 ${payload.generated ?? 0} 笔周期流水，请到流水页核对`;
    default:
      return '请查看提醒记录';
  }
}
