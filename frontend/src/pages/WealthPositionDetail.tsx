import ManualNAVForm from '../features/wealth/ManualNAVForm';
import WealthOrders, { WealthOrderEventInfo } from '../features/wealth/WealthOrders';
import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { Alert, App, Button, Card, Empty, Popconfirm, Space, Table, Tabs, Tag } from 'antd';
import { useWealthAccount, useWealthAudits, useWealthEvents, useWealthMutations, useWealthPosition, useWealthValuations, type WealthEvent, type WealthEventKind, type WealthManual, useWealthQuotes, useWealthManuals, useWealthManualAudits } from '../api/wealth';
import { useAuth, canRecord } from '../stores/auth';
import { canModifyOwnedRecord } from '../domain/permissions';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import { QueryBoundary } from '../components/QueryState';
import Money from '../components/Money';
import WealthForm, { dateText, WEALTH_EVENT_LABEL } from '../features/wealth/WealthForm';
import { WealthAmount } from './Wealth';
import { wealthMoney } from '../domain/wealth';
import '../styles/wealth.css';

export default function WealthPositionDetail() {
  const positionId = Number(useParams().id || 0);
  const focusedEvent = Number(new URLSearchParams(useLocation().search).get('event') || 0);
  const navigate = useNavigate();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const user = useAuth((s) => s.user);
  const { message } = App.useApp();
  const position = useWealthPosition(positionId);
  const account = useWealthAccount(position.data?.account_id || 0);
  const events = useWealthEvents(positionId);
  useEffect(() => { if (focusedEvent && events.data) document.getElementById(`wealth-event-${focusedEvent}`)?.scrollIntoView({ block: 'center' }); }, [focusedEvent, events.data]);
  const history = useWealthValuations(positionId);
  const audits = useWealthAudits(positionId);
  const { save } = useWealthMutations();
  const undoKeys = useRef(new Map<number, string>());
  const [operation, setOperation] = useState<{ kind: WealthEventKind; amend?: boolean; settlement?: WealthEvent }>();
  useEffect(() => { setOperation(undefined); undoKeys.current.clear(); }, [scope, positionId]);
  const p = position.data;
  const quotes=useWealthQuotes(p?.product_id || 0);
  const manuals=useWealthManuals(p?.product_id || 0);
  const manualAudits=useWealthManualAudits(p?.product_id || 0);
  const [manualAction,setManualAction]=useState<{record?: WealthManual;withdraw?: boolean}>();
  useEffect(()=>setManualAction(undefined),[scope,positionId]);
  const settled = new Set((events.data ?? []).filter((e) => !e.voided_at && e.source_event_id).map((e) => e.source_event_id));
  const ordered = (events.data ?? []).filter((e) => !e.voided_at).sort((a, b) => dateText(a.effective_date).localeCompare(dateText(b.effective_date)) || a.id - b.id);
  const lastId = ordered.at(-1)?.id;
  const undo = async (eventId: number) => { if (save.isPending) return; let key = undoKeys.current.get(eventId); if (!key) { key = crypto.randomUUID(); undoKeys.current.set(eventId, key); } try { await save.mutateAsync({ path: `/wealth/positions/${positionId}/events/${eventId}/undo`, input: { request_key: key } }); if (alive.current && currentScope.current === scope) message.success('事件已撤销'); } catch (e) { if (alive.current && currentScope.current === scope) message.error(errMsg(e)); } };
  return <div className="page">
    <div className="page-head"><div><div className="eyebrow">WEALTH POSITION · {p?.code || ''}</div><h2>{p?.name || '理财持仓'}</h2></div><Button onClick={() => navigate(p ? `/wealth/${p.account_id}` : '/wealth')}>返回账户</Button></div>
    <QueryBoundary query={position}>{p && <>
      <section className="panel wealth-overview"><div><div className="muted">持仓市值</div><div className="wealth-primary-amount"><WealthAmount value={p.market_value} /></div></div><div><div className="muted">持仓盈亏</div><WealthAmount value={p.holding_pnl} pnl /><div className="small muted">{p.holding_return == null ? '收益率暂无' : `${p.holding_return}%`}</div></div><div><div className="muted">最近净值区间盈亏</div><WealthAmount value={p.daily_pnl} pnl /><div className="small muted">{p.daily_start && p.daily_end ? `${p.daily_start} → ${p.daily_end}` : '暂无可核对的日盈亏'}</div></div></section>
      <p className="muted">{p.product.name} · {p.product.registration_code} · {p.product.currency}</p><p className="muted">{p.code} · 单位净值 {p.unit_nav || '暂无'} · 净值截至 {p.nav_date || '暂无'} · 持仓生效 {p.effective_date || '暂无'}</p>
      <Card className="panel"><div className="wealth-values"><div><span className="muted">确认份额</span><strong>{p.shares}</strong></div><div><span className="muted">单位成本</span><strong>{p.cost_price}</strong></div><div><span className="muted">剩余成本</span><Money value={p.cost} /></div><div><span className="muted">已实现赎回盈亏</span><WealthAmount value={p.realized_pnl} pnl /></div><div><span className="muted">分红收益</span><Money value={p.dividends} /></div><div><span className="muted">投资费用（已计成本或净额）</span><Money value={p.fees} /></div><div><span className="muted">待结算资金</span><Money value={p.pending_funds} /></div><div><span className="muted">累计投资盈亏</span><WealthAmount value={p.total_pnl} pnl /></div></div></Card>
      {p.cost_estimated && <p className="small muted">期初成本来自你提供的平均成本价，不能还原未录入的历史已实现收益。</p>}
      {p.daily_status === 'dividend_review' ? <Alert type="warning" title="分红待核对" description="请确认个人实际权益和方式后登记分红，除息造成的净值下降暂不作为确定亏损。" /> : p.daily_pnl == null && <Alert type="info" title="暂无可核对的日盈亏" description="需要相邻净值日期以及对应的真实持仓区间；当前市值和持仓盈亏仍可查看。" />}
      <p className="muted small">净值来源：{p.quote?.source === 'manual' ? '本人补录' : p.quote ? '自动同步' : '暂无'} · 最近尝试 {p.product.last_attempt ? new Date(p.product.last_attempt).toLocaleString('zh-CN') : '尚未查询'} · 最近成功 {p.product.last_success ? new Date(p.product.last_success).toLocaleString('zh-CN') : '暂无'} · {syncLabel(p.product.sync_status)}{p.rebuild_pending && ' · 历史估值待重建，当前按有效净值计算'}</p>{p.source_error && <Alert type="warning" title={p.source_error} description={`保留最近有效净值：${p.nav_date || '暂无'}`} />}
      <Space wrap className="wealth-toolbar">{p.can_amend && <Button onClick={() => setOperation({ kind: 'opening', amend: true })}>更正期初</Button>}{canRecord(user?.role) && <>{(['buy', 'redeem', 'cash_dividend', 'reinvest'] as const).filter((kind) => kind !== 'buy' || (account.data?.account.status === 'active' && p.product.admission_status === 'enabled')).map((kind) => <Button key={kind} onClick={() => setOperation({ kind })}>{WEALTH_EVENT_LABEL[kind]}</Button>)}</>}</Space>
      <h3>申购与转出订单</h3>
      <WealthOrders key={`${scope}:${positionId}`} position={p} events={events.data || []} active={account.data?.account.status === 'active'} />
      <Tabs defaultActiveKey="events" items={[
        { key: 'events', label: '交易与资金', children: <QueryBoundary query={events}>{!events.data?.length ? <Empty /> : <div className="wealth-event-list">{[...(events.data ?? [])].reverse().map((e) => <div id={`wealth-event-${e.id}`} className={`panel wealth-event-row${focusedEvent === e.id ? ' is-located' : ''}`} key={e.id}><div className="wealth-position-heading"><div><strong>{WEALTH_EVENT_LABEL[e.kind]}</strong>{e.voided_at && <Tag>已撤销</Tag>}<div className="small muted">生效 {dateText(e.effective_date)} · {e.cash_date ? `资金 ${dateText(e.cash_date)}` : e.kind === 'redeem' || e.kind === 'confirm_redeem' || e.kind === 'cash_dividend' ? settled.has(e.id) ? '已登记到账' : '等待到账' : '无外部现金记录'}</div></div><div><Money value={wealthMoney(e.amount)} /></div></div><div className="small muted">{e.shares} 份 · 费用 {e.fee} · {e.note}</div><WealthOrderEventInfo event={e} /><Space wrap>{canRecord(user?.role) && !e.voided_at && !e.cash_date && !settled.has(e.id) && (e.kind === 'redeem' || e.kind === 'confirm_redeem' || e.kind === 'cash_dividend') && Number(e.amount) > Number(e.fee) && <Button size="small" onClick={() => setOperation({ kind: 'settlement', settlement: e })}>登记到账</Button>}{e.id === lastId && !e.voided_at && canModifyOwnedRecord(user, e.created_by) && <Popconfirm title="撤销这笔理财事件及关联资金？" onConfirm={() => void undo(e.id)} okButtonProps={{ loading: save.isPending, disabled: save.isPending }}><Button danger size="small">撤销末笔</Button></Popconfirm>}</Space></div>)}</div>}</QueryBoundary> },
        { key: 'history', label: '每日盈亏', children: <QueryBoundary query={history}><Table rowKey="id" size="small" scroll={{ x: 620 }} dataSource={history.data ?? []} pagination={{ pageSize: 20 }} columns={[{ title: '净值日期', dataIndex: 'nav_date', render: dateText }, { title: '份额', dataIndex: 'shares' }, { title: '市值', dataIndex: 'market_value', render: (v: string) => <Money value={v} /> }, { title: '区间盈亏', dataIndex: 'daily_pnl', render: (v: string | null) => <WealthAmount value={v} pnl /> }, { title: '状态', dataIndex: 'status', render: (s: string) => s === 'ready' ? '可核对' : s === 'dividend_review' ? '分红待核对' : '历史不足' }]} /></QueryBoundary> },
        { key: 'navs', label: '净值与本人补录', children: <>
          <Alert type="info" title="本人补录仅补缺失日期" description="本人所有账户共用该产品补值，其他账号不受影响；自动同日值优先，单位净值用于估值。分红请按银行权益另外核对登记。" />
          <Button onClick={() => setManualAction({})}>补录缺失净值</Button>
          <h3>有效净值历史</h3><QueryBoundary query={quotes}><Table rowKey={(q) => `${q.source}:${q.record_id}`} size="small" scroll={{x: 560}} dataSource={[...(quotes.data ?? [])].reverse()} pagination={{pageSize: 20}} columns={[{title:'净值日期',dataIndex:'nav_date',render:dateText},{title:'单位净值',dataIndex:'unit_nav'},{title:'累计净值（参考）',dataIndex:'total_nav',render:(v:string|null)=>v || '暂无'},{title:'来源',dataIndex:'source',render:(v:string)=>v==='manual'?'本人补录':'自动同步'}]} /></QueryBoundary>
          <h3>本人补录记录</h3><QueryBoundary query={manuals}>{!manuals.data?.length ? <Empty description="暂无本人补录" /> : [...manuals.data].reverse().map((n) => <Card className="panel" key={n.id}><Space wrap><strong>{dateText(n.nav_date)} · {n.unit_nav}</strong><Tag>{n.automatic_replaced?'自动值已优先采用':n.withdrawn?'已撤回':'本人补录'}</Tag></Space><p className="muted small">{n.source_note} · 第 {n.revision} 版</p>{!n.automatic_replaced && <Space wrap><Button onClick={() => setManualAction({record:n})}>{n.withdrawn?'重新补录':'更正'}</Button>{!n.withdrawn && <Button danger onClick={() => setManualAction({record:n,withdraw:true})}>撤回补录</Button>}</Space>}</Card>)}</QueryBoundary>
          <h3>补录及来源切换审计</h3><QueryBoundary query={manualAudits}>{manualAudits.data?.map((a) => <p className="small" key={a.id}>{dateText(a.created_at)} · {auditLabel(a.action)} · {manualAuditText(a.before,a.after)}</p>)}</QueryBoundary>
        </> },
        { key: 'audit', label: '修正记录', children: <QueryBoundary query={audits}>{!audits.data?.length ? <Empty description="暂无修正记录" /> : audits.data.map((a) => <p key={a.id}>{dateText(a.created_at)} · {auditLabel(a.action)}</p>)}</QueryBoundary> },
      ]} />
    </>}</QueryBoundary>
    {p && manualAction && <ManualNAVForm key={`${scope}:${p.product_id}:${manualAction.record?.id || 0}:${!!manualAction.withdraw}`} productId={p.product_id} {...manualAction} onClose={() => setManualAction(undefined)} />}
    {p && operation && <WealthForm key={`${scope}:${positionId}:${operation.kind}:${operation.settlement?.id || ''}`} accountId={p.account_id} position={p} {...operation} onClose={() => setOperation(undefined)} />}
  </div>;
}

function auditLabel(action: string) { return ({create:'补录创建',amend:'更正',withdraw:'撤回补录',automatic_takeover:'自动值优先采用',undo:'事件撤销',opening:'登记期初',first_buy:'首次申购',event:'记录实际事件',revalue:'估值版本修订',valuation_remove:'清理失效估值日期'} as Record<string,string>)[action] || action; }
function manualAuditText(before: string,after: string) { try {const a=JSON.parse(before || 'null') as {unit_nav?:string;nav_date?:string}|null;const b=JSON.parse(after || 'null') as {unit_nav?:string;nav_date?:string}|null;return `${dateText(b?.nav_date || a?.nav_date)} 净值 ${a?.unit_nav || '无'} → ${b?.unit_nav || '无'}`;}catch{return '资料已留档';} }
function syncLabel(status:string){return ({pending:'等待首次查询',updated:'已更新',no_new_nav:'检查成功，暂无新净值',failed:'查询失败',refreshing:'正在查询'} as Record<string,string>)[status] || status;}
