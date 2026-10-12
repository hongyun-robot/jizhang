import type { InstallmentPlan } from '../../api/liability';

export interface PlanDraft {
  amount?: string;
  entry_date?: string;
  category_id?: number;
  note?: string;
  platform?: string;
  source_txn_id?: number;
}
interface Unused {
  billId?: never;
  planId?: never;
  txnId?: never;
  operationId?: never;
  version?: never;
  draft?: never;
}
type State<Mode extends string, Fields = object> = {
  mode: Mode;
  requestKey: string;
} & Omit<Unused, keyof Fields> &
  Fields;
export type LiabilityDialog =
  | State<'repay'>
  | State<'allocation', { txnId: number }>
  | State<'reconcile', { billId: number }>
  | State<'create_plan', { draft?: PlanDraft }>
  | State<'edit_plan', { planId: number; version: number }>
  | State<'settle', { planId: number; version: number }>
  | State<'reverse', { operationId: number }>;
type Intent<T> = T extends { version: number }
  ? Omit<T, 'requestKey' | 'version'> & { version?: number }
  : T extends object
    ? Omit<T, 'requestKey'>
    : never;
export type LiabilityIntent = Intent<LiabilityDialog>;
export function beginDialog(
  intent: LiabilityIntent,
  plans: Pick<InstallmentPlan, 'id' | 'version'>[],
  requestKey: string = crypto.randomUUID()
): LiabilityDialog {
  if (intent.mode === 'edit_plan' || intent.mode === 'settle') {
    const version =
      intent.version ??
      plans.find((plan) => plan.id === intent.planId)?.version;
    if (version === undefined) throw new Error('分期已变化，请重新读取后操作');
    return { ...intent, requestKey, version };
  }
  return { ...intent, requestKey };
}
