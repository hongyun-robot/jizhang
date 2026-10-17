import { useRef, useState } from 'react';
import { Alert, App, Button, Card, Collapse, Empty, Modal, Select, Space, Tag } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { useWealthCashAccount, useWealthCashMutations, useWealthProducts, type WealthCashAccount } from '../api/wealth';
import { errMsg } from '../api/client';
import DatePicker from '../components/DatePicker';
import Money from '../components/Money';
import { QueryBoundary } from '../components/QueryState';
import WealthCashYieldDialog from '../features/wealth/WealthCashYieldDialog';
import { canRecord } from '../domain/permissions';
import { useAuth } from '../stores/auth';
import '../styles/wealth.css';

function Configure({ value, onClose }: { value: WealthCashAccount; onClose: () => void }) {
  const products = useWealthProducts();
  const { configure } = useWealthCashMutations();
  const { message } = App.useApp();
  const [product, setProduct] = useState<number | undefined>(value.link?.product_id);
  const [date, setDate] = useState(dayjs().subtract(1, 'day'));
  const key = useRef({ payload: '', key: '' });
  const revision = useRef(value.link?.revision ?? 0);
  const busy = useRef(false);
  const save = async () => {
    if (busy.current) return;
    if (!product) { message.error('请选择理财产品及份额'); return; }
    const payload = { product_id: product, start_date: date.format('YYYY-MM-DD'), active: true, expected_revision: revision.current };
    const encoded = JSON.stringify(payload);
    if (key.current.payload !== encoded) key.current = { payload: encoded, key: crypto.randomUUID() };
    busy.current = true;
    try { await configure.mutateAsync({ id: value.account.id, input: { ...payload, request_key: key.current.key } }); message.success('已关联，现有定时同步将自动记入收益'); onClose(); }
    catch (error) { message.error(errMsg(error)); }
    finally { busy.current = false; }
  };
  return <Modal open title={value.link?.active ? '调整理财零钱关联' : '启用理财零钱'} onCancel={() => { if (!configure.isPending) onClose(); }} onOk={() => void save()} confirmLoading={configure.isPending} destroyOnHidden>
    <div className="wealth-cash-form">
      <label>理财产品及份额<Select aria-label="关联理财产品" value={product} placeholder="选择产品及份额" loading={products.isLoading} options={(products.data ?? []).map((p) => ({ value: p.id, label: `${p.name} · ${p.code}` }))} onChange={setProduct} /></label>
      <label>起算基准日<DatePicker aria-label="理财零钱基准日" value={date} allowClear={false} disabledDate={(d) => d.isAfter(dayjs(), 'day')} onChange={(d) => { if (d) setDate(d); }} /></label>
      <Alert type="info" showIcon title="基准日结束时的账户余额按已确认计息资金处理，从下一日起估算收益。请选择余额已经核对且资金已确认的日期。" description="保留账户编号、期初余额和原流水。此账户可继续记收入、支出和转账；理财页展示同一份资产。调整关联时，基准日不得早于最后一笔收益日。" />
    </div>
  </Modal>;
}

