import type { Dayjs } from 'dayjs';
import type { LiabilityDetail } from '../../api/liability';
import type {
  AllocationMode,
  FeeMode,
  LiabilityCommand,
  SettlementInput,
} from '../../api/liability-inputs';
import type { LiabilityDialog } from './dialog';
import { cents } from '../../lib/money';

export interface TermValue {
  id: number;
  remaining_principal?: string;
  statement_id?: number;
  fee_amount?: string;
  fee_date?: Dayjs;
  fee_txn_id?: number;
}
export interface LiabilityValues {
  amount?: string;
  entry_date?: Dayjs;
  from_account_id?: number;
  mode?: AllocationMode;
  targets?: { statement_id: number; amount: string }[];
  reason?: string;
  reference_amount?: string;
  complete?: boolean;
  moves?: { item_id: number; statement_id: number }[];
  source_txn_id?: number;
  category_id?: number;
  note?: string;
  platform?: string;
  term_count?: number;
  first_statement_date?: Dayjs;
  fee_mode?: FeeMode;
  fee_input?: string;
  fee_account_id?: number;
  fee_category_id?: number;
  fee_date?: Dayjs;
  terms?: TermValue[];
  refund_txn_id?: number;
  principal?: string;
  fee_amount?: string;
  version?: number;
}
function required<T>(value: T | undefined, label: string): T {
  if (value === undefined) throw new Error(`请填写${label}`);
  return value;
}
export function allocationMode(value: string | undefined): AllocationMode {
  if (!value || value === 'auto') return 'auto';
  if (value === 'manual' || value === 'hold') return value;
  throw new Error('还款分配模式无效，请重新选择');
}
export function settlementInput(
  dialog: Extract<LiabilityDialog, { mode: 'settle' }>,
  value: LiabilityValues
): SettlementInput {
  return {
    request_key: dialog.requestKey,
    reason: value.reason ?? '',
    version: value.version ?? dialog.version,
    principal: required(value.principal, '实际剩余本金'),
    fee_amount: value.fee_amount || '0',
    fee_account_id: required(value.fee_account_id, '费用支付账户'),
    fee_category_id: required(value.fee_category_id, '费用分类'),
    from_account_id: required(value.from_account_id, '还款账户'),
    entry_date: required(value.entry_date, '实际日期').format('YYYY-MM-DD'),
  };
}
export function liabilityCommand(
  detail: LiabilityDetail,
  dialog: LiabilityDialog,
  value: LiabilityValues,
  feeDates?: string[]
): LiabilityCommand {
  const base = { request_key: dialog.requestKey, reason: value.reason ?? '' };
  const mode = () => allocationMode(value.mode);
  switch (dialog.mode) {
    case 'repay':
      return {
        mode: 'repay',
        input: {
          ...base,
          account_id: detail.account.id,
          from_account_id: required(value.from_account_id, '还款账户'),
          amount: required(value.amount, '还款金额'),
          entry_date: required(value.entry_date, '实际还款日期').format(
            'YYYY-MM-DD'
          ),
          mode: mode(),
          targets: mode() === 'manual' ? (value.targets ?? []) : [],
        },
      };
    case 'allocation':
      return {
        mode: 'allocation',
        txnId: dialog.txnId,
        input: {
          ...base,
          mode: mode(),
          targets: mode() === 'manual' ? (value.targets ?? []) : [],
        },
      };
    case 'reconcile':
      return {
        mode: 'reconcile',
        billId: dialog.billId,
        input: {
          ...base,
          reference_amount: value.reference_amount || undefined,
          complete: !!value.complete,
          moves: value.moves ?? [],
        },
      };
    case 'create_plan': {
      const schedule = {
        ...base,
        account_id: detail.account.id,
        term_count: required(value.term_count, '分期期数'),
        first_statement_date: required(
          value.first_statement_date,
          '首期账单'
        ).format('YYYY-MM-DD'),
        fee_mode: value.fee_mode ?? 'per_term',
        fee_input: value.fee_input || '0',
        fee_account_id: required(value.fee_account_id, '费用账户'),
        fee_category_id: required(value.fee_category_id, '费用分类'),
        fee_date: value.fee_date?.format('YYYY-MM-DD'),
        fee_dates: feeDates,
        note: value.note,
        platform: value.platform,
      };
      return value.source_txn_id
        ? {
            mode: 'create_plan',
            input: {
              ...schedule,
              source_txn_id: value.source_txn_id,
              entry_date: value.entry_date?.format('YYYY-MM-DD'),
              category_id: value.category_id,
            },
          }
        : {
            mode: 'create_plan',
            input: {
              ...schedule,
              amount: required(value.amount, '消费金额'),
              entry_date: required(value.entry_date, '实际消费日期').format(
                'YYYY-MM-DD'
              ),
              category_id: required(value.category_id, '消费分类'),
            },
          };
    }
    case 'edit_plan': {
      const plan = detail.plans.find((item) => item.id === dialog.planId);
      if (!plan) throw new Error('分期已变化，请重新读取');
      const terms = (value.terms ?? []).map((term) => {
        const previous = plan.terms.find((item) => item.id === term.id);
        if (!previous) throw new Error('期次已变化，请重新核对');
        return {
          id: term.id,
          remaining_principal:
            term.remaining_principal !== undefined &&
            cents(term.remaining_principal) !== cents(previous.remaining)
              ? term.remaining_principal
              : undefined,
          statement_id:
            term.statement_id !== previous.statement_id
              ? term.statement_id
              : undefined,
          fee_amount:
            term.fee_amount !== undefined &&
            cents(term.fee_amount) !== cents(previous.fee_amount)
              ? term.fee_amount
              : undefined,
          fee_date:
            term.fee_date?.format('YYYY-MM-DD') !== previous.fee_date
              ? term.fee_date?.format('YYYY-MM-DD')
              : undefined,
          fee_txn_id: term.fee_txn_id,
        };
      });
      return {
        mode:
          plan.status === 'needs_reconcile' ? 'refund_adjustment' : 'edit_plan',
        planId: dialog.planId,
        input: {
          ...base,
          version: value.version ?? dialog.version,
          refund_txn_id: value.refund_txn_id,
          terms,
        },
      };
    }
    case 'settle':
      return {
        mode: 'settle',
        planId: dialog.planId,
        input: settlementInput(dialog, value),
      };
    case 'reverse':
      return { mode: 'reverse', operationId: dialog.operationId, input: base };
  }
}
