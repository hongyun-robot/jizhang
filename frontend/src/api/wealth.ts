import { useMutation, useQuery } from '@tanstack/react-query';
import { readApi, writeApi } from './request';
import { queryKeys, useScope } from './keys';
import { useInvalidation } from './cache';
import { requestJson } from './request';
import type { Account } from './types';

export type WealthOrderKind = 'subscribe' | 'confirm_buy' | 'cancel_buy' | 'refund_buy' | 'request_redeem' | 'confirm_redeem' | 'cancel_redeem';
export type WealthEventKind = 'opening' | 'buy' | 'redeem' | 'cash_dividend' | 'reinvest' | 'settlement' | WealthOrderKind;
export interface WealthAllocation { source_id: number; amount: string; shares: string }
export interface WealthOrderData { trade_date?: string; nav_date?: string; opening_nav?: string; expected_date?: string; application_time?: string; previous_trade_date?: string; reason?: string; nav_source?: string; allocations?: WealthAllocation[]; failed_principal?: string; insufficient_policy?: 'cancel' | 'all'; cancelled_for_insufficient?: boolean; allocated_amount?: string }
export interface WealthOrder {event_id: number; kind: 'subscribe' | 'request_redeem'; status: string; amount: string; refund_due: string; reserved_amount: string; suggested_shares: string; ready: boolean; data: WealthOrderData}
export interface WealthPosition {
  id: number; owner_id: number; account_id: number; product_id: number;
  code: string; ticker: string; name: string; source_name: string; display_name: string; valuation_basis: string;
  order_policy?: 'confirmed_facts' | 'zhoulibao_20250402';
  shares: string; cost: string; cost_price: string; cost_estimated: boolean;
  market_value: string | null; unit_nav: string | null; nav_date: string; effective_date: string; pending_funds: string;
  holding_pnl: string | null; holding_return: string | null; realized_pnl: string; dividends: string; fees: string; total_pnl: string | null;
  daily_pnl: string | null; daily_start: string; daily_end: string; daily_status: string; valuation_status: string;
  source_status: string; quote: WealthQuote | null; product: WealthProduct; series_version: number; rebuild_pending: boolean; source_error?: string; can_amend: boolean;
  orders: WealthOrder[];
  opening_nav?: string; opening_nav_date?: string;
}
export interface WealthAccount {
  account: Account; positions: WealthPosition[]; market_value: string | null; pending_funds: string;
  total_value: string | null; total_pnl: string | null; complete: boolean; configured: boolean; known_subtotal: string;
}
export interface WealthProduct {id: number; issuer: string; registration_code: string; code: string; name: string; currency: string; admission_status: string; inception_date: string; sync_status: string; last_error?: string; last_attempt?: string; last_success?: string; next_attempt?: string}
export interface WealthQuote {product_id: number; nav_date: string; unit_nav: string; total_nav: string | null; source: 'automatic' | 'manual'; record_id: number; revision: number}
export interface WealthManual {id: number; product_id: number; nav_date: string; unit_nav: string; revision: number; source_note: string; withdrawn: boolean; automatic_replaced: boolean; effective: boolean}
export interface WealthManualInput {request_key: string; nav_date: string; unit_nav?: string; source_note: string; expected_revision: number; withdraw: boolean}
export interface WealthInput {
  request_key: string; kind?: WealthEventKind; code?: string; display_name?: string;
  shares?: string; cost_price?: string; total_cost?: string; opening_nav?: string; amount?: string; fee?: string; trade_nav?: string;
  effective_date: string; cash_date?: string; rights_date?: string; registration_date?: string;
  cash_account_id?: number; source_event_id?: number; note?: string;
  trade_date?: string; nav_date?: string; expected_date?: string; previous_trade_date?: string; adjustment_reason?: string; application_time?: string;
  insufficient_policy?: 'cancel' | 'all';
}
export interface WealthReceipt { account_id: number; position_id: number; event_id: number }
export interface WealthEvent {
  id: number; position_id: number; kind: WealthEventKind; shares: string; amount: string; fee: string; trade_nav: string;
  effective_date: string; rights_date?: string; cash_date?: string; cash_account_id?: number; source_event_id?: number;
  created_by: number; voided_at?: string; note: string;
  order_json?: string;
}
export interface WealthValuation { nav_date: string; previous_nav_date?: string; shares: string; cost: string; market_value: string; daily_pnl: string | null; status: string; quote_revision: number; event_version: number }
export interface WealthAudit { id: number; action: string; actor_id: number; before: string; after: string; created_at: string }

function useWealthQuery<T>(parts: unknown[], path: string, enabled = true) {
  const scope = useScope();
  return useQuery({ queryKey: queryKeys.key('wealth', scope, ...parts), queryFn: ({ signal }) => readApi<T>(path, signal), enabled, refetchInterval: 60_000, refetchOnWindowFocus: true });
}

