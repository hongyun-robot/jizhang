import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, App, Button, Card, Empty, Popconfirm, Space, Table, Tabs, Tag } from 'antd';
import { useFundAccount, useFundAudits, useFundDividends, useFundEvents, useFundMutations, useFundPosition, useFundValuations, type FundDividend, type FundEvent, type FundEventKind } from '../api/funds';
import { useAuth, canRecord } from '../stores/auth';
import { canModifyOwnedRecord } from '../domain/permissions';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import { QueryBoundary } from '../components/QueryState';
import Money from '../components/Money';
import FundForm, { dateText, FUND_EVENT_LABEL } from '../features/funds/FundForm';
import { FundAmount } from './Funds';
import { fundMoney } from '../domain/funds';
import '../styles/funds.css';

export default function FundPositionDetail() {
  const positionId = Number(useParams().id || 0);
  const navigate = useNavigate();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const user = useAuth((s) => s.user);
  const { message } = App.useApp();
  const position = useFundPosition(positionId);
  const account = useFundAccount(position.data?.account_id || 0);
  const events = useFundEvents(positionId);
  const history = useFundValuations(positionId);
  const dividends = useFundDividends(positionId);
  const audits = useFundAudits(positionId);
  const { save } = useFundMutations();
  const undoKeys = useRef(new Map<number, string>());
  const [operation, setOperation] = useState<{ kind: FundEventKind; amend?: boolean; settlement?: FundEvent; dividend?: FundDividend }>();
  useEffect(() => { setOperation(undefined); undoKeys.current.clear(); }, [scope, positionId]);
  const p = position.data;
  const settled = new Set((events.data ?? []).filter((e) => !e.voided_at && e.source_event_id).map((e) => e.source_event_id));
  const recordedDates = new Set((events.data ?? []).filter((e) => !e.voided_at && (e.kind === 'cash_dividend' || e.kind === 'reinvest')).map((e) => dateText(e.rights_date)));
  const ordered = (events.data ?? []).filter((e) => !e.voided_at).sort((a, b) => dateText(a.effective_date).localeCompare(dateText(b.effective_date)) || a.id - b.id);
  const lastId = ordered.at(-1)?.id;
  const undo = async (eventId: number) => { if (save.isPending) return; let key = undoKeys.current.get(eventId); if (!key) { key = crypto.randomUUID(); undoKeys.current.set(eventId, key); } try { await save.mutateAsync({ path: `/funds/positions/${positionId}/events/${eventId}/undo`, input: { request_key: key } }); if (alive.current && currentScope.current === scope) message.success('事件已撤销'); } catch (e) { if (alive.current && currentScope.current === scope) message.error(errMsg(e)); } };
  return <div className="page">
    <div className="page-head"><div><div className="eyebrow">FUND POSITION · {p?.code || ''}</div><h2>{p?.name || '基金持仓'}</h2></div><Button onClick={() => navigate(p ? `/funds/${p.account_id}` : '/funds')}>返回账户</Button></div>
    <QueryBoundary query={position}>{p && <>
      <section className="panel fund-overview"><div><div className="muted">持仓市值</div><div className="fund-primary-amount"><FundAmount value={p.market_value} /></div></div><div><div className="muted">持仓盈亏</div><FundAmount value={p.holding_pnl} pnl /><div className="small muted">{p.holding_return == null ? '收益率暂无' : `${p.holding_return}%`}</div></div><div><div className="muted">最近净值区间盈亏</div><FundAmount value={p.daily_pnl} pnl /><div className="small muted">{p.daily_start && p.daily_end ? `${p.daily_start} → ${p.daily_end}` : '暂无可核对的日盈亏'}</div></div></section>
      <p className="muted">{p.code} · 单位净值 {p.unit_nav || '暂无'} · 净值截至 {p.nav_date || '暂无'} · 持仓生效 {p.effective_date || '暂无'}</p>
      <Card className="panel"><div className="fund-values"><div><span className="muted">确认份额</span><strong>{p.shares}</strong></div><div><span className="muted">单位成本</span><strong>{p.cost_price}</strong></div><div><span className="muted">剩余成本</span><Money value={p.cost} /></div><div><span className="muted">已实现赎回盈亏</span><FundAmount value={p.realized_pnl} pnl /></div><div><span className="muted">分红收益</span><Money value={p.dividends} /></div><div><span className="muted">投资费用（已计成本或净额）</span><Money value={p.fees} /></div><div><span className="muted">待结算资金</span><Money value={p.pending_funds} /></div><div><span className="muted">累计投资盈亏</span><FundAmount value={p.total_pnl} pnl /></div></div></Card>
      {p.cost_estimated && <p className="small muted">期初成本来自你提供的平均成本价，不能还原未录入的历史已实现收益。</p>}
      {p.daily_status === 'dividend_review' ? <Alert type="warning" title="分红待核对" description="请确认个人实际权益和方式后登记分红，除息造成的净值下降暂不作为确定亏损。" /> : p.daily_pnl == null && <Alert type="info" title="暂无可核对的日盈亏" description="需要相邻净值日期以及对应的真实持仓区间；当前市值和持仓盈亏仍可查看。" />}
      {p.source_error && <Alert type="warning" title={p.source_error} description={`保留最近有效净值：${p.nav_date || '暂无'}`} />}
      <Space wrap className="fund-toolbar">{p.can_amend && <Button onClick={() => setOperation({ kind: 'opening', amend: true })}>更正期初</Button>}{canRecord(user?.role) && <>{(['buy', 'redeem', 'cash_dividend', 'reinvest'] as const).filter((kind) => kind !== 'buy' || account.data?.account.status === 'active').map((kind) => <Button key={kind} onClick={() => setOperation({ kind })}>{FUND_EVENT_LABEL[kind]}</Button>)}</>}</Space>
      <Tabs items={[
        { key: 'events', label: '交易与资金', children: <QueryBoundary query={events}>{!events.data?.length ? <Empty /> : <div className="fund-event-list">{[...(events.data ?? [])].reverse().map((e) => <div className="panel fund-event-row" key={e.id}><div className="fund-position-heading"><div><strong>{FUND_EVENT_LABEL[e.kind]}</strong>{e.voided_at && <Tag>已撤销</Tag>}<div className="small muted">生效 {dateText(e.effective_date)} · {e.cash_date ? `资金 ${dateText(e.cash_date)}` : e.kind === 'redeem' || e.kind === 'cash_dividend' ? settled.has(e.id) ? '已登记到账' : '等待到账' : '无外部现金记录'}</div></div><div><Money value={fundMoney(e.amount)} /></div></div><div className="small muted">{e.shares} 份 · 费用 {e.fee} · {e.note}</div><Space wrap>{canRecord(user?.role) && !e.voided_at && !e.cash_date && !settled.has(e.id) && (e.kind === 'redeem' || e.kind === 'cash_dividend') && Number(e.amount) > Number(e.fee) && <Button size="small" onClick={() => setOperation({ kind: 'settlement', settlement: e })}>登记到账</Button>}{e.id === lastId && !e.voided_at && canModifyOwnedRecord(user, e.created_by) && <Popconfirm title="撤销这笔基金事件及关联资金？" onConfirm={() => void undo(e.id)} okButtonProps={{ loading: save.isPending, disabled: save.isPending }}><Button danger size="small">撤销末笔</Button></Popconfirm>}</Space></div>)}</div>}</QueryBoundary> },
        { key: 'history', label: '每日盈亏', children: <QueryBoundary query={history}><Table rowKey="id" size="small" scroll={{ x: 620 }} dataSource={history.data ?? []} pagination={{ pageSize: 20 }} columns={[{ title: '净值日期', dataIndex: 'nav_date', render: dateText }, { title: '份额', dataIndex: 'shares' }, { title: '市值', dataIndex: 'market_value', render: (v: string) => <Money value={v} /> }, { title: '区间盈亏', dataIndex: 'daily_pnl', render: (v: string | null) => <FundAmount value={v} pnl /> }, { title: '状态', dataIndex: 'status', render: (s: string) => s === 'ready' ? '可核对' : s === 'dividend_review' ? '分红待核对' : '历史不足' }]} /></QueryBoundary> },
        { key: 'dividends', label: '分红核对', children: <QueryBoundary query={dividends}>{!dividends.data?.length ? <Empty description="暂无公开分红记录" /> : dividends.data.map((d) => <Card className="panel" key={d.source_key}><p>除息 {dateText(d.ex_date)} · 权益登记 {dateText(d.registration_date)} · 每十份 {d.per_ten_cash} 元</p>{recordedDates.has(dateText(d.ex_date)) ? <Tag>个人权益已核对</Tag> : canRecord(user?.role) && <Space wrap><Button onClick={() => setOperation({ kind: 'cash_dividend', dividend: d })}>记录现金分红</Button><Button onClick={() => setOperation({ kind: 'reinvest', dividend: d })}>记录再投资</Button></Space>}</Card>)}</QueryBoundary> },
        { key: 'audit', label: '修正记录', children: <QueryBoundary query={audits}>{!audits.data?.length ? <Empty description="暂无修正记录" /> : audits.data.map((a) => <p key={a.id}>{dateText(a.created_at)} · {a.action === 'undo' ? '事件撤销' : a.action === 'amend' ? '期初更正' : '估值版本修订'}</p>)}</QueryBoundary> },
      ]} />
    </>}</QueryBoundary>
    {p && operation && <FundForm key={`${scope}:${positionId}:${operation.kind}:${operation.settlement?.id || ''}`} accountId={p.account_id} position={p} {...operation} onClose={() => setOperation(undefined)} />}
  </div>;
}
