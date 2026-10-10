import { useEffect, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Alert, App, Button, Descriptions, Empty, Modal, Skeleton, Space, Table, Tabs, Tag } from 'antd';
import dayjs from 'dayjs';
import { ArrowLeftOutlined, CreditCardOutlined, PlusOutlined } from '@ant-design/icons';
import { useLiabilities, useLiabilityDetail, useLiabilityTypes, type LiabilityBill, type InstallmentPlan, type LiabilityOperation } from '../api/liability';
import { useAuth, canRecord } from '../stores/auth';
import Money from '../components/Money';
import LiabilityDialogs, { type LiabilityDialog } from '../components/LiabilityDialogs';
import TxnFormModal, { type TxnRefill } from '../components/TxnFormModal';
import type { Transaction } from '../api/types';
import { cents, sumMoney } from '../lib/money';
import '../liability.css';
import LiabilityHistory from '../components/LiabilityHistory';
import PersonalLiabilityAccountModal from '../components/PersonalLiabilityAccountModal';

const opNames: Record<string, string> = { settings: '账单规则', posting: '资金流水', repayment: '还款', allocation: '还款分配', create_plan: '创建分期', adjust_plan: '调整期次', refund_adjustment: '退款核对', fee: '分期费用', settle: '提前结清', reverse: '撤销操作', reconcile: '账单核对' };
const planNames: Record<string, string> = { active: '进行中', needs_reconcile: '待核对', settled: '已结清', completed: '已完成' };

