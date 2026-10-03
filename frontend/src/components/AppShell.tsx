import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { Grid, Layout, Menu } from 'antd';
import {
  HomeOutlined,
  PieChartOutlined,
  UnorderedListOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { useEnsureRecurring } from '../api/hooks';
import NotifBell from './NotifBell';

const { Sider } = Layout;

export interface NavItem {
  key: string;
  path: string;
  label: string;
  icon: React.ReactNode;
}

/** 全局导航：移动底部 Tab（记一笔/流水/统计/我的），桌面侧栏（同一目标不同形态）。 */
export const navItems: NavItem[] = [
  { key: 'dash', path: '/', label: '记一笔', icon: <HomeOutlined /> },
  { key: 'records', path: '/records', label: '流水', icon: <UnorderedListOutlined /> },
  { key: 'stats', path: '/stats', label: '统计', icon: <PieChartOutlined /> },
  { key: 'mine', path: '/mine', label: '我的', icon: <UserOutlined /> },
];

function activeKey(pathname: string): string {
  if (pathname === '/') return 'dash';
  return navItems.find((i) => i.path !== '/' && pathname.startsWith(i.path))?.key ?? 'dash';
}

/** 桌面侧栏布局（light zinc，符合中性底 + 单主色设计决策）。 */
export function DesktopShell({ children }: { children?: React.ReactNode }) {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <Layout style={{ minHeight: '100dvh' }}>
      <Sider width={200} breakpoint="lg" collapsedWidth={0} theme="light" className="app-sider">
        <div className="brand">
          <span>记账</span>
          <NotifBell />
        </div>
        <Menu
          mode="inline"
          selectedKeys={[activeKey(location.pathname)]}
          items={navItems.map((i) => ({ key: i.key, icon: i.icon, label: i.label }))}
          onClick={({ key }) => navigate(navItems.find((i) => i.key === key)!.path)}
        />
      </Sider>
      <Layout.Content className="app-content">{children ?? <Outlet />}</Layout.Content>
    </Layout>
  );
}

/** 移动底部 Tab 栏。 */
export function MobileShell({ children }: { children?: React.ReactNode }) {
  const navigate = useNavigate();
  const location = useLocation();
  const current = activeKey(location.pathname);
  return (
    <div className="mobile-shell">
      <div className="app-content">{children ?? <Outlet />}</div>
      <nav className="bottom-tabs">
        {navItems.map((i) => (
          <button
            key={i.key}
            type="button"
            className={current === i.key ? 'tab active' : 'tab'}
            onClick={() => navigate(i.path)}
          >
            {i.icon}
            <span>{i.label}</span>
          </button>
        ))}
        {/* 提醒铃铛：导航区内的工具位，非 Tab（四 Tab 枚举不变，specs/ui-shell） */}
        <NotifBell />
      </nav>
    </div>
  );
}

/** 按断点选择形态：≥768px 侧栏，<768px 底部 Tab（specs/ui-shell 双形态导航）。 */
export default function AppShell() {
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const location = useLocation();
  // 懒补齐（任务 9.3 / specs「打开页面懒补齐」）：应用打开时触发一次到期生成，
  // 兜底 cron 停摆；生成幂等，失败静默（下一次打开重试）。
  const ensure = useEnsureRecurring();
  useEffect(() => {
    ensure.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // 路由切换回到顶部：SPA 换页不会重置滚动位置，长列表翻页后跳统计页会停在半截。
  // 定位高亮跳转（首页 → 流水页 locateId）由流水页自己 scrollIntoView，这里必须让行；
  // 依赖只挂 pathname：清掉 locateId 的 replace 导航不触发复位，避免把刚定位的行又滚走。
  useEffect(() => {
    const st = location.state as { locateId?: number } | null;
    if (st?.locateId) return;
    window.scrollTo({ top: 0 });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);
  return isMobile ? <MobileShell /> : <DesktopShell />;
}
