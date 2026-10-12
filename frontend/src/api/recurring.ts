import { useFinancialInvalidate } from './cache';
import { writeEmptyApi } from './request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { RecurringRule, RuleInput } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

// ---------- 周期记账（specs/recurring-entries，任务 9.x） ----------

/** 模板列表（路由与服务层双门禁：仅管理员）。 */
export function useRules() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('recurring', scope),
    queryFn: ({ signal }) => unwrap<RecurringRule[]>('/recurring', signal),
  });
}

export function useRuleMutations() {
  const scope = useScope();
  const qc = useQueryClient();
  const done = () =>
    void qc.invalidateQueries({ queryKey: queryKeys.root('recurring', scope) });
  const create = useMutation({
    mutationFn: async (input: RuleInput) => {
      const data = await writeApi<RecurringRule>('post', '/recurring', input);
      return data;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({ id, input }: { id: number; input: RuleInput }) => {
      const data = await writeApi<RecurringRule>(
        'put',
        `/recurring/${id}`,
        input
      );
      return data;
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
      await writeEmptyApi('patch', `/recurring/${id}/status`, { status });
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      await writeEmptyApi('delete', `/recurring/${id}`);
    },
    onSuccess: done,
  });
  return { create, update, setStatus, remove };
}

/** 懒补齐：应用打开时触发到期生成（任务 9.3，兜底 cron）。生成了才刷新相关缓存。 */
export function useEnsureRecurring() {
  const done = useFinancialInvalidate();
  return useMutation({
    mutationFn: async () =>
      (await writeApi<{ generated: number }>('post', '/recurring/ensure'))
        .generated,
    onSuccess: (generated) => {
      if (generated > 0) return done();
    },
  });
}
