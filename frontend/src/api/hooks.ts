import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type ApiBody } from './client';
import type {
  Account,
  AssetsResp,
  AuditResp,
  BudgetSetInput,
  BudgetView,
  Category,
  CategoryForest,
  CategoryShareResp,
  ClosingPreview,
  CompareResp,
  DateRange,
  NotifListResp,
  PeriodClosing,
  PeriodSummaryResp,
  RecurringRule,
  Role,
  RuleInput,
  Settings,
  Transaction,
  TrendResp,
  TxnListParams,
  TxnListResp,
  User,
  VoucherDetail,
  VoucherListResp,
  VoucherInput,
  VoucherAction,
  VoucherOperationInput,
  VoucherPlatform,
} from './types';

/** 统一解包：{code,message,data} → data（code!=0 抛业务错误）。 */
async function unwrap<T>(url: string): Promise<T> {
  const { data } = await api.get<ApiBody<T>>(url);
  if (data.code !== 0) throw new Error(data.message);
  return data.data as T;
}

function qs(
  params: Record<string, string | number | undefined | null>,
): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : '';
}

/** 序列化流水查询参数（供 react-query key 稳定）。 */
export function txnQueryUrl(p: TxnListParams): string {
  return `/transactions${qs({
    start_date: p.start_date,
    end_date: p.end_date,
    period_key: p.period_key,
    kind: p.kind || undefined,
    account_id: p.account_id,
    category_id: p.category_id,
    amount_min: p.amount_min,
    amount_max: p.amount_max,
    keyword: p.keyword,
    related_txn_id: p.related_txn_id,
    page: p.page,
    page_size: p.page_size,
  })}`;
}

/** 导出当前筛选流水为 CSV（任务 11.2）：与列表同参但不分页，BOM 由后端产出。 */
export async function exportTxns(p: TxnListParams): Promise<void> {
  const url = `/transactions/export${qs({
    start_date: p.start_date,
    end_date: p.end_date,
    period_key: p.period_key,
    kind: p.kind || undefined,
    account_id: p.account_id,
    category_id: p.category_id,
    amount_min: p.amount_min,
    amount_max: p.amount_max,
    keyword: p.keyword,
  })}`;
  const res = await api.get(url, { responseType: 'blob', timeout: 30000 });
  const blob = new Blob([res.data], { type: 'text/csv;charset=utf-8' });
  const cd = String(res.headers['content-disposition'] ?? '');
  const name = /filename="?([^";]+)"?/.exec(cd)?.[1] ?? 'transactions.csv';
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(a.href);
}

// ---------- 查询 ----------

export function useAccounts() {
  return useQuery({
    queryKey: ['accounts'],
    queryFn: () => unwrap<Account[]>('/accounts'),
  });
}

export function useCategoryForest() {
  return useQuery({
    queryKey: ['categories'],
    queryFn: () => unwrap<CategoryForest>('/categories'),
  });
}

export function useTxnList(params: TxnListParams) {
  const url = txnQueryUrl(params);
  return useQuery({
    queryKey: ['txns', url],
    queryFn: () => unwrap<TxnListResp>(url),
    placeholderData: (prev) => prev,
  });
}

export function useStatsCategory(range: DateRange) {
  const url = `/stats/category${qs({ start_date: range.start, end_date: range.end })}`;
  return useQuery({
    queryKey: ['stats', url],
    queryFn: () => unwrap<CategoryShareResp>(url),
  });
}

export function useStatsTrend(range: DateRange, granularity: string) {
  const url = `/stats/trend${qs({ start_date: range.start, end_date: range.end, granularity })}`;
  return useQuery({
    queryKey: ['stats', url],
    queryFn: () => unwrap<TrendResp>(url),
  });
}

export function useStatsCompare(range: DateRange | null) {
  const url = `/stats/compare${qs({ start_date: range?.start, end_date: range?.end })}`;
  return useQuery({
    queryKey: ['stats', url],
    queryFn: () => unwrap<CompareResp>(url),
  });
}

