import { useNavigate } from 'react-router-dom';
import { Badge } from 'antd';
import { BellOutlined } from '@ant-design/icons';

import { useNotifications } from '../api/hooks';

/**
 * 提醒铃铛（specs/notifications「铃铛与未读红点」）：
 * 导航区常驻；未读 >0 显示红点与数字，0 时纯图标；点击打开提醒列表。
 * 桌面放侧栏 brand 行，移动放底部 Tab 栏右端（非 Tab，不改动四 Tab 枚举）。
 */
export default function NotifBell() {
  const navigate = useNavigate();
  const notifs = useNotifications();
  const unread = notifs.data?.unread ?? 0;
  return (
    <button
      type="button"
      className="notif-bell"
      data-unread={unread > 0 ? '1' : '0'}
      aria-label="提醒"
      title="提醒"
      onClick={() => navigate('/notifications')}
    >
      <Badge count={unread} size="small" overflowCount={99}>
        <BellOutlined />
      </Badge>
    </button>
  );
}
