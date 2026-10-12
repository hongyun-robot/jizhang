import { lazy, Suspense, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Avatar, Button, Card, Segmented, Skeleton, Tag } from 'antd';
import {
  AppstoreOutlined,
  CalendarOutlined,
  FundOutlined,
  HistoryOutlined,
  RightOutlined,
  SyncOutlined,
  TeamOutlined,
  TagsOutlined,
  LogoutOutlined,
  UserOutlined,
  GiftOutlined,
  CreditCardOutlined,
} from '@ant-design/icons';

import { session } from '../auth/session';
import { ROLE_LABEL } from '../labels';
import { useAuth, canRecord } from '../stores/auth';
import { useTheme, type ThemeMode } from '../stores/theme';
const UserManager = lazy(() => import('../components/UserManager'));

/** 角色徽标：不走 antd 预设蓝，统一到墨绿系（styles.css .role-tag）。 */
const ROLE_TONE: Record<string, string> = {
  admin: 'admin',
  recorder: 'recorder',
  member: 'member',
  viewer: 'viewer',
};

/** 「我的」里的入口磁贴：图标 + 名称 + 右箭头，桌面三列铺排。 */
function Tile({
  icon,
  label,
  onClick,
  active,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <Button
      type="text"
      className={`mine-tile${active ? ' is-active' : ''}`}
      onClick={onClick}
      aria-label={label}
    >
      <span className="mine-tile-icon">{icon}</span>
      <span className="mine-tile-label">{label}</span>
      <RightOutlined className="mine-tile-arrow" />
    </Button>
  );
}

/** 我的：身份信息 + 外观主题 + 记账入口（周期记账对可记账角色可见）
 *  + 管理入口（账户/分类/用户仅管理员可见）+ 退出登录。 */
export default function Mine() {
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const themeMode = useTheme((s) => s.mode);
  const setThemeMode = useTheme((s) => s.setMode);
  const [userMgmt, setUserMgmt] = useState(false);

  const logout = async () => {
    try {
      await session.logout();
    } catch {
      // 即使 refresh 已失效也照常清理本地会话
    }
    navigate('/login', { replace: true });
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">ACCOUNT · 身份、外观与入口</div>
          <h2>我的</h2>
        </div>
      </div>

      <Card size="small" className="panel panel--measure mine-card">
        <Avatar size={54} icon={<UserOutlined />} className="mine-avatar" />
        <div className="mine-id">
          <div className="mine-name">{user?.username ?? '—'}</div>
          <div className="mine-meta">
            {user && (
              <Tag
                className={`role-tag role-${ROLE_TONE[user.role] ?? 'member'}`}
              >
                {ROLE_LABEL[user.role] ?? user.role}
              </Tag>
            )}
            <span className="mine-hint">家人共用同一本账簿</span>
          </div>
        </div>
      </Card>

      {/* 记账入口：管理员/记账员/普通成员可见（specs/recurring-entries 归属隔离） */}
      <Card size="small" className="panel panel--measure" title="券">
        <div className="mine-menu">
          <Tile
            icon={<GiftOutlined />}
            label="我的券"
            onClick={() => navigate('/vouchers')}
          />
        </div>
      </Card>
      <Card size="small" className="panel panel--measure" title="资金">
        <div className="mine-menu">
          <Tile
            icon={<CreditCardOutlined />}
            label="负债账户"
            onClick={() => navigate('/liabilities')}
          />
        </div>
      </Card>
      {canRecord(user?.role) && (
        <Card size="small" className="panel panel--measure" title="记账">
          <div className="mine-menu">
            <Tile
              icon={<SyncOutlined />}
              label="周期记账"
              onClick={() => navigate('/recurring')}
            />
          </div>
        </Card>
      )}

      {user?.role === 'admin' && (
        <Card size="small" className="panel panel--measure" title="管理">
          <div className="mine-menu">
            <Tile
              icon={<TeamOutlined />}
              label="用户管理"
              active={userMgmt}
              onClick={() => setUserMgmt((v) => !v)}
            />
            <Tile
              icon={<AppstoreOutlined />}
              label="账户管理"
              onClick={() => navigate('/accounts')}
            />
            <Tile
              icon={<TagsOutlined />}
              label="分类管理"
              onClick={() => navigate('/categories')}
            />
            <Tile
              icon={<CalendarOutlined />}
              label="账期设置"
              onClick={() => navigate('/settings')}
            />
            <Tile
              icon={<FundOutlined />}
              label="预算管理"
              onClick={() => navigate('/budgets')}
            />
            <Tile
              icon={<HistoryOutlined />}
              label="审计日志"
              onClick={() => navigate('/audit')}
            />
          </div>
        </Card>
      )}

      {/* 用户管理（原地展开，不新增路由；见 components/UserManager） */}
      {user?.role === 'admin' && userMgmt && (
        <Suspense
          fallback={
            <div role="status">
              <Skeleton active />
            </div>
          }
        >
          <UserManager />
        </Suspense>
      )}

      <Card size="small" className="panel panel--measure" title="外观">
        <div className="seg-wrap">
          <Segmented
            block
            value={themeMode}
            onChange={(v) => setThemeMode(v as ThemeMode)}
            options={[
              { value: 'system', label: '跟随系统' },
              { value: 'light', label: '浅色' },
              { value: 'dark', label: '暗色' },
            ]}
          />
        </div>
      </Card>

      <Card size="small" className="panel panel--measure mine-foot">
        <Button
          block
          danger
          className="logout-btn"
          icon={<LogoutOutlined />}
          onClick={logout}
        >
          退出登录
        </Button>
      </Card>
    </div>
  );
}
