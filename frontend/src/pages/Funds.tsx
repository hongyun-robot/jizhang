import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, App, Button, Card, Empty, Space, Tag } from 'antd';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { useFundAccount, useFundAccounts, useFundMutations, type FundEventKind } from '../api/funds';
import { useAuth, canRecord } from '../stores/auth';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import AccountEditor from '../components/AccountEditor';
import Money from '../components/Money';
import { QueryBoundary } from '../components/QueryState';
import FundForm from '../features/funds/FundForm';
import { fundMoney } from '../domain/funds';
import '../styles/funds.css';

export function FundAmount({ value, pnl = false }: { value: string | null | undefined; pnl?: boolean }) {
  return value == null ? <span className="muted">暂无</span> : <Money value={fundMoney(value)} tone={pnl ? Number(value) < 0 ? 'expense' : 'income' : 'plain'} />;
}
export default function Funds() {
  const { id } = useParams();
  const accountId = Number(id || 0);
  const navigate = useNavigate();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const { message } = App.useApp();
  const user = useAuth((s) => s.user);
  const list = useFundAccounts();
  const detail = useFundAccount(accountId);
  const { refresh } = useFundMutations();
  const [createAccount, setCreateAccount] = useState(false);
  const [operation, setOperation] = useState<FundEventKind>();
  useEffect(() => { setOperation(undefined); setCreateAccount(false); }, [scope]);
  const selected = detail.data;
  const reload = async () => { try { await refresh.mutateAsync(accountId); if (alive.current && currentScope.current === scope) message.success('已检查最新可用净值'); } catch (e) { if (alive.current && currentScope.current === scope) message.error(errMsg(e)); } };
  return <div className="page">
    <div className="page-head"><div><div className="eyebrow">FUNDS · 个人投资资产</div><h2>{accountId ? selected?.account.name || '基金账户' : '基金账户'}</h2></div><Space wrap>
      {accountId ? <><Button onClick={() => navigate('/funds')}>账户列表</Button><Button icon={<ReloadOutlined />} loading={refresh.isPending} onClick={() => void reload()}>刷新净值</Button></> : <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateAccount(true)}>新建基金账户</Button>}
    </Space></div>
    {!accountId ? <QueryBoundary query={list}>{!list.data?.length ? <Card className="panel"><Empty description="还没有基金账户" /><p className="muted">建立本人的基金账户，再添加已有持仓或记录实际买入。市值会计入总资产。</p></Card> : <div className="fund-account-list">{list.data.map((f) => <Card className="panel" key={f.account.id}><div className="fund-account-heading"><Button type="link" onClick={() => navigate(`/funds/${f.account.id}`)}>{f.account.name}</Button><Tag>{f.positions.length} 只基金</Tag>{f.account.status === 'disabled' && <Tag>已停用</Tag>}</div><div className="fund-primary-amount"><FundAmount value={f.total_value} /></div><p className="muted small">持仓市值与待结算资金合计 · 本人资产</p><Button onClick={() => navigate(`/funds/${f.account.id}`)}>查看持仓</Button></Card>)}</div>}</QueryBoundary> : <QueryBoundary query={detail}>
      {selected && <>
        {!selected.configured && <Alert type="info" title="行情服务暂未开启" description="保留上次有效净值，新持仓需要可用行情才能确认。" />}
        <section className="panel fund-overview"><div><div className="muted">账户总资产</div><div className="fund-primary-amount" data-testid="fund-total"><FundAmount value={selected.total_value} /></div></div><div><div className="muted">基金市值</div><FundAmount value={selected.market_value} /></div><div><div className="muted">待结算资金</div><Money value={selected.pending_funds} /></div><div><div className="muted">累计投资盈亏</div><FundAmount value={selected.total_pnl} pnl /></div></section>
        <div className="fund-toolbar"><p className="muted small">投资收益与费用单独核算，不进入生活收支和预算。每只基金按各自最新公布净值估值。</p>{selected.account.status === 'active' && <Space wrap><Button onClick={() => setOperation('opening')}>录入已有持仓</Button>{canRecord(user?.role) && <Button type="primary" onClick={() => setOperation('buy')}>记录新买入</Button>}</Space>}</div>
        {!selected.positions.length ? <Empty description="账户暂无持仓" /> : <div className="fund-position-list">{selected.positions.map((p) => <Card className="panel fund-position-row" key={p.id}>
          <div className="fund-position-heading"><div><Button type="link" onClick={() => navigate(`/fund-positions/${p.id}`)}>{p.name || p.code}</Button><div className="muted small">{p.code} · {p.shares} 份</div></div><Button onClick={() => navigate(`/fund-positions/${p.id}`)}>详情</Button></div>
          <div className="fund-values"><div><span className="muted">市值</span><FundAmount value={p.market_value} /></div><div><span className="muted">持仓盈亏</span><FundAmount value={p.holding_pnl} pnl /></div><div><span className="muted">最近净值区间盈亏</span><FundAmount value={p.daily_pnl} pnl /></div></div>
          <p className="muted small">单位净值 {p.unit_nav || '暂无'} · 截至 {p.nav_date || '暂无有效净值'}</p>
          {p.daily_pnl == null && <p className="muted small">{p.daily_status === 'dividend_review' ? '分红待核对' : '暂无可核对的日盈亏'}</p>}
          {p.source_error && <Alert type="warning" title={p.source_error} />}
        </Card>)}</div>}
      </>}
    </QueryBoundary>}
    {createAccount && <AccountEditor key={`fund-account-${scope}`} fundOnly onClose={() => setCreateAccount(false)} onCreated={(a) => { setCreateAccount(false); navigate(`/funds/${a.id}`); }} />}
    {operation && accountId > 0 && <FundForm key={`${scope}:${accountId}:${operation}`} accountId={accountId} kind={operation} onClose={() => setOperation(undefined)} />}
  </div>;
}
