import { useInvalidation } from './cache';
import { writeEmptyApi } from './request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type {
  AuditResp,
  ClosingPreview,
  PeriodClosing,
  Settings,
} from './types';
import { readApi as unwrap, writeApi } from './request';
import { qs } from './query';
import { queryKeys, useScope } from './keys';

// ---------- 账期设置 / 封账 / 审计（管理员，specs/billing-periods） ----------

export function useSettings() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('settings', scope),
    queryFn: ({ signal }) => unwrap<Settings>('/settings', signal),
  });
}

/** 修改结算日：仅影响此后写入的归期（后端不回溯历史）。 */
export function useUpdateSettings() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (settlement_day: number) => {
      const data = await writeApi<Settings>('put', '/settings', {
        settlement_day,
      });
      return data;
    },
    onSuccess: (s) => {
      qc.setQueryData(queryKeys.key('settings', scope), s);
      // 账期区间口径变化，刷新封账预览
      void qc.invalidateQueries({
        queryKey: queryKeys.root('closings', scope),
      });
    },
  });
}

/** 封账预览：区间 + 该期收支结余（确认弹窗数据源）。 */
export function useClosingPreview(periodKey: string) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('closings', scope, 'preview', periodKey),
    queryFn: ({ signal }) =>
      unwrap<ClosingPreview>(
        `/closings/preview${qs({ period_key: periodKey })}`,
        signal
      ),
    enabled: !!periodKey,
  });
}

export function useClosingMutations() {
  const done = useInvalidation('closing');
  const close = useMutation({
    mutationFn: async (period_key: string) => {
      const data = await writeApi<PeriodClosing>('post', '/closings', {
        period_key,
      });
      return data;
    },
    onSuccess: done,
  });
  const reopen = useMutation({
    mutationFn: async (period_key: string) => {
      await writeEmptyApi('delete', `/closings/${period_key}`);
    },
    onSuccess: done,
  });
  return { close, reopen };
}

/** 审计日志分页（管理员查看）。 */
export function useAuditLogs(page: number, pageSize = 20) {
  const scope = useScope();
  const url = `/audit-logs${qs({ page, page_size: pageSize })}`;
  return useQuery({
    queryKey: queryKeys.key('audit', scope, url),
    queryFn: ({ signal }) => unwrap<AuditResp>(url, signal),
    placeholderData: (prev) => prev,
  });
}
