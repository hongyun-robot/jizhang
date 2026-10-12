import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { NotifListResp } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

// ---------- 站内提醒（specs/notifications，任务 10.2） ----------

/** 提醒列表 + 未读数（铃铛 Badge 与列表页共用）。 */
export function useNotifications(enabled = true) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('notifications', scope),
    queryFn: ({ signal }) => unwrap<NotifListResp>('/notifications', signal),
    enabled,
    // 服务端 cron（每日 08:00 还款/预算评估）会异步新增提醒，
    // 窗口聚焦刷新又被全局关闭，轮询兜底让红点不过期
    refetchInterval: 60_000,
  });
}

export function useNotifMutations() {
  const scope = useScope();
  const qc = useQueryClient();
  const done = () =>
    void qc.invalidateQueries({
      queryKey: queryKeys.root('notifications', scope),
    });
  /** 单条已读（幂等）。 */
  const markRead = useMutation({
    mutationFn: async (id: number) => {
      const data = await writeApi<{ unread: number }>(
        'patch',
        `/notifications/${id}`,
        {}
      );
      return data.unread;
    },
    onSuccess: done,
  });
  /** 一键全部已读（未读归零，历史保留）。 */
  const markAllRead = useMutation({
    mutationFn: async () => {
      const data = await writeApi<{ unread: number }>(
        'post',
        '/notifications/read-all'
      );
      return data.unread;
    },
    onSuccess: done,
  });
  return { markRead, markAllRead };
}
