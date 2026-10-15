import { useMutation, useQuery } from '@tanstack/react-query';
import { readApi, writeApi } from './request';
import { useInvalidation } from './cache';
import { queryKeys, useScope } from './keys';
import type { Account } from './types';

export type GoldKind = 'opening' | 'buy' | 'sell';
export interface GoldQuote { id: number; buy_price: string; sell_price: string; quote_at: string; fetched_at: string; revision_of?: number }
export interface GoldQuoteView { quote: GoldQuote | null; last_attempt_at: string | null; last_success_at: string | null; next_refresh_at: string | null; status: string; error?: string; refreshing: boolean }
export interface GoldEvent { id: number; kind: GoldKind; grams: string; cash_amount: string; fee: string | null; trade_date: string; cash_account_id?: number; created_by: number; voided_at?: string; note: string }
export interface GoldAccount { account: Account; grams: string; cost: string; cost_price: string | null; market_value: string | null; holding_pnl: string | null; holding_return: string | null; realized_pnl: string; total_pnl: string | null; fees: string; cost_estimated: boolean; can_amend: boolean; event_version: number; status: string; quote: GoldQuoteView; events: GoldEvent[] }
export interface GoldInput { request_key: string; kind: GoldKind; grams: string; trade_date: string; cash_amount?: string; fee?: string; cash_account_id?: number; cost_mode?: 'total' | 'unit'; cost?: string; note?: string }
export interface GoldValuation { id: number; date: string; grams: string; cost: string; market_value: string | null; realized_pnl: string; event_version: number; status: string; quote_at: string | null; fetched_at: string | null }
function useGoldQuery<T>(parts: unknown[], path: string, enabled = true) {
  const scope = useScope();
  return useQuery({ queryKey: queryKeys.key('gold', scope, ...parts), queryFn: ({ signal }) => readApi<T>(path, signal), enabled, refetchInterval: 60_000, refetchOnWindowFocus: true });
}
export const useGoldQuote = (enabled = true) => useGoldQuery<GoldQuoteView>(['quote'], '/gold/quote', enabled);
export const useGoldAccounts = (enabled = true) => useGoldQuery<GoldAccount[]>(['accounts'], '/gold/accounts', enabled);
export const useGoldAccount = (id: number) => useGoldQuery<GoldAccount>(['account', id], `/gold/accounts/${id}`, id > 0);
export const useGoldValuations = (id: number) => useGoldQuery<GoldValuation[]>(['valuations', id], `/gold/accounts/${id}/valuations`, id > 0);
export function useGoldMutations() {
  const done = useInvalidation('gold');
  const save = useMutation({ mutationFn: ({ path, input, amend = false }: { path: string; input: GoldInput | { request_key: string }; amend?: boolean }) => writeApi<{ account_id: number; event_id: number }>(amend ? 'put' : 'post', path, input), onSuccess: done });
  const refresh = useMutation({ mutationFn: () => writeApi<GoldQuoteView>('post', '/gold/quote/refresh'), onSuccess: done });
  return { save, refresh };
}
