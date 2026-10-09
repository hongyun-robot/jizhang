import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Card, Empty, Grid, Skeleton } from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { useAccounts, useBudgets, useStatsCompare, useTxnList } from '../api/hooks';
import type { Transaction } from '../api/types';
import { ROLE_LABEL } from '../labels';
import { useAuth } from '../stores/auth';
import Money from '../components/Money';
import BudgetProgress from '../components/BudgetProgress';
import TxnFormModal from '../components/TxnFormModal';

/** 指针柔光：把坐标写进 CSS 变量，只在精确指针设备上出现。 */
function trackPointer(e: React.PointerEvent<HTMLElement>) {
  const el = e.currentTarget;
  const r = el.getBoundingClientRect();
  el.style.setProperty('--mx', `${e.clientX - r.left}px`);
  el.style.setProperty('--my', `${e.clientY - r.top}px`);
}

/** 首页概览（specs/home-dashboard）：本期收支结余 hero + 预算进度 + 最近10笔 + 快捷记账。 */
export default function Dashboard() {
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const user = useAuth((s) => s.user);

  const [formOpen, setFormOpen] = useState(false);

  // 默认区间 = 当前账期，与统计页同口径（specs/home-dashboard 数值一致）
  const compare = useStatsCompare(null);
  const accounts = useAccounts();
  const recent = useTxnList({ page: 1, page_size: 10 });
  const budgets = useBudgets(); // 当前账期预算（后端推 period_key）
  const isAdmin = user?.role === 'admin';

  const totalAssets = useMemo(
    () => (accounts.data ?? []).reduce((s, a) => s + Number(a.balance ?? 0), 0),
    [accounts.data],
  );

  const activeAccounts = useMemo(
    () => (accounts.data ?? []).filter((a) => a.status === 'active').length,
    [accounts.data],
  );

  const acctName = useMemo(() => {
    const m = new Map<number, string>();
    for (const a of accounts.data ?? []) m.set(a.id, a.name);
    return m;
  }, [accounts.data]);

  const cur = compare.data?.current;
  const total = budgets.data?.total;
  const budgetLeft =
    total?.budget !== null && total?.budget !== undefined
      ? Number(total.budget) - Number(total.consumed)
      : null;

  const eyebrow = compare.data
    ? `${compare.data.start.replace(/-/g, '.')} – ${compare.data.end.replace(/-/g, '.')}`
    : '账期载入中';

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">{eyebrow}</div>
          <h2>首页</h2>
        </div>
        <div className="head-actions">
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setFormOpen(true)}>
            快捷记账
          </Button>
        </div>
      </div>

      {/* 本期结余 hero：一个主数字 + 收支拆解 + 资产/预算侧栏，取代四张同款卡片 */}
      <section className="hero panel" onPointerMove={trackPointer}>
        <div className="hero-main">
          <div className="hero-label">
            本期结余
            {budgets.data && <span className="hero-range">账期 {budgets.data.period_key}</span>}
          </div>
          <div className="hero-value">
            {!compare.data && !compare.isError ? (
              <Skeleton.Input active size="large" style={{ width: 220 }} />
            ) : (
              <Money
                value={cur?.balance ?? '0'}
                tone={Number(cur?.balance ?? 0) < 0 ? 'expense' : 'income'}
                animate
              />
            )}
          </div>
          <div className="hero-split">
            <span className="cell">
              <i className="dot income" />
              本期收入
              <Money value={cur?.income ?? '0'} tone="income" animate />
            </span>
            <span className="cell">
              <i className="dot expense" />
              本期支出
              <Money value={cur?.expense ?? '0'} tone="expense" animate />
            </span>
          </div>
        </div>

        <div className="hero-side">
          <div className="hero-stat">
            <div className="k">总资产</div>
            <div className="v">
              {accounts.data ? <Money value={totalAssets} animate /> : <span className="muted">—</span>}
            </div>
            <div className="s">{activeAccounts} 个启用账户</div>
          </div>
          <div className="hero-stat">
            <div className="k">预算剩余</div>
            <div className="v">
              {budgetLeft === null ? (
                <span className="muted">—</span>
              ) : (
                <Money
                  value={budgetLeft}
                  tone={budgetLeft < 0 ? 'expense' : 'plain'}
                  animate
                />
              )}
            </div>
            <div className="s">
              {budgetLeft !== null && budgetLeft < 0
                ? '已超支，建议调整'
                : total?.budget
                  ? `已用 ${total.percent}%`
                  : '本期尚未配置预算'}
            </div>
          </div>
        </div>
      </section>

      <div className={`dash-grid${isMobile ? '' : ' two'}`}>
        {/* 预算进度（8.3 接入真实值：超支标红显示超出金额，specs/budgets） */}
        <Card size="small" className="panel" title="预算进度">
          <BudgetProgress
            view={budgets.data}
            loading={budgets.isLoading}
            showPeriod
            configLink={isAdmin}
          />
        </Card>

        {/* 最近 10 笔（归属日期倒序） */}
        <Card
          size="small"
          className="panel"
          title="最近流水"
          extra={
            <Button type="link" size="small" onClick={() => navigate('/records')}>
              查看全部
            </Button>
          }
        >
          <div className="recent-list">
            {!recent.data && !recent.isError ? (
              <Skeleton active paragraph={{ rows: 5 }} title={false} />
            ) : (recent.data?.items ?? []).length > 0 ? (
              (recent.data?.items ?? []).map((t: Transaction) => (
                <div
                  className="recent-row clickable"
                  key={t.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => navigate('/records', { state: { locateId: t.id } })}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      navigate('/records', { state: { locateId: t.id } });
                    }
                  }}
                >
                  <span className="recent-note">
                    {t.kind === 'transfer'
                      ? `转账 ${acctName.get(t.account_id) ?? ''}→${acctName.get(t.transfer_account_id ?? 0) ?? ''}`
                      : t.note || acctName.get(t.account_id) || '—'}
                  </span>
                  <Money
                    value={
                      t.kind === 'income' || t.kind === 'refund' ? t.amount : `-${t.amount}`
                    }
                    tone={
                      t.kind === 'income' || t.kind === 'refund'
                        ? 'income'
                        : t.kind === 'expense'
                          ? 'expense'
                          : 'plain'
                    }
                  />
                </div>
              ))
            ) : (
              <div className="hi">
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description="还没有流水，记一笔吧"
                />
              </div>
            )}
          </div>
        </Card>
      </div>

      {/* 身份提示：桌面在侧栏用户位已可见，这里只在没有侧栏时补一行 */}
      {user && isMobile && (
        <div className="greeting">
          你好，<b>{user.username}</b> · {ROLE_LABEL[user.role] ?? user.role}
          {recent.data ? ` · 累计 ${recent.data.total.toLocaleString('zh-CN')} 笔` : ''}
        </div>
      )}

      <TxnFormModal open={formOpen} onClose={() => setFormOpen(false)} />
    </div>
  );
}
