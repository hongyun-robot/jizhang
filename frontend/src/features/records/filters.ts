import type { DateRange, TxnKind, TxnListParams } from '../../api/types';
import { validRange } from '../../lib/dates';
import { parseCents } from '../../lib/money';

export interface FilterState {
  range: DateRange;
  kind: TxnKind | '';
  account_id?: number;
  category_id?: number;
  amount_min?: string;
  amount_max?: string;
  keyword: string;
  related_txn_id?: number;
}
export const EMPTY_FILTERS = {
  kind: '' as const,
  account_id: undefined,
  category_id: undefined,
  amount_min: undefined,
  amount_max: undefined,
  keyword: '',
  related_txn_id: undefined,
};
const kinds: TxnKind[] = ['expense', 'income', 'transfer', 'refund'];
function positiveId(value: string | null): number | undefined {
  const parsed = Number(value);
  return value && Number.isSafeInteger(parsed) && parsed > 0
    ? parsed
    : undefined;
}
function amount(value: string | null): string | undefined {
  const parsed = parseCents(value);
  return parsed !== null && parsed >= 0n ? (value ?? undefined) : undefined;
}
export function parseFilters(
  search: URLSearchParams,
  fallback: DateRange
): FilterState {
  const range = {
    start: search.get('from') ?? '',
    end: search.get('to') ?? '',
  };
  const kind = search.get('kind');
  let min = amount(search.get('amount_min')),
    max = amount(search.get('amount_max'));
  if (min && max && parseCents(min)! > parseCents(max)!) {
    min = undefined;
    max = undefined;
  }
  return {
    range: validRange(range) ? range : fallback,
    kind: kinds.find((value) => value === kind) ?? '',
    account_id: positiveId(search.get('account_id')),
    category_id: positiveId(search.get('category_id')),
    amount_min: min,
    amount_max: max,
    keyword: search.get('keyword') ?? '',
    related_txn_id: positiveId(search.get('related_txn_id')),
  };
}
export function parsePage(search: URLSearchParams): number {
  return positiveId(search.get('page')) ?? 1;
}
export function serializeFilters(
  filters: FilterState,
  page: number,
  fallback: DateRange
): URLSearchParams {
  const search = new URLSearchParams();
  if (
    filters.range.start !== fallback.start ||
    filters.range.end !== fallback.end
  ) {
    search.set('from', filters.range.start);
    search.set('to', filters.range.end);
  }
  for (const [key, value] of Object.entries(filters)) {
    if (key === 'range' || value === undefined || value === '') continue;
    search.set(key, String(value));
  }
  if (page > 1) search.set('page', String(page));
  return search;
}
export function listParams(
  filters: FilterState,
  page: number,
  size = 20
): TxnListParams {
  return {
    start_date: filters.related_txn_id ? undefined : filters.range.start,
    end_date: filters.related_txn_id ? undefined : filters.range.end,
    kind: filters.kind || undefined,
    account_id: filters.account_id,
    category_id: filters.category_id,
    amount_min: filters.amount_min,
    amount_max: filters.amount_max,
    keyword: filters.keyword || undefined,
    related_txn_id: filters.related_txn_id,
    page,
    page_size: size,
  };
}
export function txnQueryParams(params: TxnListParams, exporting = false) {
  return {
    start_date: params.related_txn_id ? undefined : params.start_date,
    end_date: params.related_txn_id ? undefined : params.end_date,
    period_key: params.related_txn_id ? undefined : params.period_key,
    kind: params.kind || undefined,
    account_id: params.account_id,
    category_id: params.category_id,
    amount_min: params.amount_min,
    amount_max: params.amount_max,
    keyword: params.keyword,
    related_txn_id: params.related_txn_id,
    page: exporting ? undefined : params.page,
    page_size: exporting ? undefined : params.page_size,
  };
}

const KIND_LABEL: Record<TxnKind, string> = {
  expense: '支出',
  income: '收入',
  transfer: '转账',
  refund: '退款',
};
export function summarizeFilters(
  filters: FilterState,
  fallback: DateRange,
  accounts: ReadonlyMap<number, string>,
  categories: ReadonlyMap<number, { full: string }>
) {
  const parts: string[] = [
    `${filters.range.start.replace(/-/g, '.')} – ${filters.range.end.replace(/-/g, '.')}`,
  ];
  let count =
    filters.range.start !== fallback.start || filters.range.end !== fallback.end
      ? 1
      : 0;
  if (filters.kind) {
    parts.push(KIND_LABEL[filters.kind]);
    count++;
  }
  if (filters.account_id) {
    parts.push(
      accounts.get(filters.account_id) ?? `账户#${filters.account_id}`
    );
    count++;
  }
  if (filters.category_id) {
    parts.push(
      categories.get(filters.category_id)?.full ?? `分类#${filters.category_id}`
    );
    count++;
  }
  if (filters.keyword) {
    parts.push(filters.keyword);
    count++;
  }
  if (filters.amount_min) {
    parts.push(`≥${filters.amount_min}`);
    count++;
  }
  if (filters.amount_max) {
    parts.push(`≤${filters.amount_max}`);
    count++;
  }
  if (filters.related_txn_id) {
    parts.push(`退款#${filters.related_txn_id}`);
    count++;
  }
  return { summary: parts.join(' · '), count };
}
