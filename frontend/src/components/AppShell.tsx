import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { Grid, Layout, Menu } from 'antd';
import {
  HomeOutlined,
  MoonOutlined,
  PieChartOutlined,
  SunOutlined,
  UnorderedListOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { useBudgets, useEnsureRecurring, useEnsureVouchers, useStatsCompare } from '../api/hooks';
import { useEnsureLiabilities } from '../api/liability';
import { ROLE_LABEL } from '../labels';
import { canRecord, useAuth } from '../stores/auth';
import { resolve, useTheme } from '../stores/theme';
import Brand from './Brand';
import Money, { fmtMoney } from './Money';
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
  if (pathname.startsWith('/vouchers') || pathname.startsWith('/liabilities')) return 'mine';
  if (pathname === '/') return 'dash';
  return navItems.find((i) => i.path !== '/' && pathname.startsWith(i.path))?.key ?? 'dash';
}

/**
 * 各路径最近一次的查询串：切 Tab 返回时带上（specs/transactions 筛选状态跨路由保留）。
 * URL 仍是唯一事实来源——这里只记住「离开时该路径长什么样」，由 AppShell 在每次导航后回填。
 */
const lastSearch = new Map<string, string>();

/** 导航目标 = 路径 + 该路径最近一次查询串；已是当前地址则不重复压栈。 */
function navTarget(path: string, current: string): string | null {
  const target = path + (lastSearch.get(path) ?? '');
  return target === current ? null : target;
}

/** 路径最近一次的查询串：首页 locateId 跳转等非 Tab 导航复用（specs/transactions 筛选跨路由保留）。 */
export function lastSearchOf(pathname: string): string {
  return lastSearch.get(pathname) ?? '';
}

/** 侧栏「本期」速览：结余 + 预算水位，把导航下方的留白变成可扫读的信息位。 */
function SiderBrief() {
  const compare = useStatsCompare(null);
  const budgets = useBudgets();
  const cur = compare.data?.current;
  const total = budgets.data?.total;
  const pct = Math.min(100, Number(total?.percent ?? 0));

  return (
    <div className="sider-brief">
      <div className="brief-label">
        <span>本期结余</span>
        <span>{budgets.data?.period_key ?? '—'}</span>
      </div>
      <div className="brief-value">
        {compare.data ? (
          <Money
            value={cur?.balance ?? '0'}
            tone={Number(cur?.balance ?? 0) < 0 ? 'expense' : 'income'}
          />
        ) : (
          <span className="muted">—</span>
        )}
      </div>
      <div className="brief-meta">
        {compare.data ? (
          <>
            收入 {fmtMoney(cur?.income)} · 支出 {fmtMoney(cur?.expense)}
          </>
        ) : (
          '读取中…'
        )}
      </div>
      {total?.budget && (
        <>
          <div className={total.over ? 'brief-bar over' : 'brief-bar'}>
            <i style={{ width: `${pct}%` }} />
          </div>
          <div className="brief-meta">
            预算已用 {total.percent}%
            {total.over && <span className="budget-over"> · 已超支</span>}
          </div>
        </>
      )}
    </div>
  );
}

