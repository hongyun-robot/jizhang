import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Card, Empty, Grid, Skeleton } from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { useAccounts, useBudgets, useStatsCompare, useTxnList } from '../api/hooks';
import type { Transaction } from '../api/types';
import { useAuth } from '../stores/auth';
import Money from '../components/Money';
import BudgetProgress from '../components/BudgetProgress';
import TxnFormModal from '../components/TxnFormModal';

const ROLE_LABEL: Record<string, string> = {
  admin: '管理员',
  recorder: '记账员',
  member: '成员',
  viewer: '只读',
};

/** 首页概览（specs/home-dashboard）：本期收支结余 + 预算进度占位 + 最近10笔 + 快捷记账。 */
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

  const acctName = useMemo(() => {
    const m = new Map<number, string>();
    for (const a of accounts.data ?? []) m.set(a.id, a.name);
    return m;
  }, [accounts.data]);

  const cur = compare.data?.current;

  return (
    <div className="page">
      <div className="page-head">
        <h2>首页</h2>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setFormOpen(true)}>
          快捷记账
        </Button>
      </div>

      {/* 本期收支结余（与统计页同口径）；加载期用骨架占位，避免「0.00 → 真值」闪跳 */}
      <div className="stat-grid">
        <Card size="small" className="panel stat-card">
          <div className="stat-label">本期收入</div>
          {!compare.data && !compare.isError ? (
            <Skeleton active title={false} paragraph={{ rows: 1, width: '62%' }} />
          ) : (
            <Money value={cur?.income ?? '0'} tone="income" animate />
          )}
        </Card>
        <Card size="small" className="panel stat-card">
          <div className="stat-label">本期支出</div>
          {!compare.data && !compare.isError ? (
            <Skeleton active title={false} paragraph={{ rows: 1, width: '62%' }} />
          ) : (
            <Money value={cur?.expense ?? '0'} tone="expense" animate />
          )}
        </Card>
        <Card size="small" className="panel stat-card">
          <div className="stat-label">本期结余</div>
          {!compare.data && !compare.isError ? (
            <Skeleton active title={false} paragraph={{ rows: 1, width: '62%' }} />
          ) : (
            <Money
              value={cur?.balance ?? '0'}
              tone={Number(cur?.balance ?? 0) < 0 ? 'expense' : 'income'}
              animate
            />
          )}
        </Card>
        <Card size="small" className="panel stat-card">
          <div className="stat-label">总资产</div>
          {!accounts.data && !accounts.isError ? (
            <Skeleton active title={false} paragraph={{ rows: 1, width: '62%' }} />
          ) : (
            <Money value={totalAssets} animate />
          )}
        </Card>
      </div>

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
                    value={t.kind === 'income' ? t.amount : `-${t.amount}`}
                    tone={t.kind === 'income' ? 'income' : t.kind === 'expense' ? 'expense' : 'plain'}
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

      {user && <div className="muted small hi">你好，{user.username}（{ROLE_LABEL[user.role]}）</div>}

      <TxnFormModal open={formOpen} onClose={() => setFormOpen(false)} />
    </div>
  );
}
