/** 后端 DTO 类型（与 Go model JSON 一一对应；金额恒为字符串）。 */

export type { Role } from '../domain/identity';
import type { SessionUser, UserStatus } from '../domain/identity';
export type AccountType = 'asset' | 'liability';
export type Status = UserStatus;
export type TxnKind = 'expense' | 'income' | 'transfer' | 'refund';
export type TxnPurpose =
  | 'regular'
  | 'voucher_purchase'
  | 'voucher_redemption'
  | 'voucher_refund'
  | 'voucher_writeoff'
  | 'fund_purchase'
  | 'fund_redemption'
  | 'fund_dividend';
export type CategoryKind = 'expense' | 'income';
export type PrincipalRemainder = 'first' | 'last';

export interface Account {
  id: number;
  name: string;
  type: AccountType;
  asset_class?: 'cash' | 'fund';
  valuation_status?: string;
  nav_date?: string;
  owner_id?: number | null;
  opening_balance: string;
  repayment_day?: number | null;
  liability_type?: string;
  billing_enabled?: boolean;
  principal_remainder?: PrincipalRemainder;
  has_financial_history?: boolean;
  sort: number;
  status: Status;
  /** 仅列表接口返回：期初 + 流水净额 */
  balance?: string;
}

/** 系统用户（单账本共享，不开放注册；无物理删除，spec identity/user-management）。 */
export interface User extends SessionUser {
  created_at: string;
  updated_at: string;
}

export interface Category {
  id: number;
  parent_id?: number | null;
  kind: CategoryKind;
  name: string;
  sort: number;
}

export interface CategoryTreeNode extends Category {
  children?: CategoryTreeNode[] | null;
}

/** 按 kind 分组的两级树：[支出, 收入]。 */
export type CategoryForest = CategoryTreeNode[];

export interface Transaction {
  fund_event_id?: number | null;
  fund_position_id?: number | null;
  fund_code?: string;
  historical_readonly?: boolean;
  account_name_override?: string;
  transfer_name_override?: string;
  id: number;
  kind: TxnKind;
  amount: string;
  account_id: number;
  transfer_account_id?: number | null;
  category_id?: number | null;
  /** 退款关联的原支出流水 id（仅退款有） */
  related_txn_id?: number | null;
  /** 仅列表接口返回：该支出被关联退款累计的金额 */
  refunded_amount?: string;
  entry_date: string;
  period_key: string;
  note?: string;
  platform?: string;
  created_by: number;
  source: 'manual' | 'recurring' | 'voucher_auto' | 'liability_auto';
  installment_plan_id?: number | null;
  liability_term_id?: number | null;
  liability_operation_id?: number | null;
  liability_account_id?: number | null;
  purpose?: TxnPurpose;
  voucher_event_id?: number | null;
  voucher_id?: number | null;
  balance_effect?: string;
  expense_effect?: string;
  created_at: string;
  updated_at: string;
}

export interface TxnListResp {
  total: number;
  page: number;
  page_size: number;
  items: Transaction[];
}

export interface TxnListParams {
  start_date?: string;
  end_date?: string;
  period_key?: string;
  kind?: TxnKind | '';
  account_id?: number;
  category_id?: number;
  amount_min?: string;
  amount_max?: string;
  keyword?: string;
  /** 定位关联到该支出的退款（refunds 跳转） */
  related_txn_id?: number;
  page?: number;
  page_size?: number;
}

/** 统计：金额均为字符串，占比/幅度为数字（可空）。 */
export interface ShareItem {
  id: number;
  name: string;
  amount: string;
  percent: number;
}
export interface ShareGroup {
  total: string;
  items: ShareItem[];
}
export interface CategoryShareResp {
  start: string;
  end: string;
  expense: ShareGroup;
  income: ShareGroup;
}

export interface TrendPoint {
  key: string;
  label: string;
  income: string;
  expense: string;
  balance: string;
}
export interface TrendResp {
  granularity: 'day' | 'week' | 'month';
  start: string;
  end: string;
  points: TrendPoint[];
}

export interface StatsTotals {
  income: string;
  expense: string;
  balance: string;
}
export interface CompareResp {
  start: string;
  end: string;
  previous_start: string;
  previous_end: string;
  current: StatsTotals;
  previous: StatsTotals;
  change: {
    income_percent: number | null;
    expense_percent: number | null;
    balance_percent: number | null;
  };
}

export interface PeriodItem {
  period_key: string;
  income: string;
  expense: string;
  balance: string;
}
export interface PeriodSummaryResp {
  year: number;
  periods: PeriodItem[];
}

export interface AssetAcct {
  account_id: number;
  name: string;
  type: AccountType;
  balance: string | null;
  asset_class?: 'cash' | 'fund';
  nav_date?: string;
}
export interface AssetPoint {
  date: string;
  total: string | null;
  accounts: AssetAcct[];
}
export interface AssetsResp {
  start: string;
  end: string;
  points: AssetPoint[];
  ending_total: string | null;
}

/** 通用区间（与统计页共用的区间语义）。 */
export interface DateRange {
  start: string;
  end: string;
}

// ---------- 账期设置 / 封账 / 审计（specs/billing-periods） ----------

export interface Settings {
  settlement_day: number;
}

/** 封账预览：区间按结算日换算展示，收支结余按物化 period_key 汇总。 */
export interface ClosingPreview {
  period_key: string;
  start_date: string;
  end_date: string;
  closed: boolean;
  ended: boolean;
  income: string;
  expense: string;
  balance: string;
}

