import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Avatar, Button, Card, Segmented, Tag } from 'antd';
import {
  AppstoreOutlined,
  CalendarOutlined,
  FundOutlined,
  HistoryOutlined,
  SyncOutlined,
  TeamOutlined,
  TagsOutlined,
  LogoutOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { api } from '../api/client';
import { useAuth, canRecord } from '../stores/auth';
import { useTheme, type ThemeMode } from '../stores/theme';
import UserManager from '../components/UserManager';

const ROLE_LABEL: Record<string, string> = {
  admin: '管理员',
  recorder: '记账员',
  member: '成员',
  viewer: '只读',
};

const ROLE_COLOR: Record<string, string> = {
  admin: 'geekblue',
  recorder: 'cyan',
  member: 'default',
  viewer: 'default',
};

/** 我的：身份信息 + 外观主题 + 记账入口（周期记账对可记账角色可见）
 *  + 管理入口（账户/分类/用户仅管理员可见）+ 退出登录。 */
export default function Mine() {
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const clear = useAuth((s) => s.clear);
  const themeMode = useTheme((s) => s.mode);
  const setThemeMode = useTheme((s) => s.setMode);
  const [userMgmt, setUserMgmt] = useState(false);

  const logout = async () => {
    try {
      await api.post('/auth/logout');
    } catch {
      // 即使 refresh 已失效也照常清理本地会话
    }
    clear();
    navigate('/login', { replace: true });
  };

  return (
    <div className="page">
      <div className="page-head">
        <h2>我的</h2>
      </div>

      <Card size="small" className="panel mine-card">
        <Avatar size={48} icon={<UserOutlined />} style={{ background: '#0f766e' }} />
        <div className="mine-id">
          <div className="mine-name">{user?.username ?? '—'}</div>
          <div>
            {user && (
              <Tag color={ROLE_COLOR[user.role]}>{ROLE_LABEL[user.role] ?? user.role}</Tag>
            )}
          </div>
        </div>
      </Card>

      {/* 记账入口：管理员/记账员/普通成员可见（specs/recurring-entries 归属隔离） */}
      {canRecord(user?.role) && (
        <Card size="small" className="panel" title="记账">
          <div className="mine-menu">
            <Button block type="text" icon={<SyncOutlined />} onClick={() => navigate('/recurring')}>
              周期记账
            </Button>
          </div>
        </Card>
      )}

      {user?.role === 'admin' && (
        <Card size="small" className="panel" title="管理">
          <div className="mine-menu">
            <Button
              block
              type={userMgmt ? 'primary' : 'text'}
              icon={<TeamOutlined />}
              onClick={() => setUserMgmt((v) => !v)}
            >
              用户管理
            </Button>
            <Button
              block
              type="text"
              icon={<AppstoreOutlined />}
              onClick={() => navigate('/accounts')}
            >
              账户管理
            </Button>
            <Button block type="text" icon={<TagsOutlined />} onClick={() => navigate('/categories')}>
              分类管理
            </Button>
            <Button
              block
              type="text"
              icon={<CalendarOutlined />}
              onClick={() => navigate('/settings')}
            >
              账期设置
            </Button>
            <Button block type="text" icon={<FundOutlined />} onClick={() => navigate('/budgets')}>
              预算管理
            </Button>
            <Button block type="text" icon={<HistoryOutlined />} onClick={() => navigate('/audit')}>
              审计日志
            </Button>
          </div>
        </Card>
      )}

      {/* 用户管理（原地展开，不新增路由；见 components/UserManager） */}
      {user?.role === 'admin' && userMgmt && <UserManager />}

      <Card size="small" className="panel" title="外观">
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
      </Card>

      <Card size="small" className="panel">
        <Button block danger icon={<LogoutOutlined />} onClick={logout}>
          退出登录
        </Button>
      </Card>
    </div>
  );
}