export interface WealthCashLink { id: number; account_id: number; product_id: number; policy: string; start_date: string; active: boolean; revision: number; sync_status: string; last_error?: string; last_reconciled_at?: string }
export interface WealthCashYield { id: number; account_id: number; nav_date: string; previous_nav_date: string; unit_nav: string; previous_nav: string; amount: string; calculated_amount: string; manual_override: boolean; manual_start_date?: string; txn_id: number | null; revision: number; status: string; note: string }
export interface WealthCashAccount { account: Account; link: WealthCashLink | null; product: WealthProduct | null; yields: WealthCashYield[]; audits: WealthAudit[]; unit_nav: string | null; nav_date: string; estimated_shares: string | null; confirmed_amount: string; pending_amount: string; uninvested_amount: string; total_yield: string }
export interface WealthCashInput { request_key: string; expected_revision: number; product_id: number; start_date: string; active: boolean; policy?: string }
export interface WealthCashYieldInput { request_key: string; expected_revision: number; amount?: string; balance?: string; note: string }
export const useWealthCashAccounts = () => useWealthQuery<WealthCashAccount[]>(['cash-accounts'], '/wealth/cash-accounts');
export const useWealthCashAccount = (id: number) => useWealthQuery<WealthCashAccount>(['cash-account', id], `/wealth/cash-accounts/${id}`, id > 0);
export const useWealthCashYield = (id: number) => useWealthQuery<WealthCashYield>(['cash-yield', id], `/wealth/cash-yields/${id}`, id > 0);
export function useWealthCashMutations() {
  const done = useInvalidation('wealth');
  const configure = useMutation({ mutationFn: ({ id, input }: { id: number; input: WealthCashInput }) => writeApi('put', `/wealth/cash-accounts/${id}`, input), onSuccess: done });
  const edit = useMutation({ mutationFn: ({ id, input }: { id: number; input: WealthCashYieldInput }) => writeApi('put', `/wealth/cash-yields/${id}`, input), onSuccess: done });
  const refresh = useMutation({ mutationFn: (id: number) => requestJson<WealthCashAccount>(`/wealth/cash-accounts/${id}/refresh`, { method: 'post', timeout: 70_000 }), onSuccess: done });
  return { configure, edit, refresh };
}
export const useWealthAccounts = () => useWealthQuery<WealthAccount[]>(['accounts'], '/wealth/accounts');
export const useWealthAccount = (id: number) => useWealthQuery<WealthAccount>(['account', id], `/wealth/accounts/${id}`, id > 0);
export const useWealthPosition = (id: number) => useWealthQuery<WealthPosition>(['position', id], `/wealth/positions/${id}`, id > 0);
export const useWealthEvents = (id: number) => useWealthQuery<WealthEvent[]>(['events', id], `/wealth/positions/${id}/events`, id > 0);
export const useWealthValuations = (id: number) => useWealthQuery<WealthValuation[]>(['valuations', id], `/wealth/positions/${id}/valuations`, id > 0);
export const useWealthAudits = (id: number) => useWealthQuery<WealthAudit[]>(['audits', id], `/wealth/positions/${id}/audits`, id > 0);
export const useWealthProducts = (enabled = true) => useWealthQuery<WealthProduct[]>(['products'], '/wealth/products', enabled);
export const useWealthQuotes = (id: number) => useWealthQuery<WealthQuote[]>(['quotes', id], `/wealth/products/${id}/navs`, id > 0);
export const useWealthManuals = (id: number) => useWealthQuery<WealthManual[]>(['manuals', id], `/wealth/products/${id}/manual-navs`, id > 0);
export const useWealthManualAudits = (id: number) => useWealthQuery<WealthAudit[]>(['manual-audits', id], `/wealth/products/${id}/manual-audits`, id > 0);
export function useWealthMutations() {
  const done = useInvalidation('wealth');
  const save = useMutation({ mutationFn: ({ path, input, method = 'post' }: { path: string; input: WealthInput | { request_key: string }; method?: 'post' | 'put' }) => writeApi<WealthReceipt>(method, path, input), onSuccess: done });
  const refresh = useMutation({ mutationFn: (id: number) => requestJson<WealthAccount>(`/wealth/accounts/${id}/refresh`, { method: 'post', timeout: 70_000 }), onSuccess: done });
  const manual = useMutation({mutationFn: ({productId, input}: {productId: number; input: WealthManualInput}) => writeApi('post', `/wealth/products/${productId}/manual-navs`, input), onSuccess: done});
  const refreshProduct = useMutation({mutationFn: (id: number) => requestJson<WealthProduct>(`/wealth/products/${id}/refresh`, { method: 'post', timeout: 70_000 }), onSuccess: done});
  return { save, refresh, manual, refreshProduct };
}
