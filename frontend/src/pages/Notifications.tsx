import { Button, Empty, Skeleton, Tag } from 'antd';
import {
  AlertOutlined,
  CheckOutlined,
  CreditCardOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import dayjs from 'dayjs';

import { useNotifMutations, useNotifications } from '../api/hooks';
import type { NotifKind, NotifPayload, Notification } from '../api/types';

/** 类型 → 图标与标题（specs/notifications 三类提醒）；用主题变量，暗色自动跟随。 */
const KIND_META: Record<NotifKind, { title: string; icon: React.ReactNode; color: string }> = {
  budget_overdue: { title: '预算超支', icon: <AlertOutlined />, color: 'var(--expense)' },
  repayment_due: { title: '还款提醒', icon: <CreditCardOutlined />, color: 'var(--primary)' },
  recurring_patched: { title: '周期补记', icon: <SyncOutlined />, color: 'var(--primary)' },
};

function parsePayload(raw: string): NotifPayload {
  try {
    return JSON.parse(raw) as NotifPayload;
  } catch {
    return {};
  }
}

/** 按类型生成提醒正文（spec：预算含分类名与超出金额，还款含账户名/欠款/还款日）。 */
function notifText(kind: NotifKind, p: NotifPayload): string {
  switch (kind) {
    case 'budget_overdue': {
      const scope = p.scope === 'category' ? `「${p.category_name}」` : '总预算';
      return `${scope}已超支 ¥${p.over_amount ?? '0.00'}（预算 ¥${p.budget ?? '0'}，已用 ¥${p.consumed ?? '0'}，账期 ${p.period_key ?? ''}）`;
    }
    case 'repayment_due': {
      const when = p.overdue
        ? `已过还款日 ${p.repayment_date}`
        : `距还款日 ${p.days} 天（${p.repayment_date}）`;
      return `${p.account_name ?? '信用卡'}欠款 ¥${p.debt ?? '0.00'}，${when}`;
    }
    case 'recurring_patched':
      return `已补记 ${p.generated ?? 0} 笔周期流水，请到流水页核对`;
    default:
      return '';
  }
}

/**
 * 提醒列表页（specs/notifications，任务 10.3）：
 * 单条已读 + 一键全部已读；已读行保留可回看。对四档角色开放。
 */
export default function Notifications() {
  const notifs = useNotifications();
  const { markRead, markAllRead } = useNotifMutations();

  const list = notifs.data?.list ?? [];
  const unread = notifs.data?.unread ?? 0;

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            INBOX{unread > 0 ? ` · 未读 ${unread}` : ' · 全部已读'}
          </div>
          <h2>提醒</h2>
        </div>
        <Button
          icon={<CheckOutlined />}
          disabled={unread === 0}
          loading={markAllRead.isPending}
          onClick={() => markAllRead.mutate()}
        >
          全部已读
        </Button>
      </div>

      {!notifs.data && !notifs.isError ? (
        <div className="list-skeleton">
          <Skeleton active paragraph={{ rows: 4 }} title={false} />
        </div>
      ) : list.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无提醒" />
      ) : (
        <ul className="notif-list">
          {list.map((n: Notification) => {
            const meta = KIND_META[n.kind] ?? { title: n.kind, icon: null, color: 'var(--muted)' };
            const isUnread = !n.read_at;
            return (
              <li key={n.id} className={`notif-row${isUnread ? ' unread' : ''}`}>
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
                  <div className="notif-text">{notifText(n.kind, parsePayload(n.payload))}</div>
                </div>
                {isUnread && (
                  <Button
                    size="small"
                    loading={markRead.isPending && markRead.variables === n.id}
                    onClick={() => markRead.mutate(n.id)}
                  >
                    标为已读
                  </Button>
                )}
                {n.read_at && <span className="notif-read small muted">已读</span>}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
