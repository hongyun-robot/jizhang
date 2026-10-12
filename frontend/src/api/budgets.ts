import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { BudgetSetInput, BudgetView } from './types';
import { readApi as unwrap, writeApi } from './request';
import { qs } from './query';
import { queryKeys, useScope } from './keys';

// ---------- 预算（specs/budgets，任务 8.3） ----------

/** 预算配置与进度；不传 period_key 时后端返回当前账期（不在客户端推算归期）。 */
export function useBudgets(periodKey?: string) {
  const scope = useScope();
  const url = `/budgets${qs({ period_key: periodKey })}`;
  return useQuery({
    queryKey: queryKeys.key('budgets', scope, url),
    queryFn: ({ signal }) => unwrap<BudgetView>(url, signal),
  });
}

/** 保存预算整单配置（仅管理员）：保存后立即评估超支并生成提醒。 */
export function useUpdateBudgets() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      periodKey,
      input,
    }: {
      periodKey: string;
      input: BudgetSetInput;
    }) => {
      const data = await writeApi<{ period_key: string }>(
        'put',
        `/budgets/${periodKey}`,
        input
      );
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.root('budgets', scope) });
      // 保存即评估超支，可能新增提醒（铃铛红点）
      void qc.invalidateQueries({
        queryKey: queryKeys.root('notifications', scope),
      });
    },
  });
}