export default function Liabilities() {
  const navigate = useNavigate(); const { id } = useParams(); const [search, setSearch] = useSearchParams();
  const location = useLocation();
  const list = useLiabilities(); const accountId = Number(id) || 0; const detail = useLiabilityDetail(accountId);
  const types=useLiabilityTypes();
  const labels:Record<string,string>=Object.fromEntries((types.data??[]).map((t)=>[t.code,t.name]));
  const user = useAuth((s) => s.user); const writable = canRecord(user?.role); const { message } = App.useApp();
  const [dialog, setDialog] = useState<LiabilityDialog | null>(null); const [source, setSource] = useState<Transaction | null>(null);
  const [refill, setRefill] = useState<TxnRefill | null>(null); const [txnOpen, setTxnOpen] = useState(false);
  const [planFocus, setPlanFocus] = useState<number>();
  const [accountForm,setAccountForm]=useState(false);
  const closeSource=()=>{setSource(null);if(search.has('source')){const next=new URLSearchParams(search);next.delete('source');setSearch(next,{replace:true});}};
  const d = detail.data; const editable = (owner: number) => writable && (user?.role !== 'member' || owner === user.id);
  useEffect(()=>{const sourceId=Number(search.get('source'));if(sourceId&&d)setSource(d.transactions.find((t)=>t.id===sourceId)??null);},[search,d]);
  useEffect(()=>{
    const draft = (location.state as { liabilityDraft?: LiabilityDialog['draft'] } | null)?.liabilityDraft;
    if (!draft || !d || !writable) return;
    setDialog({mode:'create_plan',requestKey:crypto.randomUUID(),draft});
    navigate(location.pathname,{replace:true,state:null});
  },[d,writable,location.state,location.pathname,navigate]);
  const open = (value: Omit<LiabilityDialog, 'requestKey'>) => { setPlanFocus(undefined); setDialog({ ...value, version: value.planId ? d?.plans.find((p) => p.id === value.planId)?.version : undefined, requestKey: crypto.randomUUID() }); };
  const refund = (t: Transaction) => { setRefill({ kind: 'refund', related_txn_id: t.id, account_id: t.account_id, category_id: t.category_id ?? undefined, related_amount: t.amount, refunded: Number(t.refunded_amount ?? 0), installment_plan_id:t.installment_plan_id??undefined,liability_account_id:d?.account.id }); setTxnOpen(true); setSource(null); };
  if (!accountId) return <div className="page">
    <div className="page-head"><div><div className="eyebrow">MY CREDIT ACCOUNTS</div><h2>我的负债账户</h2><p className="small muted">仅本人可查看和管理，每个账户独立设置还款日期。</p></div><Button type="primary" icon={<PlusOutlined/>} onClick={()=>setAccountForm(true)}>新增负债账户</Button></div>
    {accountForm&&<PersonalLiabilityAccountModal onClose={()=>setAccountForm(false)} onCreated={id=>navigate(`/liabilities/${id}`)}/>}
    {list.isError ? <Alert type="error" title="负债账户查询失败" description={String(list.error)} /> : !list.data ? <Skeleton active /> : list.data.length === 0 ? <Empty description="尚未创建消费信用账户" /> : <>
      <div className="liability-summary"><div><span>总欠款</span><Money value={sumMoney(list.data.map((a) => a.total_debt))} /></div><div><span>最近应还合计</span><Money value={sumMoney(list.data.map((a) => a.current_due))} /></div><div><span>逾期应还</span><Money value={sumMoney(list.data.map((a) => a.overdue))} tone="expense" /></div></div>
      <div className="liability-accounts">{list.data.map((a) => <button type="button" className="liability-account-row" key={a.account_id} onClick={() => navigate(`/liabilities/${a.account_id}`)}>
        <span><CreditCardOutlined /> <strong>{a.name}</strong><small>{labels[a.liability_type] || '消费信用'} · {a.billing_enabled ? (a.due_date ? `${a.due_date} 到期` : '暂无已出账应还') : '待配置账单规则'}</small></span>
        <span><small>{cents(a.surplus) > 0n ? '溢缴金额' : '总欠款'}</small><Money value={cents(a.surplus) > 0n ? a.surplus : a.total_debt} /><small>本期应还 <Money value={a.current_due} /></small></span>
      </button>)}</div>
    </>}
  </div>;
  if (detail.isError) return <div className="page"><Button onClick={() => navigate('/liabilities')}>返回负债账户</Button><Alert type="error" title="账户详情查询失败" description={String(detail.error)} /></div>;
  if (!d) return <div className="page"><Skeleton active /></div>;
  const bills = new Map(d.statements.map((b) => [b.id, b])); const txns = new Map(d.transactions.map((t) => [t.id, t]));
  const activeRule = d.rules.filter((r) => r.effective_from <= dayjs().format('YYYY-MM-DD')).at(-1); const pendingRule = d.rules.find((r) => r.effective_from > dayjs().format('YYYY-MM-DD'));
  const billTitle = (b: LiabilityBill) => b.kind === 'settlement' ? `${b.cycle_end} 结清单` : `${b.cycle_start} — ${b.cycle_end}`;
  const billRows = d.statements.map((b) => ({ ...b, key: b.id }));
  const sourceButton = (sourceId: number) => <Button type="link" size="small" onClick={() => { const t = txns.get(sourceId); if (t) setSource(t); else message.info('原流水已通过合法修正删除，请查看操作记录'); }}>查看流水</Button>;
  const selectedPlan = d.plans.find((p) => p.id === planFocus);
  const planDetails = (p: InstallmentPlan) => <>
    <div className="liability-note">分期本金 <Money value={p.principal} /> · 未还本金 <Money value={sumMoney(p.terms.map((t) => t.remaining))} /> · {sourceButton(p.source_txn_id)}</div>
    <Table size="small" pagination={false} rowKey="id" dataSource={p.terms} scroll={{ x: 620 }} columns={[
      { title: '期次', dataIndex: 'number', render: (n: number) => `第 ${n} 期` },
      { title: '归属账单', render: (_, t) => bills.get(t.statement_id)?.cycle_end ?? '—' },
      { title: '本金 / 未还', render: (_, t) => <><Money value={t.principal} /><div className="muted"><Money value={t.remaining} /></div></> },
      { title: '手续费', render: (_, t) => <><Money value={t.fee_amount} /><div className="small muted">{t.fee_txn_id ? '已发生' : t.fee_status === 'cancelled' ? '已取消' : t.fee_status === 'blocked' ? '待处理' : cents(t.fee_amount) === 0n ? '无费用' : '预估'} · {t.fee_date}</div>{t.blocked_reason && <span className="liability-problem">{t.blocked_reason}</span>}{t.fee_txn_id && sourceButton(t.fee_txn_id)}</> },
    ]} />
    {editable(p.created_by) && p.status !== 'settled' && <Space wrap className="liability-toolbar"><Button onClick={() => open({ mode: 'edit_plan', planId: p.id })}>{p.status === 'needs_reconcile' ? '核对退款后的期次' : '调整未来期次 / 费用'}</Button><Button onClick={() => open({ mode: 'settle', planId: p.id })}>提前结清</Button></Space>}
  </>;
  const incoming = d.credits.filter((c) => txns.get(c.source_id)?.kind === 'transfer');
  const history = <Table rowKey="id" size="small" pagination={{ pageSize: 10 }} dataSource={incoming} scroll={{ x: 550 }} columns={[
    { title: '还款日期', dataIndex: 'entry_date' }, { title: '还款金额', render: (_, c) => <Money value={c.amount} /> }, { title: '待抵扣', render: (_, c) => <Money value={c.remaining} /> },
    { title: '分配', render: (_, c) => <>{c.mode === 'auto' ? '按到期日' : c.mode === 'settlement' ? '结清分配' : c.mode === 'hold' ? '待抵扣' : '手动指定'}{d.allocations.filter((a) => a.credit_event_id === c.id).map((a) => <div className="small muted" key={a.id}>{bills.get(d.items.find((i) => i.id === a.debt_item_id)?.statement_id ?? 0)?.cycle_end} · <Money value={a.amount} /></div>)}</> },
    { title: '操作', render: (_, c) => { const t = txns.get(c.source_id); return <Space>{sourceButton(c.source_id)}{t && !t.liability_operation_id && editable(t.created_by) && <Button size="small" onClick={() => open({ mode: 'allocation', txnId: t.id })}>调整分配</Button>}</Space>; } },
  ]} />;
  const operations = <Table rowKey="id" size="small" pagination={{ pageSize: 10 }} dataSource={d.operations} scroll={{ x: 600 }} columns={[
    { title: '时间', render: (_, o) => dayjs(o.created_at).format('MM-DD HH:mm') }, { title: '操作', render: (_, o) => <>{opNames[o.kind] || o.kind}{o.reversed_at && <Tag>已撤销</Tag>}</> },
    { title: '操作人', render: (_, o) => o.actor_id === 0 ? '系统' : o.actor_id === user?.id ? '我' : `成员 ${o.actor_id}` }, { title: '说明', dataIndex: 'reason' },
    { title: '修正', render: (_, o: LiabilityOperation) => ['settle', 'repayment'].includes(o.kind) && !o.reversed_at && editable(o.actor_id) ? <Button size="small" onClick={() => open({ mode: 'reverse', operationId: o.id })}>撤销并恢复</Button> : null },
  ]} expandable={{ expandedRowRender: (o) => <LiabilityHistory operation={o} bills={bills}/> }} />;
  return <div className="page liability-detail">
    <div className="page-head"><div><Button type="text" icon={<ArrowLeftOutlined />} onClick={() => navigate('/liabilities')}>负债账户</Button><h2>{d.account.name} <Tag>{labels[d.account.liability_type ?? ''] || '消费信用'}</Tag>{d.account.status === 'disabled' && <Tag>已停用</Tag>}</h2></div><Space wrap>{writable && d.account.billing_enabled && <Button type="primary" onClick={() => open({ mode: 'repay' })}>还款</Button>}{writable && d.account.billing_enabled && d.account.status === 'active' && <Button icon={<PlusOutlined />} onClick={() => open({ mode: 'create_plan' })}>记分期</Button>}<Button onClick={()=>setAccountForm(true)}>账户设置</Button></Space></div>
    {accountForm&&<PersonalLiabilityAccountModal detail={d} onClose={()=>setAccountForm(false)} onCreated={()=>{}}/>}
    {!d.account.billing_enabled ? <Alert type="info" title="请设置此账户的账单日和还款日" description={<Button onClick={()=>setAccountForm(true)}>设置我的账户</Button>} /> : <>
      <div className="liability-summary"><div><span>{cents(d.summary.surplus) > 0n ? '溢缴金额' : '总欠款'}</span><Money value={cents(d.summary.surplus) > 0n ? d.summary.surplus : d.summary.total_debt} /></div><div><span>本期应还 {d.summary.due_date && `· ${d.summary.due_date}`}</span><Money value={d.summary.current_due} /></div><div><span>逾期应还</span><Money value={d.summary.overdue} tone="expense" /></div></div>
      <div className="liability-breakdown"><span>未出账 <Money value={d.summary.unbilled} /></span><span>其中后续分期本金 <Money value={d.summary.future_principal} /></span><span>待抵扣资金 <Money value={d.summary.pending_credit} /></span><span>未来手续费预估 <Money value={d.summary.forecast_fees} /></span></div>
      <p className="small muted">消费时计支出，本金还款记转账。后续分期本金包含在未出账金额内；预估手续费尚未计入欠款和支出。</p>
      {activeRule && <p className="small muted">账单日每月 {activeRule.statement_day} 日 · {activeRule.repayment_month_offset ? '次月' : '当月'} {activeRule.repayment_day} 日还款 · 短月份取月底{pendingRule && ` · 新规则自 ${pendingRule.effective_from} 生效`}</p>}
      {d.plans.some((p) => p.status === 'needs_reconcile' || p.terms.some((t) => t.fee_status === 'blocked')) && <Alert type="warning" title="部分分期需要核对或处理费用" description="请在分期计划中查看原因并确认剩余期次，账单核对完成前会保留提示。" />}
      <Tabs defaultActiveKey={search.get('plan') ? 'plans' : 'bills'} items={[
        { key: 'bills', label: '账单', children: <Table rowKey="id" size="small" dataSource={billRows} pagination={{ pageSize: 8 }} scroll={{ x: 650 }} columns={[
          { title: '账单周期', render: (_, b) => <>{billTitle(b)}<div className="small muted">{b.issued_at ? b.reconciled_at ? '已核对' : '待核对' : '未出账'}</div></> },
          { title: '账单金额', render: (_, b) => <Money value={b.amount} /> }, { title: '已还', render: (_, b) => <Money value={b.paid_amount} /> },
          { title: '剩余应还', render: (_, b) => <Money value={b.remaining} tone={b.due_date < dayjs().format('YYYY-MM-DD') && b.issued_at ? 'expense' : 'plain'} /> },
          { title: '还款日', dataIndex: 'due_date' }, { title: '操作', render: (_, b) => writable && b.kind === 'monthly' ? <Button size="small" onClick={() => open({ mode: 'reconcile', billId: b.id })}>核对 / 调整</Button> : null },
        ]} expandable={{ defaultExpandedRowKeys: search.get('bill') ? [Number(search.get('bill'))] : [], expandedRowRender: (b) => <><p className="small muted">出账快照 <Money value={b.snapshot_amount} /> · 当前账单 <Money value={b.amount} />{b.reference_amount !== null && <> · 平台参考 <Money value={b.reference_amount} /></>}</p><Table rowKey="id" size="small" pagination={false} dataSource={d.items.filter((i) => i.statement_id === b.id)} columns={[{ title: '项目', render: (_, i) => <>{i.kind === 'fee' ? '手续费' : i.plan_id ? '分期本金' : '消费本金'}{i.plan_id && <Button type="link" size="small" onClick={() => setPlanFocus(i.plan_id!)}>查看分期</Button>}</> }, { title: '金额', render: (_, i) => <Money value={i.amount} /> }, { title: '未还', render: (_, i) => <Money value={i.remaining} /> }, { title: '来源', render: (_, i) => sourceButton(i.source_id) }]} /></> }} /> },
        { key: 'plans', label: '分期计划', children: d.plans.length ? <div className="liability-plans">{d.plans.map((p) => <section key={p.id} className="liability-plan"><h3>分期 {p.id} · {p.term_count} 期 <Tag color={p.status === 'needs_reconcile' ? 'orange' : undefined}>{planNames[p.status] || p.status}</Tag></h3>{planDetails(p)}</section>)}</div> : <Empty description="暂无分期计划" /> },
        { key: 'payments', label: '还款与抵扣', children: history }, { key: 'operations', label: '操作记录', children: operations },
      ]} />
    </>}
    {dialog&&<LiabilityDialogs detail={d} dialog={dialog} onClose={() => setDialog(null)} onAddExpense={() => { setRefill({ kind: 'expense', account_id: d.account.id }); setTxnOpen(true); }} />}
    <Modal open={!!source} title="资金流水" onCancel={closeSource} footer={null}>{source && <><Descriptions column={1} items={[{ key: 'kind', label: '类型', children: source.kind === 'transfer' ? '转账还款' : source.kind === 'refund' ? '退款' : source.kind === 'income' ? '收入' : source.liability_term_id || source.liability_operation_id ? '手续费支出' : '消费支出' }, { key: 'amount', label: '金额', children: <Money value={source.amount} /> }, { key: 'date', label: '实际日期', children: dayjs(source.entry_date).format('YYYY-MM-DD') }, { key: 'period', label: '收支账期', children: source.period_key }, { key:'bill',label:'负债账单归属',children:d.items.filter((i)=>i.source_id===source.id).map((i)=>bills.get(i.statement_id)?.cycle_end).filter(Boolean).join(' / ') || (source.kind==='expense'?'费用由资产直接支付，未计入负债应还':'查看还款与抵扣记录') }, { key: 'note', label: '备注', children: source.note || '—' }]} />{writable && source.kind === 'expense' && (!source.purpose || source.purpose === 'regular') && <Button onClick={() => refund(source)}>记录实际退款</Button>}</>}</Modal>
    <Modal open={!!selectedPlan} title="分期明细" onCancel={() => setPlanFocus(undefined)} footer={null} width={760}>{selectedPlan && planDetails(selectedPlan)}</Modal>
    <TxnFormModal open={txnOpen} onClose={() => setTxnOpen(false)} refill={refill} />
  </div>;
}
