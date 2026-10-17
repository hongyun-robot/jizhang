import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, App, Button, Card, Empty, Space, Tag } from 'antd';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { useWealthAccount, useWealthAccounts, useWealthCashAccounts, useWealthMutations, type WealthEventKind } from '../api/wealth';
import { useAuth, canRecord } from '../stores/auth';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import AccountEditor from '../components/AccountEditor';
import Money from '../components/Money';
import { QueryBoundary } from '../components/QueryState';
import WealthForm from '../features/wealth/WealthForm';
import WealthOrderForm from '../features/wealth/WealthOrderForm';
import { wealthMoney } from '../domain/wealth';
import '../styles/wealth.css';

export function WealthAmount({ value, pnl = false }: { value: string | null | undefined; pnl?: boolean }) {
  return value == null ? <span className="muted">暂无</span> : <Money value={wealthMoney(value)} tone={pnl ? Number(value) < 0 ? 'expense' : 'income' : 'plain'} />;
}
export default function Wealth() {
  const { id } = useParams();
  const accountId = Number(id || 0);
  const navigate = useNavigate();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const { message } = App.useApp();
  const user = useAuth((s) => s.user);
  const list = useWealthAccounts();
  const cash = useWealthCashAccounts();
  const detail = useWealthAccount(accountId);
  const { refresh } = useWealthMutations();
  const [createAccount, setCreateAccount] = useState(false);
  const [operation, setOperation] = useState<WealthEventKind>();
  const [pendingOrder, setPendingOrder] = useState(false);
  useEffect(() => { setOperation(undefined); setCreateAccount(false); setPendingOrder(false); }, [scope, accountId]);
  const selected = detail.data;
  const reload = async () => { try { const result=await refresh.mutateAsync(accountId); if (alive.current && currentScope.current === scope) {if(result.positions.some((p)=>p.source_status==='failed')) message.warning('查询失败，保留上次有效净值');else if(result.positions.some((p)=>p.source_status==='refreshing')) message.info('净值查询已在进行中');else message.success('已检查净值状态；冷却期间使用缓存');} } catch (e) { if (alive.current && currentScope.current === scope) message.error(errMsg(e)); } };
  return <div className="page">
    <div className="page-head"><div><div className="eyebrow">WEALTH · 个人投资资产</div><h2>{accountId ? selected?.account.name || '理财账户' : '理财账户'}</h2></div><Space wrap>
      {accountId ? <><Button onClick={() => navigate('/wealth')}>账户列表</Button><Button icon={<ReloadOutlined />} loading={refresh.isPending} onClick={() => void reload()}>刷新净值</Button></> : <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateAccount(true)}>新建理财账户</Button>}
    </Space></div>
    {!accountId ? <QueryBoundary query={list}>{!list.data?.length ? <Card className="panel"><Empty description="还没有理财账户" /><p className="muted">建立本人的理财账户，再添加已有持仓或记录实际买入。市值会计入总资产。</p></Card> : <div className="wealth-account-list">{list.data.map((f) => <Card className="panel" key={f.account.id}><div className="wealth-account-heading"><Button type="link" onClick={() => navigate(`/wealth/${f.account.id}`)}>{f.account.name}</Button><Tag>{f.positions.length} 款产品</Tag>{f.account.status === 'disabled' && <Tag>已停用</Tag>}</div><div className="wealth-primary-amount"><WealthAmount value={f.total_value} /></div><p className="muted small">持仓市值与待结算资金合计 · 本人资产</p><Button onClick={() => navigate(`/wealth/${f.account.id}`)}>查看持仓</Button></Card>)}</div>}</QueryBoundary> : <QueryBoundary query={detail}>
      {selected && <>
 {!selected.complete && <Alert type="warning" title="估值不完整" description={<>部分持仓缺少有效净值，已知小计 <WealthAmount value={selected.known_subtotal} />（包含应收），无法给出完整总资产。</>} />}
        {!selected.configured && <Alert type="info" title="行情服务暂未开启" description="保留上次有效净值。已接入产品即使缺少净值，也可以保存实际持仓和交易。" />}
        <section className="panel wealth-overview"><div><div className="muted">账户总资产</div><div className="wealth-primary-amount" data-testid="wealth-total"><WealthAmount value={selected.total_value} /></div></div><div><div className="muted">理财市值</div><WealthAmount value={selected.market_value} /></div><div><div className="muted">待结算资金</div><Money value={selected.pending_funds} /></div><div><div className="muted">累计投资盈亏</div><WealthAmount value={selected.total_pnl} pnl /></div></section>
        <div className="wealth-toolbar"><p className="muted small">投资收益与费用单独核算，不进入生活收支和预算。每款产品按本人有效净值估值；自动同日值优先，本人补录仅补缺失日期。</p>{selected.account.status === 'active' && <Space wrap><Button onClick={() => setOperation('opening')}>录入已有持仓</Button>{canRecord(user?.role) && <Button type="primary" onClick={() => setOperation('buy')}>记录新买入</Button>}</Space>}</div>
        {selected.account.status === 'active' && canRecord(user?.role) && <Button onClick={() => setPendingOrder(true)}>登记待确认申购</Button>}
        {!selected.positions.length ? <Empty description="账户暂无持仓" /> : <div className="wealth-position-list">{selected.positions.map((p) => <Card className="panel wealth-position-row" key={p.id}>
          <div className="wealth-position-heading"><div><Button type="link" onClick={() => navigate(`/wealth-positions/${p.id}`)}>{p.name || p.code}</Button><div className="muted small">{p.code} · {p.shares} 份</div></div><Button onClick={() => navigate(`/wealth-positions/${p.id}`)}>详情</Button></div>
          <div className="wealth-values"><div><span className="muted">市值</span><WealthAmount value={p.market_value} /></div><div><span className="muted">持仓盈亏</span><WealthAmount value={p.holding_pnl} pnl /></div><div><span className="muted">最近净值区间盈亏</span><WealthAmount value={p.daily_pnl} pnl /></div></div>
          <p className="muted small">单位净值 {p.unit_nav || '暂无'} · 截至 {p.nav_date || '暂无有效净值'}</p>
          {p.daily_pnl == null && <p className="muted small">{p.daily_status === 'dividend_review' ? '分红待核对' : '暂无可核对的日盈亏'}</p>}
          <p className="muted small">来源：{p.quote?.source === 'manual' ? '本人补录' : p.quote ? '自动同步' : '暂无'} · {p.product.currency}</p>{p.source_error && <Alert type="warning" title={p.source_error} />}
        </Card>)}</div>}
      </>}
    </QueryBoundary>}
    {createAccount && <AccountEditor key={`wealth-account-${scope}`} wealthOnly onClose={() => setCreateAccount(false)} onCreated={(a) => { setCreateAccount(false); navigate(`/wealth/${a.id}`); }} />}
    {!accountId && <section><div className="wealth-position-heading"><h3>理财零钱</h3><Button onClick={() => navigate('/accounts')}>关联普通账户</Button></div><QueryBoundary query={cash}>{!cash.data?.length ? <p className="muted">可将余利宝等现金账户关联理财产品，继续直接收支并自动记入收益。</p> : <div className="wealth-account-list">{cash.data.map((row) => <Card className="panel" key={row.account.id}><Button type="link" onClick={() => navigate(`/wealth-cash/${row.account.id}`)}>{row.account.name}</Button><Tag>理财零钱</Tag><div className="wealth-primary-amount"><Money value={row.account.balance ?? '0'} /></div><p className="muted small">{row.product?.name} · 可直接收支</p><p className="muted small">同一账户余额，已计入总资产</p><Button onClick={() => navigate(`/wealth-cash/${row.account.id}`)}>收益与设置</Button></Card>)}</div>}</QueryBoundary></section>}
    {operation && accountId > 0 && <WealthForm key={`${scope}:${accountId}:${operation}`} accountId={accountId} kind={operation} onClose={() => setOperation(undefined)} />}
    {pendingOrder && accountId > 0 && <WealthOrderForm key={`${scope}:${accountId}:subscribe`} accountId={accountId} kind="subscribe" onClose={() => setPendingOrder(false)} />}
  </div>;
}