/** 桌面侧栏：品牌位 + 四项导航 + 本期速览 + 用户位（含明暗切换）。 */
export function DesktopShell({ children }: { children?: React.ReactNode }) {
  const navigate = useNavigate();
  const location = useLocation();
  const user = useAuth((s) => s.user);
  const mode = useTheme((s) => s.mode);
  const sys = useTheme((s) => s.sys);
  const setMode = useTheme((s) => s.setMode);
  const resolved = resolve(mode, sys);

  return (
    <Layout style={{ minHeight: '100dvh' }}>
      <Sider width={236} breakpoint="lg" collapsedWidth={0} theme="light" className="app-sider">
        <div className="brand">
          <Brand />
          <NotifBell />
        </div>
        <Menu
          mode="inline"
          selectedKeys={[activeKey(location.pathname)]}
          items={navItems.map((i) => ({ key: i.key, icon: i.icon, label: i.label }))}
          onClick={({ key }) => {
            const target = navTarget(
              navItems.find((i) => i.key === key)!.path,
              location.pathname + location.search,
            );
            if (target) navigate(target);
          }}
        />
        <SiderBrief />
        <div className="sider-foot">
          <span className="user-chip">
            <span className="avatar" aria-hidden="true">
              {(user?.username ?? '?').slice(0, 1).toUpperCase()}
            </span>
            <span className="who">
              <b>{user?.username ?? '—'}</b>
              <span>{user ? ROLE_LABEL[user.role] ?? user.role : ''}</span>
            </span>
          </span>
          <button
            type="button"
            className="theme-toggle"
            title={resolved === 'dark' ? '切换到浅色' : '切换到暗色'}
            aria-label={resolved === 'dark' ? '切换到浅色' : '切换到暗色'}
            onClick={() => setMode(resolved === 'dark' ? 'light' : 'dark')}
          >
            {resolved === 'dark' ? <SunOutlined /> : <MoonOutlined />}
          </button>
        </div>
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
            aria-current={current === i.key ? 'page' : undefined}
            onClick={() => {
              const target = navTarget(i.path, location.pathname + location.search);
              if (target) navigate(target);
            }}
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

/**
 * 按断点选择形态：≥992px 侧栏，<992px 底部 Tab（specs/ui-shell 双形态导航）。
 * 断点用 `lg`（992）与 `Sider breakpoint="lg"` 对齐，杜绝 768–991px 的「无导航带」；
 * `Grid.useBreakpoint()` 首帧可能为 undefined，只在明确 md/lg 为 false 时切移动形态，
 * 未定（undefined）按桌面渲染，避免首帧闪烁。
 */
export default function AppShell() {
  const screens = Grid.useBreakpoint();
  const isMobile = screens.lg === false;
  const location = useLocation();
  // 懒补齐（任务 9.3 / specs「打开页面懒补齐」）：应用打开时触发一次到期生成，
  // 兜底 cron 停摆；生成幂等，失败静默（下一次打开重试）。
  const ensure = useEnsureRecurring();
  const ensureVouchers = useEnsureVouchers();
  const ensureLiabilities=useEnsureLiabilities();
  useEffect(()=>{ensureLiabilities.mutate();const timer=window.setInterval(()=>ensureLiabilities.mutate(),60_000);return()=>window.clearInterval(timer);},[ensureLiabilities.mutate]);
  const voucherWriter = canRecord(useAuth((s) => s.user?.role));
  useEffect(() => {
    ensure.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (!voucherWriter) return;
    ensureVouchers.mutate();
    const timer = window.setInterval(() => ensureVouchers.mutate(), 60_000);
    return () => window.clearInterval(timer);
  }, [voucherWriter, ensureVouchers.mutate]);
  // 路由切换回到顶部：SPA 换页不会重置滚动位置，长列表翻页后跳统计页会停在半截。
  // 定位高亮跳转（首页 → 流水页 locateId）由流水页自己 scrollIntoView，这里必须让行；
  // 依赖只挂 pathname：清掉 locateId 的 replace 导航不触发复位，避免把刚定位的行又滚走。
  useEffect(() => {
    const st = location.state as { locateId?: number } | null;
    if (st?.locateId) return;
    // 瞬时复位：全局 html 仍是 scroll-behavior:smooth（锚点/定位跳转依赖），
    // 这里显式 instant，避免长列表从底部缓慢滚回顶。
    window.scrollTo({ top: 0, behavior: 'instant' as ScrollBehavior });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);
  // 记住每个路径最近一次的查询串：流水页切走再切回时恢复筛选（specs/transactions 跨路由保留）
  useEffect(() => {
    lastSearch.set(location.pathname, location.search);
  }, [location.pathname, location.search]);
  return isMobile ? <MobileShell /> : <DesktopShell />;
}