export function useStatsPeriods(year: number) {
  const url = `/stats/periods${qs({ year })}`;
  return useQuery({
    queryKey: ['stats', url],
    queryFn: () => unwrap<PeriodSummaryResp>(url),
  });
}

export function useStatsAssets(range: DateRange) {
  const url = `/stats/assets${qs({ start_date: range.start, end_date: range.end })}`;
  return useQuery({
    queryKey: ['stats', url],
    queryFn: () => unwrap<AssetsResp>(url),
  });
}

// ---------- 流水写操作（成功后失效流水/余额/统计缓存） ----------

export interface TxnInput {
  kind: 'expense' | 'income' | 'transfer' | 'refund';
  amount: string;
  account_id: number;
  transfer_account_id?: number;
  category_id?: number;
  /** 退款关联的原支出流水 id */
  related_txn_id?: number;
  entry_date: string;
  note?: string;
  platform?: string;
}

function useTxnInvalidate() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ['txns'] });
    void qc.invalidateQueries({ queryKey: ['voucher-platforms'] });
    void qc.invalidateQueries({ queryKey: ['accounts'] });
    void qc.invalidateQueries({ queryKey: ['liabilities'] });
    void qc.invalidateQueries({ queryKey: ['stats'] });
    void qc.invalidateQueries({ queryKey: ['budgets'] });
    // 写流水可能触发预算超支提醒（OnWritePeriod），铃铛红点随之刷新
    void qc.invalidateQueries({ queryKey: ['notifications'] });
  };
}

// ---------- 券 ----------
export function useVoucherPlatforms(enabled: boolean) {
  return useQuery({
    queryKey: ['voucher-platforms'],
    queryFn: () => unwrap<VoucherPlatform[]>('/voucher-platforms'),
    enabled,
    retry: false,
    staleTime: 0,
  });
}
export function useCreateVoucherPlatform() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      voucherWrite<VoucherPlatform>('post', '/voucher-platforms', { name }),
    onSuccess: async (row) => {
      await qc.cancelQueries({ queryKey: ['voucher-platforms'] });
      qc.setQueryData<VoucherPlatform[]>(['voucher-platforms'], (rows = []) =>
        [...rows.filter((item) => item.id !== row.id), row].sort((a, b) =>
          a.name < b.name ? -1 : a.name > b.name ? 1 : a.id - b.id,
        ),
      );
      void qc.invalidateQueries({ queryKey: ['voucher-platforms'] });
    },
  });
}
export function useVouchers(
  params: {
    keyword?: string;
    status?: string;
    page?: number;
    page_size?: number;
  } = {},
) {
  const url = `/vouchers${qs(params)}`;
  return useQuery({
    queryKey: ['vouchers', url],
    queryFn: () => unwrap<VoucherListResp>(url),
  });
}
export function useVoucherDetail(id: number | null) {
  return useQuery({
    queryKey: ['vouchers', 'detail', id],
    queryFn: () => unwrap<VoucherDetail>(`/vouchers/${id}`),
    enabled: id !== null,
  });
}
function useVoucherInvalidate() {
  const qc = useQueryClient();
  return () => {
    for (const key of [
      'vouchers',
      'voucher-platforms',
      'txns',
      'accounts',
      'liabilities',
      'stats',
      'budgets',
      'notifications',
    ]) {
      void qc.invalidateQueries({ queryKey: [key] });
    }
  };
}
async function voucherWrite<T>(
  method: 'post' | 'put' | 'delete',
  url: string,
  input?: unknown,
): Promise<T> {
  const { data } = await api.request<ApiBody<T>>({ method, url, data: input });
  if (data.code !== 0) throw new Error(data.message);
  return data.data as T;
}
export function useEnsureVouchers() {
  const done = useVoucherInvalidate();
  return useMutation({
    mutationFn: () =>
      voucherWrite<{ generated: number; failed: number }>(
        'post',
        '/vouchers/ensure',
      ),
    onSuccess: done,
  });
}
export function useVoucherMutations() {
  const done = useVoucherInvalidate();
  const create = useMutation({
    mutationFn: (input: VoucherInput) =>
      voucherWrite<VoucherDetail>('post', '/vouchers', input),
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: number; input: VoucherInput }) =>
      voucherWrite<VoucherDetail>('put', `/vouchers/${id}`, input),
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: (id: number) => voucherWrite<null>('delete', `/vouchers/${id}`),
    onSuccess: done,
  });
  const operate = useMutation({
    mutationFn: ({
      id,
      action,
      input,
    }: {
      id: number;
      action: Exclude<VoucherAction, 'purchase'>;
      input: VoucherOperationInput;
    }) =>
      voucherWrite<VoucherDetail>('post', `/vouchers/${id}/${action}`, input),
    onSuccess: done,
  });
  const undo = useMutation({
    mutationFn: ({ id, eventId }: { id: number; eventId: number }) =>
      voucherWrite<VoucherDetail>(
        'post',
        `/vouchers/${id}/events/${eventId}/undo`,
      ),
    onSuccess: done,
  });
  return { create, update, remove, operate, undo };
}

