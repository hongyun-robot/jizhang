import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Empty, Modal, Popconfirm, Space, Table, Tag } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { useGoldAccount, useGoldAccounts, useGoldMutations, useGoldValuations, type GoldKind, type GoldValuation } from '../api/gold';
import { useScope } from '../api/keys';
import { useAuth } from '../stores/auth';
import { canRecord, canModifyOwnedRecord } from '../domain/permissions';
import { errMsg } from '../api/client';
import { QueryBoundary } from '../components/QueryState';
import AccountEditor from '../components/AccountEditor';
import Money from '../components/Money';
import GoldForm, { GOLD_LABEL } from '../features/gold/GoldForm';
import GoldQuotePanel, { quoteTime } from '../features/gold/GoldQuotePanel';
import { useEditorSession } from '../hooks/useEditorSession';
import '../styles/gold.css';

export function GoldMoney({ value }: { value: string | null }) { return value === null ? <span className="muted">待报价</span> : <Money value={value} />; }
export default function Gold() {
  const id = Number(useParams().id ?? 0);
  const navigate = useNavigate();
  const user = useAuth((state) => state.user);
  const scope = useScope();
  const editor = useEditorSession(true, `${id}`);
  const list = useGoldAccounts();
  const detail = useGoldAccount(id);
  const valuations = useGoldValuations(id);
  const { save } = useGoldMutations();
  const { message } = App.useApp();
  const [creating, setCreating] = useState(false);
  const [operation, setOperation] = useState<{ kind: GoldKind; amend?: boolean } | null>(null);
  useEffect(() => { setOperation(null); setCreating(false); }, [scope, id]);
  const undoKeys = useRef(new Map<number, string>());
  const selected = detail.data;
  const last = selected?.events.filter((event) => !event.voided_at).at(-1);
  const undo = async (eventId: number) => {
    if (save.isPending) return;
    let key = undoKeys.current.get(eventId); if (!key) { key = crypto.randomUUID(); undoKeys.current.set(eventId, key); }
    const current = editor.capture();
    try { await save.mutateAsync({ path: `/gold/accounts/${id}/events/${eventId}/undo`, input: { request_key: key } }); if (current()) message.success('黄金事件及关联现金已撤销'); }
    catch (e) { if (current()) message.error(errMsg(e)); }
  };
  return <div className="page gold-page">
    <div className="page-head"><div><div className="eyebrow">GOLD · 建设银行个人黄金积存</div><h2>{id ? selected?.account.name ?? '黄金账户' : '建行积存金'}</h2></div>{id ? <Button onClick={() => navigate('/gold')}>账户列表</Button> : <Button type="primary" onClick={() => setCreating(true)}>新建黄金账户</Button>}</div>
    {!id ? <QueryBoundary query={list}>{!list.data?.length ? <Empty description="还没有黄金账户，可先建立账户，再录入持有克数和成本。" /> : <div className="gold-account-list">{list.data.map((row) => <section className="panel gold-account-summary" key={row.account.id}><Button type="link" onClick={() => navigate(`/gold/${row.account.id}`)}>{row.account.name}</Button>{row.account.status === 'disabled' && <Tag>停用</Tag>}<p>{row.grams} 克 · 平均成本 {row.cost_price ?? '—'} 元/克</p><div className="gold-amount"><GoldMoney value={row.market_value} /></div><p className="muted small">按建行赎回价估值</p><Button onClick={() => navigate(`/gold/${row.account.id}`)}>查看黄金持仓</Button></section>)}</div>}</QueryBoundary> : <QueryBoundary query={detail}>{selected && <>
      <section className="panel gold-overview" aria-label="黄金持仓概览">
        <div><span className="muted">持有克数</span><strong data-testid="gold-grams">{selected.grams} 克</strong></div>
        <div><span className="muted">当前市值 · 赎回价</span><strong data-testid="gold-value"><GoldMoney value={selected.market_value} /></strong></div>
        <div><span className="muted">剩余成本</span><strong data-testid="gold-cost"><Money value={selected.cost} /></strong></div>
        <div><span className="muted">平均每克成本</span><strong>{selected.cost_price ?? '—'} 元</strong></div>
        <div><span className="muted">持仓盈亏</span><strong data-testid="gold-holding-pnl"><GoldMoney value={selected.holding_pnl} /></strong></div>
        <div><span className="muted">已实现盈亏</span><strong data-testid="gold-realized-pnl"><Money value={selected.realized_pnl} /></strong></div>
        <div><span className="muted">累计盈亏</span><strong><GoldMoney value={selected.total_pnl} /></strong></div>
        <div><span className="muted">已单列费用</span><strong><Money value={selected.fees} /></strong></div>
      </section>
      {selected.cost_estimated && <p className="small muted">含期初历史成本，累计盈亏按录入成本估算。费用已在成本或净到账中体现，不再次扣减。</p>}
      {selected.status === 'no_price' && <Alert type="warning" title="持仓已保存，市值待报价；总资产估值不完整。" />}
      <GoldQuotePanel />
      <Space wrap className="gold-actions">
        {selected.account.status === 'active' && !selected.events.some((event) => !event.voided_at) && <Button onClick={() => setOperation({ kind: 'opening' })}>录入已有持仓</Button>}
        {selected.can_amend && selected.account.status === 'active' && <Button onClick={() => setOperation({ kind: 'opening', amend: true })}>更正期初</Button>}
        {canRecord(user?.role) && <>{selected.account.status === 'active' && <Button type="primary" onClick={() => setOperation({ kind: 'buy' })}>买入黄金</Button>}<Button disabled={Number(selected.grams) <= 0} onClick={() => setOperation({ kind: 'sell' })}>卖出黄金</Button></>}
      </Space>
      <p className="muted small">登记已完成的真实交易，资金与克数同日生效。买卖是投资转账，不进入生活收支和预算。</p>
      <h3>持仓与资金记录</h3>
      <div className="gold-events">{[...selected.events].reverse().map((event) => <section className="gold-event" key={event.id}><div><strong>{GOLD_LABEL[event.kind]}</strong> {event.voided_at && <Tag>已撤销</Tag>}<div className="small muted">{event.trade_date.slice(0, 10)} · {event.grams} 克 · {event.note}</div></div><div><Money value={event.cash_amount} /><div className="small muted">{event.kind === 'opening' ? '期初总成本' : event.kind === 'buy' ? '实际总扣款' : '实际净到账'} · 费用 {event.fee ?? '未单列'}</div></div>{event.id === last?.id && canRecord(user?.role) && canModifyOwnedRecord(user, event.created_by) && <Popconfirm title="撤销此末笔事件及关联现金？" onConfirm={() => void undo(event.id)} okButtonProps={{ loading: save.isPending, disabled: save.isPending }}><Button danger size="small">撤销末笔</Button></Popconfirm>}</section>)}</div>
      <h3>每日参考市值</h3><p className="muted small">历史缺少当日保存的报价时保留缺口，不能用今天的价格倒算。</p>
      <Table rowKey="id" size="small" dataSource={valuations.data ?? []} pagination={{ pageSize: 10 }} columns={[{ title: '日期 / 银行报价', dataIndex: 'date', render: (value: string, row: GoldValuation) => <><span>{value.slice(0, 10)}</span>{row.quote_at && <div className="small muted" style={{ overflowWrap: 'anywhere' }}>源 {quoteTime(row.quote_at)}</div>}</> }, { title: '克数', dataIndex: 'grams' }, { title: '市值', dataIndex: 'market_value', render: (value: string | null) => <GoldMoney value={value} /> }]} />
    </>}</QueryBoundary>}
    {creating && <AccountEditor goldOnly key={`gold-${scope}`} onClose={() => setCreating(false)} onCreated={(a) => { setCreating(false); navigate(`/gold/${a.id}`); }} />}
    {operation && selected && <Modal key={`${scope}:${id}:${operation.kind}:${operation.amend}`} open title={operation.amend ? '更正黄金期初' : GOLD_LABEL[operation.kind]} footer={null} onCancel={() => setOperation(null)} styles={{ body: { maxHeight: '70dvh', overflowY: 'auto' } }}><GoldForm kind={operation.kind} open account={selected} amend={operation.amend} onSaved={() => setOperation(null)} /></Modal>}
  </div>;
}
