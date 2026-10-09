/** 后端 DTO 类型（与 Go model JSON 一一对应；金额恒为字符串）。 */

export type Role = 'admin' | 'recorder' | 'member' | 'viewer';
export type AccountType = 'asset' | 'liability';
export type Status = 'active' | 'disabled';
export type TxnKind = 'expense' | 'income' | 'transfer' | 'refund';
export type CategoryKind = 'expense' | 'income';

export interface Account {
  id: number;
  name: string;
  type: AccountType;
  opening_balance: string;
  repayment_day?: number | null;
  sort: number;
  status: Status;
  /** 仅列表接口返回：期初 + 流水净额 */
  balance?: string;
}

/** 系统用户（单账本共享，不开放注册；无物理删除，spec identity/user-management）。 */
export interface User {
  id: number;
  username: string;
  role: Role;
  status: Status;
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
  created_by: number;
  source: 'manual' | 'recurring';
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
  balance: string;
}
export interface AssetPoint {
  date: string;
  total: string;
  accounts: AssetAcct[];
}
export interface AssetsResp {
  start: string;
  end: string;
  points: AssetPoint[];
  ending_total: string;
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

export type NotifKind = 'budget_overdue' | 'repayment_due' | 'recurring_patched';

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
  /** recurring_patched */
  generated?: number;
}

export interface Notification {
  id: number;
  kind: NotifKind;
  /** JSON 字符串，前端 JSON.parse 后按 kind 展示 */
  payload: string;
  read_at: string | null;
  created_at: string;
}

/** GET /notifications 响应（铃铛与列表页共用一次请求）。 */
export interface NotifListResp {
  list: Notification[];
  unread: number;
}