export interface PeriodClosing {
  id: number;
  period_key: string;
  status: string;
  closed_by: number;
  closed_at: string;
}

export interface AuditItem {
  id: number;
  user_id: number;
  username: string;
  /** period.close / period.reopen */
  action: string;
  period_key: string;
  created_at: string;
}

export interface AuditResp {
  total: number;
  page: number;
  page_size: number;
  items: AuditItem[];
}

// ---------- 预算（specs/budgets） ----------

/** 预算进度：budget=null 未设置；percent 封顶 100（超支由 over/over_amount 表达）。 */
export interface BudgetProgress {
  budget: string | null;
  consumed: string;
  percent: number;
  over: boolean;
  over_amount: string;
}

export interface BudgetCategoryRow extends BudgetProgress {
  category_id: number;
  category_name: string;
}

/** GET /budgets：配置与进度一体，categories 为全部一级支出分类（回填用）。 */
export interface BudgetView {
  period_key: string;
  total: BudgetProgress;
  categories: BudgetCategoryRow[];
}

/** PUT /budgets/:period_key 整单提交：缺项即清除，amount 0 也是清除。 */
export interface BudgetSetItem {
  category_id: number;
  amount: string;
}
export interface BudgetSetInput {
  total: string | null;
  items: BudgetSetItem[];
}

// ---------- 周期记账（specs/recurring-entries，任务 9.x） ----------

export type RuleFreq = 'daily' | 'weekly' | 'monthly' | 'yearly';
export type RuleStatus = 'enabled' | 'paused';

export interface RecurringRule {
  blocked_reason?: string;
  id: number;
  amount: string;
  freq: RuleFreq;
  account_id: number;
  category_id: number;
  note: string;
  /** 首次生成日期 */
  start_date: string;
  /** 下一次应生成的归属日 */
  next_run_date: string;
  status: RuleStatus;
  created_by: number;
  /** 创建人姓名（仅列表接口附带，见 specs/recurring-entries 归属隔离）。 */
  created_by_name?: string;
  created_at: string;
  updated_at: string;
}

/** POST / PUT /recurring 提交体。 */
export interface RuleInput {
  amount: string;
  freq: RuleFreq;
  account_id: number;
  category_id: number;
  note?: string;
  start_date: string;
}

// ---------- 站内提醒（specs/notifications，任务 10.x） ----------

export type NotifKind =
  | 'budget_overdue'
  | 'repayment_due'
  | 'recurring_patched';

/** 提醒 payload：按 kind 取所需字段（服务端写入时已序列化为 JSON 字符串）。 */
export interface NotifPayload {
  dedup_key?: string;
  /** budget_overdue */
  period_key?: string;
  scope?: 'total' | 'category';
  category_name?: string;
  budget?: string;
  consumed?: string;
  over_amount?: string;
  /** repayment_due */
  account_id?: number;
  account_name?: string;
  debt?: string;
  repayment_date?: string;
  days?: number;
  overdue?: boolean;
  statement_id?: number;
  resolved?: boolean;
  initial_debt?: string;
  /** recurring_patched */
  generated?: number;
}

export interface Notification {
  id: number;
  kind: NotifKind;
  /** JSON 字符串，前端 JSON.parse 后按 kind 展示 */
  payload: string;
  read_at: string | null;
  statement_id?: number | null;
  resolved_at?: string | null;
  created_at: string;
}

/** GET /notifications 响应（铃铛与列表页共用一次请求）。 */
export interface NotifListResp {
  list: Notification[];
  unread: number;
}

export type VoucherAction = 'purchase' | 'redeem' | 'refund' | 'writeoff';
export interface VoucherPlatform {
  sort: number;
  id: number;
  name: string;
}
export interface Voucher {
  id: number;
  name: string;
  platform: string;
  note: string;
  paid_amount: string;
  purchase_date: string;
  account_id: number;
  category_id: number;
  total_units: number;
  expires_on: string | null;
  auto_refund_enabled: boolean;
  auto_refund_suspended: boolean;
  created_by: number;
  schedule_version: number;
  last_error: string;
  last_attempt_at: string | null;
  used_units: number;
  refunded_units: number;
  written_off_units: number;
  remaining_units: number;
  used_amount: string;
  refunded_amount: string;
  written_off_amount: string;
  remaining_amount: string;
  next_unit_amount: string;
  status: 'pending' | 'settled' | 'expired' | 'failed';
  settlement: '' | 'used' | 'refunded' | 'written_off';
  can_edit: boolean;
  can_edit_purchase: boolean;
  can_delete: boolean;
  can_operate: boolean;
}
export interface VoucherEvent {
  id: number;
  action: VoucherAction;
  units: number;
  amount: string;
  entry_date: string;
  period_key: string;
  created_by: number;
  source: 'manual' | 'auto';
  voided_at: string | null;
  voided_by: number | null;
  can_undo: boolean;
}
export interface VoucherDetail extends Voucher {
  events: VoucherEvent[];
}
export interface VoucherListResp {
  items: Voucher[];
  total: number;
  page: number;
  page_size: number;
  unused_amount: string;
}
export interface VoucherInput {
  name: string;
  platform?: string;
  note?: string;
  paid_amount: string;
  purchase_date: string;
  account_id: number;
  category_id: number;
  total_units: number;
  expires_on: string | null;
  auto_refund_enabled: boolean;
  resume_auto_refund?: boolean;
  request_key?: string;
}
export interface VoucherOperationInput {
  entry_date: string;
  units?: number;
  request_key: string;
}