export function useCreateTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async (input: TxnInput) => {
      const { data } = await api.post<ApiBody<Transaction>>(
        '/transactions',
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
}

export function useUpdateTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async ({ id, input }: { id: number; input: TxnInput }) => {
      const { data } = await api.put<ApiBody<Transaction>>(
        `/transactions/${id}`,
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
}

export function useDeleteTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async (id: number) => {
      const { data } = await api.delete<ApiBody<null>>(`/transactions/${id}`);
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
}

// ---------- 账户 / 分类写操作（管理员） ----------

export interface AccountInput {
  name: string;
  type: 'asset' | 'liability';
  opening_balance?: string;
  repayment_day?: number;
  liability_type?: string;
  statement_day?: number;
  repayment_month_offset?: number;
  sort?: number;
  status?: 'active' | 'disabled';
}

export function useAccountMutations() {
  const qc = useQueryClient();
  const done = () => {
    void qc.invalidateQueries({ queryKey: ['liabilities'] });
    void qc.invalidateQueries({ queryKey: ['accounts'] });
    void qc.invalidateQueries({ queryKey: ['stats'] });
  };
  const create = useMutation({
    mutationFn: async (input: AccountInput) => {
      const { data } = await api.post<ApiBody<Account>>('/accounts', input);
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({ id, input }: { id: number; input: AccountInput }) => {
      const { data } = await api.put<ApiBody<Account>>(
        `/accounts/${id}`,
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      const { data } = await api.delete<ApiBody<null>>(`/accounts/${id}`);
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  return { create, update, remove };
}

export interface CategoryInput {
  kind: 'expense' | 'income';
  name: string;
  parent_id?: number;
  sort?: number;
}

export function useCategoryMutations() {
  const qc = useQueryClient();
  const done = () => {
    void qc.invalidateQueries({ queryKey: ['categories'] });
    void qc.invalidateQueries({ queryKey: ['txns'] });
    void qc.invalidateQueries({ queryKey: ['stats'] });
  };
  const create = useMutation({
    mutationFn: async (input: CategoryInput) => {
      const { data } = await api.post<ApiBody<Category>>('/categories', input);
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({
      id,
      name,
      sort,
    }: {
      id: number;
      name: string;
      sort?: number;
    }) => {
      const { data } = await api.put<ApiBody<Category>>(`/categories/${id}`, {
        name,
        sort,
      });
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      const { data } = await api.delete<ApiBody<null>>(`/categories/${id}`);
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  return { create, update, remove };
}

// ---------- 账期设置 / 封账 / 审计（管理员，specs/billing-periods） ----------

export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => unwrap<Settings>('/settings'),
  });
}

/** 修改结算日：仅影响此后写入的归期（后端不回溯历史）。 */
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (settlement_day: number) => {
      const { data } = await api.put<ApiBody<Settings>>('/settings', {
        settlement_day,
      });
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s);
      // 账期区间口径变化，刷新封账预览
      void qc.invalidateQueries({ queryKey: ['closings'] });
    },
  });
}

/** 封账预览：区间 + 该期收支结余（确认弹窗数据源）。 */
export function useClosingPreview(periodKey: string) {
  return useQuery({
    queryKey: ['closings', 'preview', periodKey],
    queryFn: () =>
      unwrap<ClosingPreview>(
        `/closings/preview${qs({ period_key: periodKey })}`,
      ),
    enabled: !!periodKey,
  });
}

export function useClosingMutations() {
  const qc = useQueryClient();
  const done = () => {
    void qc.invalidateQueries({ queryKey: ['closings'] });
    void qc.invalidateQueries({ queryKey: ['audit'] });
    void qc.invalidateQueries({ queryKey: ['txns'] });
    void qc.invalidateQueries({ queryKey: ['stats'] });
  };
  const close = useMutation({
    mutationFn: async (period_key: string) => {
      const { data } = await api.post<ApiBody<PeriodClosing>>('/closings', {
        period_key,
      });
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const reopen = useMutation({
    mutationFn: async (period_key: string) => {
      const { data } = await api.delete<ApiBody<null>>(
        `/closings/${period_key}`,
      );
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  return { close, reopen };
}

/** 审计日志分页（管理员查看）。 */
export function useAuditLogs(page: number, pageSize = 20) {
  const url = `/audit-logs${qs({ page, page_size: pageSize })}`;
  return useQuery({
    queryKey: ['audit', url],
    queryFn: () => unwrap<AuditResp>(url),
    placeholderData: (prev) => prev,
  });
}

// ---------- 预算（specs/budgets，任务 8.3） ----------

/** 预算配置与进度；不传 period_key 时后端返回当前账期（不在客户端推算归期）。 */
export function useBudgets(periodKey?: string) {
  const url = `/budgets${qs({ period_key: periodKey })}`;
  return useQuery({
    queryKey: ['budgets', url],
    queryFn: () => unwrap<BudgetView>(url),
  });
}

/** 保存预算整单配置（仅管理员）：保存后立即评估超支并生成提醒。 */
export function useUpdateBudgets() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      periodKey,
      input,
    }: {
      periodKey: string;
      input: BudgetSetInput;
    }) => {
      const { data } = await api.put<ApiBody<{ period_key: string }>>(
        `/budgets/${periodKey}`,
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['budgets'] });
      // 保存即评估超支，可能新增提醒（铃铛红点）
      void qc.invalidateQueries({ queryKey: ['notifications'] });
    },
  });
}

// ---------- 周期记账（specs/recurring-entries，任务 9.x） ----------

/** 模板列表（路由与服务层双门禁：仅管理员）。 */
export function useRules() {
  return useQuery({
    queryKey: ['recurring'],
    queryFn: () => unwrap<RecurringRule[]>('/recurring'),
  });
}

export function useRuleMutations() {
  const qc = useQueryClient();
  const done = () => void qc.invalidateQueries({ queryKey: ['recurring'] });
  const create = useMutation({
    mutationFn: async (input: RuleInput) => {
      const { data } = await api.post<ApiBody<RecurringRule>>(
        '/recurring',
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({ id, input }: { id: number; input: RuleInput }) => {
      const { data } = await api.put<ApiBody<RecurringRule>>(
        `/recurring/${id}`,
        input,
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const setStatus = useMutation({
    mutationFn: async ({
      id,
      status,
    }: {
      id: number;
      status: 'enabled' | 'paused';
    }) => {
      const { data } = await api.patch<ApiBody<unknown>>(
        `/recurring/${id}/status`,
        { status },
      );
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      const { data } = await api.delete<ApiBody<null>>(`/recurring/${id}`);
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  return { create, update, setStatus, remove };
}

/** 懒补齐：应用打开时触发到期生成（任务 9.3，兜底 cron）。生成了才刷新相关缓存。 */
export function useEnsureRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data } =
        await api.post<ApiBody<{ generated: number }>>('/recurring/ensure');
      if (data.code !== 0) throw new Error(data.message);
      return data.data!.generated;
    },
    onSuccess: (generated) => {
      if (generated > 0) {
        void qc.invalidateQueries({ queryKey: ['txns'] });
        void qc.invalidateQueries({ queryKey: ['stats'] });
        void qc.invalidateQueries({ queryKey: ['accounts'] });
        void qc.invalidateQueries({ queryKey: ['liabilities'] });
        void qc.invalidateQueries({ queryKey: ['budgets'] });
        // 懒补齐会新增一条「已补记」提醒（specs/notifications）
        void qc.invalidateQueries({ queryKey: ['notifications'] });
      }
    },
  });
}

// ---------- 用户管理（仅管理员，specs/identity/user-management，任务 2.5） ----------

export function useUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: () => unwrap<User[]>('/users'),
  });
}

export interface CreateUserInput {
  username: string;
  password: string;
  role: Role;
}

export function useUserMutations() {
  const qc = useQueryClient();
  // 角色/状态变化会改变权限与可见入口，统一刷新用户列表
  const done = () => void qc.invalidateQueries({ queryKey: ['users'] });
  const create = useMutation({
    mutationFn: async (input: CreateUserInput) => {
      const { data } = await api.post<ApiBody<User>>('/users', input);
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const setRole = useMutation({
    mutationFn: async ({ id, role }: { id: number; role: Role }) => {
      const { data } = await api.patch<ApiBody<User>>(`/users/${id}/role`, {
        role,
      });
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  const setStatus = useMutation({
    mutationFn: async ({
      id,
      status,
    }: {
      id: number;
      status: 'active' | 'disabled';
    }) => {
      const { data } = await api.patch<ApiBody<User>>(`/users/${id}/status`, {
        status,
      });
      if (data.code !== 0) throw new Error(data.message);
      return data.data!;
    },
    onSuccess: done,
  });
  /** 重置密码：后端会使该用户已签发 token 全部失效（含当前会话，若改的是自己）。 */
  const resetPassword = useMutation({
    mutationFn: async ({ id, password }: { id: number; password: string }) => {
      const { data } = await api.post<ApiBody<null>>(`/users/${id}/password`, {
        password,
      });
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: done,
  });
  return { create, setRole, setStatus, resetPassword };
}

// ---------- 站内提醒（specs/notifications，任务 10.2） ----------

/** 提醒列表 + 未读数（铃铛 Badge 与列表页共用）。 */
export function useNotifications(enabled = true) {
  return useQuery({
    queryKey: ['notifications'],
    queryFn: () => unwrap<NotifListResp>('/notifications'),
    enabled,
    // 服务端 cron（每日 08:00 还款/预算评估）会异步新增提醒，
    // 窗口聚焦刷新又被全局关闭，轮询兜底让红点不过期
    refetchInterval: 60_000,
  });
}

export function useNotifMutations() {
  const qc = useQueryClient();
  const done = () => void qc.invalidateQueries({ queryKey: ['notifications'] });
  /** 单条已读（幂等）。 */
  const markRead = useMutation({
    mutationFn: async (id: number) => {
      const { data } = await api.patch<ApiBody<{ unread: number }>>(
        `/notifications/${id}`,
        {},
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!.unread;
    },
    onSuccess: done,
  });
  /** 一键全部已读（未读归零，历史保留）。 */
  const markAllRead = useMutation({
    mutationFn: async () => {
      const { data } = await api.post<ApiBody<{ unread: number }>>(
        '/notifications/read-all',
      );
      if (data.code !== 0) throw new Error(data.message);
      return data.data!.unread;
    },
    onSuccess: done,
  });
  return { markRead, markAllRead };
}