export default function WealthCash() {
  const id = Number(useParams().id);
  const query = useWealthCashAccount(id);
  const { configure, refresh } = useWealthCashMutations();
  const { message, modal } = App.useApp();
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const [setup, setSetup] = useState(false);
  const [editing, setEditing] = useState<{ id: number; balance?: string } | null>(null);
  const value = query.data;
  const writable = canRecord(user?.role) && value?.account.owner_id === user?.id;
  const latest = value?.yields.at(-1);
  const reload = async () => { try { await refresh.mutateAsync(id); message.success('已同步并核对收益'); } catch (error) { message.error(errMsg(error)); } };
  const stop = () => {
    if (!value?.link) return;
    const link = value.link;
    modal.confirm({ title: '停止自动收益？', content: '已入账的收益和历史记录会保留，之后按普通现金账户记账。', onOk: async () => {
      try { await configure.mutateAsync({ id, input: { request_key: crypto.randomUUID(), expected_revision: link.revision, product_id: link.product_id, start_date: link.start_date.slice(0, 10), active: false } }); message.success('已停止自动收益'); }
      catch (error) { message.error(errMsg(error)); throw error; }
    } });
  };
  return <div className="page wealth-page">
    <div className="page-head"><div><div className="eyebrow">理财零钱</div><h2>{value?.account.name ?? '账户详情'}</h2></div><Space wrap><Button onClick={() => navigate('/accounts')}>账户管理</Button>{value?.link?.active && writable && <Button loading={refresh.isPending} onClick={() => void reload()}>同步收益</Button>}</Space></div>
    <QueryBoundary query={query}>{value && <>
      <section className="panel wealth-overview"><div><div className="muted">可用余额</div><div className="wealth-primary-amount" data-testid="wealth-cash-balance"><Money value={value.account.balance ?? '0'} /></div></div><div><div className="muted">已入账理财收益</div><Money value={value.total_yield} /></div><div><div className="muted">最近净值</div><span>{value.unit_nav ?? '暂无'}<small className="muted"> · {value.nav_date || '等待披露'}</small></span></div></section>
      {!value.link?.active ? <Card className="panel"><Empty description={value.link ? '自动收益已停止' : '此账户尚未关联理财产品'} />{writable && <Button type="primary" onClick={() => setSetup(true)}>启用理财零钱</Button>}</Card> : <>
        <Card className="panel"><div className="wealth-position-heading"><div><strong>{value.product?.name}</strong><p className="muted small">{value.product?.code} · 登记编码 {value.product?.registration_code}</p></div><Tag>可直接收支</Tag></div>
          <div className="wealth-values"><div><span className="muted">已确认计收益资金（估算）</span><Money value={value.confirmed_amount} /></div><div><span className="muted">待确认资金（估算）</span><Money value={value.pending_amount} /></div><div><span className="muted">未再申购现金</span><Money value={value.uninvested_amount} /></div></div>
          <p className="small muted">估算持有 {value.estimated_shares ?? '—'} 份 · 基准日 {value.link.start_date.slice(0, 10)} · 本账户已计入总资产一次</p>
          <Alert type={['blocked_closed', 'dividend_review'].includes(value.link.sync_status) ? 'warning' : 'info'} showIcon title={value.link.last_error || (value.product?.last_error ? `净值源暂未更新：${value.product.last_error}` : '支出默认快速转出，当天停止对应收益；转入资金确认后才计收益。')} description={value.link.policy === 'yulibao_20250715' ? <>确认日期未填写时，按披露净值日期及 15:00 截点估算下一交易日确认，可在流水中修改。未披露日期的区间收益待净值发布后统一入账。<a href="https://render.mybank.cn/p/yuyan/180020590000000085/index.html?chInfo=tabs" target="_blank" rel="noreferrer">查看转入转出规则</a></> : '转入默认次日确认，可在流水填写实际确认日。净值跨日时按资金变化估算区间收益，实际以机构确认为准。'} />
          {writable && <Space wrap className="wealth-cash-actions"><Button onClick={() => setSetup(true)}>调整关联</Button><Button onClick={stop} loading={configure.isPending}>停止自动收益</Button></Space>}
        </Card>
      </>}
      <Card className="panel"><div className="wealth-position-heading"><h3>收益明细</h3>{writable && latest && <Button onClick={() => setEditing({ id: latest.id, balance: value.account.balance ?? '0' })}>校准余额</Button>}</div>
        <p className="muted small">每天随已披露净值自动核对，正负收益均影响账户余额。收益流水不计入生活收支和预算；金额为 0 时保留明细。付款失败退回的资金不会自动再次申购。</p>
        {!value.yields.length ? <Empty description="等待基准日后的净值披露" /> : <div className="wealth-cash-yields">{[...value.yields].reverse().map((row) => <div className="wealth-cash-yield" key={row.id}>
          <div><strong>{row.nav_date.slice(0, 10)}</strong><div className="small muted">{(row.manual_start_date ?? row.previous_nav_date).slice(0, 10)} → {row.nav_date.slice(0, 10)}</div>{row.manual_override && <Tag>手动收益</Tag>}{row.status === 'manual_interval' && <Tag>已含在手动区间收益中</Tag>}{row.status === 'blocked_closed' && <Tag color="warning">封账待核对</Tag>}{row.status === 'dividend_review' && <Tag color="warning">分红待核对</Tag>}</div>
          <div><Money value={row.amount} /><div className="small muted">自动估算 ¥{row.calculated_amount}</div></div>
          {writable && <Button onClick={() => setEditing({ id: row.id })}>修改收益</Button>}
        </div>)}</div>}
      </Card>
      {!!value.audits.length && <Collapse items={[{ key: 'history', label: `调整记录（${value.audits.length}）`, children: <div className="wealth-cash-yields">{value.audits.slice(0, 30).map((audit) => <div className="wealth-cash-yield" key={audit.id}><span>{audit.action === 'manual' ? '手动修改收益或校准余额' : audit.action === 'creation' ? '新建理财零钱' : audit.action === 'configuration' ? '调整账户关联' : '按净值核对收益'}</span><span className="muted small">{dayjs(audit.created_at).format('YYYY-MM-DD HH:mm')}</span></div>)}</div> }]} />}
    </>}</QueryBoundary>
    {setup && value && <Configure value={value} onClose={() => setSetup(false)} />}
    {editing && <WealthCashYieldDialog key={editing.id + ':' + (editing.balance ?? 'yield')} {...editing} onClose={() => setEditing(null)} />}
  </div>;
}
