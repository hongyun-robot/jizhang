import { parsePayload, notifText } from '../features/notifications/payload';
import { App, Button, Empty, Skeleton, Tag } from 'antd';
import { QueryBoundary } from '../components/QueryState';
import { errMsg } from '../api/client';
import {
  AlertOutlined,
  CheckOutlined,
  CreditCardOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router-dom';

import { useNotifMutations, useNotifications } from '../api/notifications';
import type { NotifKind, Notification } from '../api/types';

/** 类型 → 图标与标题（specs/notifications 三类提醒）；用主题变量，暗色自动跟随。 */
const KIND_META: Record<
  NotifKind,
  { title: string; icon: React.ReactNode; color: string }
> = {
  budget_overdue: {
    title: '预算超支',
    icon: <AlertOutlined />,
    color: 'var(--expense)',
  },
  repayment_due: {
    title: '还款提醒',
    icon: <CreditCardOutlined />,
    color: 'var(--primary)',
  },
  recurring_patched: {
    title: '周期补记',
    icon: <SyncOutlined />,
    color: 'var(--primary)',
  },
};

/**
 * 提醒列表页（specs/notifications，任务 10.3）：
 * 单条已读 + 一键全部已读；已读行保留可回看。对四档角色开放。
 */
export default function Notifications() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const notifs = useNotifications();
  const { markRead, markAllRead } = useNotifMutations();

  const list = notifs.data?.list ?? [];
  const unread = notifs.data?.unread ?? 0;

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            INBOX
            {!notifs.data
              ? notifs.error
                ? ' · 读取失败'
                : ' · 读取中'
              : unread > 0
                ? ` · 未读 ${unread}`
                : ' · 全部已读'}
          </div>
          <h2>提醒</h2>
        </div>
        <Button
          icon={<CheckOutlined />}
          disabled={unread === 0}
          loading={markAllRead.isPending}
          onClick={() =>
            markAllRead.mutate(undefined, {
              onError: (error) => message.error(errMsg(error)),
            })
          }
        >
          全部已读
        </Button>
      </div>

      <QueryBoundary query={notifs}>
        {!notifs.data && !notifs.isError ? (
          <div className="list-skeleton">
            <Skeleton active paragraph={{ rows: 4 }} title={false} />
          </div>
        ) : list.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无提醒" />
        ) : (
          <ul className="notif-list">
            {list.map((n: Notification) => {
              const meta = KIND_META[n.kind] ?? {
                title: n.kind,
                icon: null,
                color: 'var(--muted)',
              };
              const isUnread = !n.read_at && !n.resolved_at;
              return (
                <li
                  key={n.id}
                  className={`notif-row${isUnread ? ' unread' : ''}`}
                >
                  <span className="notif-icon" style={{ color: meta.color }}>
                    {meta.icon}
                  </span>
                  <div className="notif-main">
                    <div className="notif-title">
                      <span>{meta.title}</span>
                      <Tag className="small" style={{ marginInlineEnd: 0 }}>
                        {dayjs(n.created_at).format('MM-DD HH:mm')}
                      </Tag>
                    </div>
                    <div className="notif-text">
                      {notifText(n.kind, parsePayload(n.payload))}
                    </div>
                    {n.kind === 'repayment_due' &&
                      parsePayload(n.payload).account_id && (
                        <Button
                          type="link"
                          size="small"
                          onClick={() =>
                            navigate(
                              `/liabilities/${parsePayload(n.payload).account_id}?bill=${n.statement_id ?? parsePayload(n.payload).statement_id ?? ''}`
                            )
                          }
                        >
                          查看账单
                        </Button>
                      )}
                  </div>
                  {isUnread && (
                    <Button
                      size="small"
                      loading={
                        markRead.isPending && markRead.variables === n.id
                      }
                      onClick={() =>
                        markRead.mutate(n.id, {
                          onError: (error) => message.error(errMsg(error)),
                        })
                      }
                    >
                      标为已读
                    </Button>
                  )}
                  {n.read_at && (
                    <span className="notif-read small muted">已读</span>
                  )}
                  {n.resolved_at && <Tag>已解决</Tag>}
                </li>
              );
            })}
          </ul>
        )}
      </QueryBoundary>
    </div>
  );
}
