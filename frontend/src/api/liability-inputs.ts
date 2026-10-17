export type AllocationMode = 'auto' | 'manual';
export type FeeMode = 'per_term' | 'total_spread' | 'upfront';
export interface OperationInput {
  request_key: string;
  reason: string;
}
export interface AllocationTarget {
  statement_id: number;
  amount: string;
}
export interface AllocationInput extends OperationInput {
  mode: AllocationMode;
  targets: AllocationTarget[];
}
export type RepaymentScope = 'issued' | 'unbilled_ordinary';
export interface RepaymentInput extends AllocationInput {
  scope?: RepaymentScope;
  quote_signature?: string;
  account_id: number;
  from_account_id: number;
  amount: string;
  entry_date: string;
}
export interface ReconcileInput extends OperationInput {
  reference_amount?: string;
  complete: boolean;
  moves: { item_id: number; statement_id: number }[];
}
interface ScheduleInput extends OperationInput {
  account_id: number;
  term_count: number;
  first_statement_date: string;
  fee_mode: FeeMode;
  fee_input: string;
  fee_account_id: number;
  fee_category_id: number;
  fee_date?: string;
  fee_dates?: string[];
  note?: string;
  platform?: string;
}
export type InstallmentInput = ScheduleInput &
  (
    | {
        source_txn_id: number;
        amount?: never;
        entry_date?: string;
        category_id?: number;
      }
    | {
        source_txn_id?: undefined;
        amount: string;
        entry_date: string;
        category_id: number;
      }
  );
export interface TermChange {
  id: number;
  remaining_principal?: string;
  statement_id?: number;
  fee_amount?: string;
  fee_date?: string;
  fee_txn_id?: number;
}
export interface InstallmentUpdate extends OperationInput {
  fee_account_id?: number;
  fee_category_id?: number;
  version: number;
  refund_txn_id?: number;
  terms: TermChange[];
}
export interface InstallmentRepaymentInput extends OperationInput {
  version: number;
  term_ids?: number[];
  all_remaining?: boolean;
  from_account_id: number;
  entry_date: string;
  quote_signature?: string;
  total_amount?: string;
}
export type SettlementInput = InstallmentRepaymentInput;
export interface LiabilityResult {
  repayment_scope?: RepaymentScope;
  allocation_mode?: AllocationMode;
  repayment_targets?: { statement_id: number; debt_item_id: number; source_id: number; amount: string }[];
  operation_id: number;
  plan_id?: number;
  version?: number;
  transaction_ids: number[];
  term_ids?: number[];
}
export type LiabilityCommand =
  | { mode: 'repay'; input: RepaymentInput }
  | { mode: 'allocation'; txnId: number; input: AllocationInput }
  | { mode: 'reconcile'; billId: number; input: ReconcileInput }
  | { mode: 'create_plan'; input: InstallmentInput }
  | {
      mode: 'edit_plan' | 'refund_adjustment';
      planId: number;
      input: InstallmentUpdate;
    }
  | { mode: 'settle'; planId: number; input: SettlementInput }
  | { mode: 'repay_plan'; planId: number; input: InstallmentRepaymentInput }
  | { mode: 'reverse'; operationId: number; input: OperationInput };
