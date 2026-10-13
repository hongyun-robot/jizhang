import { useMutation, useQuery } from '@tanstack/react-query';
import { readApi, writeApi } from './request';
import { queryKeys, useScope } from './keys';
import { useInvalidation } from './cache';
import type { Account } from './types';

export type FundEventKind = 'opening' | 'buy' | 'redeem' | 'cash_dividend' | 'reinvest' | 'settlement';
export interface FundPosition {
  id: number; owner_id: number; account_id: number; instrument_id: number;
  code: string; ticker: string; name: string; source_name: string; display_name: string; valuation_basis: string;
  shares: string; cost: string; cost_price: string; cost_estimated: boolean;
  market_value: string | null; unit_nav: string | null; nav_date: string; effective_date: string; pending_funds: string;
  holding_pnl: string | null; holding_return: string | null; realized_pnl: string; dividends: string; fees: string; total_pnl: string | null;
  daily_pnl: string | null; daily_start: string; daily_end: string; daily_status: string; valuation_status: string;
  source_status: string; source_error?: string; can_amend: boolean;
}
export interface FundAccount {
  account: Account; positions: FundPosition[]; market_value: string | null; pending_funds: string;
  total_value: string | null; total_pnl: string | null; complete: boolean; configured: boolean;
}
export interface FundInstrument { thscode: string; ticker: string; name: string; asset_type: string; currency: string }
export interface FundInput {
  request_key: string; kind?: FundEventKind; code?: string; display_name?: string; valuation_basis?: string; basis_confirmed?: boolean;
  shares?: string; cost_price?: string; amount?: string; fee?: string; trade_nav?: string;
  effective_date: string; cash_date?: string; rights_date?: string; registration_date?: string;
  cash_account_id?: number; source_event_id?: number; note?: string;
}
export interface FundReceipt { account_id: number; position_id: number; event_id: number }
export interface FundEvent {
  id: number; position_id: number; kind: FundEventKind; shares: string; amount: string; fee: string; trade_nav: string;
  effective_date: string; rights_date?: string; cash_date?: string; cash_account_id?: number; source_event_id?: number;
  created_by: number; voided_at?: string; note: string;
}
export interface FundValuation { nav_date: string; previous_nav_date?: string; shares: string; cost: string; market_value: string; daily_pnl: string | null; status: string; quote_revision: number; event_version: number }
export interface FundDividend { source_key: string; registration_date: string; ex_date: string; payment_date?: string; per_ten_cash: string }
export interface FundAudit { id: number; action: string; actor_id: number; before: string; after: string; created_at: string }

function useFundQuery<T>(parts: unknown[], path: string, enabled = true) {
  const scope = useScope();
  return useQuery({ queryKey: queryKeys.key('funds', scope, ...parts), queryFn: ({ signal }) => readApi<T>(path, signal), enabled, refetchInterval: 60_000, refetchOnWindowFocus: true });
}
export const useFundAccounts = () => useFundQuery<FundAccount[]>(['accounts'], '/funds/accounts');
export const useFundAccount = (id: number) => useFundQuery<FundAccount>(['account', id], `/funds/accounts/${id}`, id > 0);
export const useFundPosition = (id: number) => useFundQuery<FundPosition>(['position', id], `/funds/positions/${id}`, id > 0);
export const useFundEvents = (id: number) => useFundQuery<FundEvent[]>(['events', id], `/funds/positions/${id}/events`, id > 0);
export const useFundValuations = (id: number) => useFundQuery<FundValuation[]>(['valuations', id], `/funds/positions/${id}/valuations`, id > 0);
export const useFundDividends = (id: number) => useFundQuery<FundDividend[]>(['dividends', id], `/funds/positions/${id}/dividends`, id > 0);
export const useFundAudits = (id: number) => useFundQuery<FundAudit[]>(['audits', id], `/funds/positions/${id}/audits`, id > 0);
export function searchFund(q: string, signal?: AbortSignal): Promise<FundInstrument[]> { return readApi(`/funds/instruments/search?q=${encodeURIComponent(q)}`, signal); }
export function useFundMutations() {
  const done = useInvalidation('fund');
  const save = useMutation({ mutationFn: ({ path, input, method = 'post' }: { path: string; input: FundInput | { request_key: string }; method?: 'post' | 'put' }) => writeApi<FundReceipt>(method, path, input), onSuccess: done });
  const refresh = useMutation({ mutationFn: (id: number) => writeApi<FundAccount>('post', `/funds/accounts/${id}/refresh`), onSuccess: done });
  return { save, refresh };
}
