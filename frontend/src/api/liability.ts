import { liabilityClient } from './liability-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { readApi as get, writeApi } from './request';
import { queryKeys, useScope } from './keys';
import { invalidateFinancialViews } from './cache';
import type { Account, Transaction, TxnListResp } from './types';

export interface LiabilityAccountType {
  code: string;
  name: string;
  sort: number;
}
export interface LiabilitySummary {
  account_id: number;
  name: string;
  liability_type: string;
  billing_enabled: boolean;
  balance: string;
  total_debt: string;
  current_due: string;
  due_date: string;
  overdue: string;
  unbilled: string;
  future_principal: string;
  forecast_fees: string;
}
export interface BillRule {
  id: number;
  version: number;
  statement_day: number;
  repayment_day: number;
  repayment_month_offset: number;
  effective_from: string;
}
export interface LiabilityBill {
  id: number;
  kind: 'monthly' | 'settlement';
  cycle_start: string;
  cycle_end: string;
  due_date: string;
  issued_at: string | null;
  snapshot_amount: string;
  reference_amount: string | null;
  reconciled_at: string | null;
  amount: string;
  paid_amount: string;
  remaining: string;
}
export interface DebtItem {
  id: number;
  source_id: number;
  source_txn_id: number | null;
  kind: string;
  statement_id: number;
  plan_id: number | null;
  term_id: number | null;
  amount: string;
  remaining: string;
}
export interface CreditEvent {
  id: number;
  source_id: number;
  amount: string;
  remaining: string;
  mode: string;
  targets: string;
  entry_date: string;
}
export interface Allocation {
  id: number;
  credit_event_id: number;
  debt_item_id: number;
  amount: string;
}
export interface InstallmentTerm {
  id: number;
  number: number;
  statement_id: number;
  principal: string;
  remaining: string;
  fee_amount: string;
  fee_date: string;
  fee_txn_id: number | null;
  status: string;
  fee_status: string;
  blocked_reason: string;
}
export interface InstallmentPlan {
  id: number;
  source_txn_id: number;
  principal: string;
  term_count: number;
  fee_mode: string;
  fee_input: string;
  fee_account_id: number;
  fee_category_id: number;
  created_by: number;
  version: number;
  status: string;
  terms: InstallmentTerm[];
}
export interface LiabilityOperation {
  id: number;
  actor_id: number;
  kind: string;
  plan_id: number | null;
  reason: string;
  before_json: string;
  result_json: string;
  reversed_at: string | null;
  created_at: string;
}
export interface LiabilityDetail {
  account: Account;
  summary: LiabilitySummary;
  rules: BillRule[];
  statements: LiabilityBill[];
  items: DebtItem[];
  credits: CreditEvent[];
  allocations: Allocation[];
  plans: InstallmentPlan[];
  operations: LiabilityOperation[];
  transactions: Transaction[];
}
export interface SettlementPreview {
  plan_id: number;
  version: number;
  principal: string;
  paid_principal: string;
  outstanding_fees: string;
  scheduled_fees: string;
  total_amount: string;
  quote_signature: string;
  terms: {
    id: number;
    number: number;
    principal: string;
    outstanding_fees: string;
    scheduled_fees: string;
    total_amount: string;
  }[];
  transfer_amount: string;
}

export function useLiabilities() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('liabilities', scope),
    queryFn: ({ signal }) => get<LiabilitySummary[]>('/liabilities', signal),
  });
}
export function useLiabilityTypes() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('liability-types', scope),
    queryFn: ({ signal }) =>
      get<LiabilityAccountType[]>('/accounts/liability-types', signal),
  });
}
export function useLiabilityDetail(id: number) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('liabilities', scope, 'detail', id),
    queryFn: ({ signal }) =>
      get<LiabilityDetail>(`/accounts/${id}/liability`, signal),
    enabled: id > 0,
  });
}
export function useFeeTransactions(
  accountId: number | undefined,
  enabled: boolean
) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('txns', scope, 'fee-candidates', accountId),
    queryFn: ({ signal }) =>
      get<TxnListResp>(
        `/transactions?account_id=${accountId}&kind=expense&page_size=100`,
        signal
      ),
    enabled: enabled && !!accountId,
  });
}
export function useLiabilityWrite() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: liabilityClient.execute,
    onSuccess: () => invalidateFinancialViews(qc, scope),
  });
}
export function useEnsureLiabilities() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      writeApi<{ generated: number }>('post', '/liabilities/ensure', {}),
    onSuccess: (r) => {
      if (r.generated > 0) void invalidateFinancialViews(qc, scope);
      else
        void qc.invalidateQueries({
          queryKey: queryKeys.root('notifications', scope),
        });
    },
  });
}

export interface OrdinaryRepaymentPreview {
  scope: 'unbilled_ordinary';
  available_amount: string;
  amount: string;
  bills: { statement_id: number; cycle_start: string; cycle_end: string; due_date: string; available_amount: string }[];
  allocations: { statement_id: number; debt_item_id: number; source_id: number; amount: string }[];
  quote_signature: string;
}
