import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type ApiBody } from './client';
import type { Account, Transaction, TxnListResp } from './types';

export interface LiabilityAccountType { code: string; name: string; sort: number }
export interface LiabilitySummary {
  account_id: number; name: string; liability_type: string; billing_enabled: boolean;
  balance: string; total_debt: string; current_due: string; due_date: string;
  overdue: string; unbilled: string; future_principal: string; pending_credit: string; surplus: string; forecast_fees: string;
}
export interface BillRule { id: number; version: number; statement_day: number; repayment_day: number; repayment_month_offset: number; effective_from: string }
export interface LiabilityBill {
  id: number; kind: 'monthly' | 'settlement'; cycle_start: string; cycle_end: string; due_date: string;
  issued_at: string | null; snapshot_amount: string; reference_amount: string | null; reconciled_at: string | null;
  amount: string; paid_amount: string; remaining: string;
}
export interface DebtItem { id: number; source_id: number; source_txn_id: number | null; kind: string; statement_id: number; plan_id: number | null; term_id: number | null; amount: string; remaining: string }
export interface CreditEvent { id: number; source_id: number; amount: string; remaining: string; mode: string; targets: string; entry_date: string }
export interface Allocation { id: number; credit_event_id: number; debt_item_id: number; amount: string }
export interface InstallmentTerm { id: number; number: number; statement_id: number; principal: string; remaining: string; fee_amount: string; fee_date: string; fee_txn_id: number | null; status: string; fee_status: string; blocked_reason: string }
export interface InstallmentPlan { id: number; source_txn_id: number; principal: string; term_count: number; fee_mode: string; fee_input: string; fee_account_id: number; fee_category_id: number; created_by: number; version: number; status: string; terms: InstallmentTerm[] }
export interface LiabilityOperation { id: number; actor_id: number; kind: string; plan_id: number | null; reason: string; before_json: string; result_json: string; reversed_at: string | null; created_at: string }
export interface LiabilityDetail { account: Account; summary: LiabilitySummary; rules: BillRule[]; statements: LiabilityBill[]; items: DebtItem[]; credits: CreditEvent[]; allocations: Allocation[]; plans: InstallmentPlan[]; operations: LiabilityOperation[]; transactions: Transaction[] }
export interface SettlementPreview { plan_id: number; version: number; principal: string; paid_principal: string; outstanding_fees: string; cancelled_fees: string; pending_credit: string; fee_amount: string; transfer_amount: string }

async function get<T>(url: string): Promise<T> { const { data } = await api.get<ApiBody<T>>(url); if (data.code !== 0) throw new Error(data.message); return data.data!; }
export async function liabilityWrite<T>(method: 'post' | 'put' | 'patch', url: string, input: unknown): Promise<T> { const { data } = await api.request<ApiBody<T>>({ method, url, data: input }); if (data.code !== 0) throw new Error(data.message); return data.data!; }
export function useLiabilities() { return useQuery({ queryKey: ['liabilities'], queryFn: () => get<LiabilitySummary[]>('/liabilities') }); }
export function useLiabilityTypes(){return useQuery({queryKey:['liability-types'],queryFn:()=>get<LiabilityAccountType[]>('/accounts/liability-types')});}
export function useLiabilityDetail(id: number) { return useQuery({ queryKey: ['liabilities', 'detail', id], queryFn: () => get<LiabilityDetail>(`/accounts/${id}/liability`), enabled: id > 0 }); }
export function useFeeTransactions(accountId:number|undefined,enabled:boolean) {return useQuery({queryKey:['txns','fee-candidates',accountId],queryFn:()=>get<TxnListResp>(`/transactions?account_id=${accountId}&kind=expense&page_size=100`),enabled:enabled&&!!accountId});}
export function useLiabilityWrite() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ method = 'post', url, input }: { method?: 'post' | 'put' | 'patch'; url: string; input: unknown }) => liabilityWrite(method, url, input),
    onSuccess: async () => { await Promise.all(['liabilities', 'accounts', 'txns', 'stats', 'budgets', 'notifications', 'vouchers'].map((key) => qc.invalidateQueries({ queryKey: [key] }))); } });
}
export function useEnsureLiabilities() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => liabilityWrite<{ generated: number }>('post', '/liabilities/ensure', {}), onSuccess: (r) => {
    if (r.generated > 0) void Promise.all(['liabilities', 'accounts', 'txns', 'stats', 'budgets', 'notifications'].map((key) => qc.invalidateQueries({ queryKey: [key] })));
  } });
}
