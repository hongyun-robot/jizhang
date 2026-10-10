import { useEffect, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import { Alert, App, Button, Checkbox, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table } from 'antd';
import { useAccounts, useCategoryForest } from '../api/hooks';
import { rootsOfKind } from '../api/category';
import { liabilityWrite, useLiabilityWrite, useFeeTransactions, type LiabilityDetail, type SettlementPreview } from '../api/liability';
import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import { cents, money, spreadMoney, sumMoney } from '../lib/money';
import { plannedBills } from '../lib/liability';
import Money from './Money';

export interface LiabilityDialog {
  mode: 'repay' | 'allocation' | 'reconcile' | 'create_plan' | 'edit_plan' | 'settle' | 'reverse';
  requestKey: string; billId?: number; planId?: number; txnId?: number; operationId?: number; version?: number;
  draft?: { amount?: string; entry_date?: string; category_id?: number; note?: string; platform?: string; source_txn_id?: number };
}
interface TermValue { id: number; remaining_principal?: string; statement_id?: number; fee_amount?: string; fee_date?: Dayjs; fee_txn_id?: number }
interface Values {
  amount?: string; entry_date?: Dayjs; from_account_id?: number; mode?: string;
  targets?: { statement_id: number; amount: string }[]; reason?: string;
  reference_amount?: string; complete?: boolean; moves?: { item_id: number; statement_id: number }[];
  source_txn_id?: number; category_id?: number; note?: string; platform?: string; term_count?: number; first_statement_date?: Dayjs;
  fee_mode?: string; fee_input?: string; fee_account_id?: number; fee_category_id?: number; fee_date?: Dayjs;
  terms?: TermValue[]; refund_txn_id?: number; principal?: string; fee_amount?: string; version?: number;
}
const titles: Record<LiabilityDialog['mode'], string> = { repay: '记录还款', allocation: '调整还款分配', reconcile: '核对账单', create_plan: '记录固定分期', edit_plan: '调整分期与费用', settle: '提前结清', reverse: '撤销并恢复记录' };
const amountRules = [{ pattern: /^\d{1,10}(\.\d{1,2})?$/, message: '请输入金额，最多两位小数' }];

export default function LiabilityDialogs({ detail: d, dialog, onClose, onAddExpense }: { detail: LiabilityDetail; dialog: LiabilityDialog | null; onClose: () => void; onAddExpense: () => void }) {
  const [form] = Form.useForm<Values>(); const values = Form.useWatch([], form) as Values | undefined;
  const accounts = useAccounts(); const cats = useCategoryForest(); const write = useLiabilityWrite(); const { message } = App.useApp();
  const user = useAuth((s) => s.user); const [preview, setPreview] = useState<SettlementPreview | null>(null); const [previewing, setPreviewing] = useState(false);
  const [feeDates, setFeeDates] = useState<Record<number, string>>({});
  const plan = d.plans.find((p) => p.id === dialog?.planId); const bill = d.statements.find((b) => b.id === dialog?.billId);
  const feeCandidates=useFeeTransactions(plan?.fee_account_id,dialog?.mode==='edit_plan');
  const editable = (id: number) => user?.role === 'admin' || user?.role === 'recorder' || user?.id === id;
  const assets = (accounts.data ?? []).filter((a) => a.type === 'asset' && a.status === 'active').map((a) => ({ value: a.id, label: a.name }));
  const feeAccounts = [{ value: d.account.id, label: `${d.account.name} · 计入欠款` }, ...assets];
  const categories = rootsOfKind(cats.data,'expense').flatMap((root) => root.children?.length ? root.children.map((leaf) => ({ value: leaf.id, label: `${root.name} / ${leaf.name}` })) : [{ value: root.id, label: root.name }]);
  const bills = d.statements.map((b) => ({ value: b.id, label: `${b.kind === 'settlement' ? '结清单' : '账单'} ${b.cycle_end} · ${b.due_date} 到期${b.issued_at ? '' : ' · 未出账'}` }));
  const payableBills = bills.filter((b) => d.statements.find((x) => x.id === b.value)?.issued_at);
  const sourceExpense = d.transactions.find((t) => t.id === values?.source_txn_id);
  const availableExpenses = d.transactions.filter((t) => t.kind === 'expense' && (!t.purpose || t.purpose === 'regular') && !t.installment_plan_id && !t.liability_term_id && !t.liability_operation_id && t.account_id === d.account.id && editable(t.created_by));
  const originalRemaining = sourceExpense ? sumMoney(d.items.filter((i) => i.source_id === sourceExpense.id && !i.plan_id).map((i) => i.remaining)) : values?.amount ?? '0';
  const count = values?.term_count ?? 3; const principalParts = spreadMoney(originalRemaining, count);
  const cycleRows = plannedBills(d.rules, values?.first_statement_date?.format('YYYY-MM-DD') || values?.entry_date?.format('YYYY-MM-DD') || dayjs().format('YYYY-MM-DD'), count);
  const feeInput = values?.fee_input ?? '0'; const feeMode = values?.fee_mode ?? 'per_term';
  const feeParts = feeMode === 'total_spread' ? spreadMoney(feeInput, count) : Array.from({ length: Math.min(120, Math.max(0, count)) }, (_, i) => feeMode === 'upfront' && i > 0 ? '0.00' : feeInput);
  const schedule = principalParts.map((amount, i) => ({ key: i, number: i + 1, amount, fee: feeParts[i] ?? '0', close: cycleRows[i]?.close ?? '', due: cycleRows[i]?.due ?? '', feeDate: feeDates[i] || (feeMode === 'upfront' && i === 0 ? values?.fee_date?.format('YYYY-MM-DD') : cycleRows[i]?.close) || '' }));

  useEffect(() => {
    if (!dialog) return; form.resetFields(); setPreview(null); setFeeDates({});
    const firstAsset = assets[0]?.value;
    const initial: Values = { entry_date: dayjs(), mode: 'auto', targets: [], moves: [], term_count: 3, fee_mode: 'per_term', fee_input: '0', fee_amount: '0', fee_account_id: d.account.id, from_account_id: firstAsset, first_statement_date: dayjs(), fee_date: dayjs(), reason: '', fee_category_id: categories[0]?.value, category_id: categories[0]?.value };
    if (dialog.mode === 'create_plan' && dialog.draft) {
      Object.assign(initial, dialog.draft, { entry_date: dayjs(dialog.draft.entry_date), first_statement_date: dayjs(dialog.draft.entry_date) });
    }
    if (dialog.mode === 'repay') initial.amount = cents(d.summary.overdue) > 0n ? d.summary.overdue : d.summary.current_due;
    if (dialog.mode === 'allocation') { const credit = d.credits.find((c) => c.source_id === dialog.txnId); if (credit) { initial.mode = credit.mode; try { initial.targets = JSON.parse(credit.targets); } catch { initial.targets = []; } } }
    if (dialog.mode === 'reconcile') { initial.reference_amount = bill?.reference_amount ?? undefined; initial.complete = true; }
    if (plan) {
      initial.version = dialog.version ?? plan.version; initial.fee_category_id = plan.fee_category_id;
      if (dialog.mode === 'settle') { initial.principal = sumMoney(plan.terms.map((t) => t.remaining)); initial.fee_account_id = firstAsset; }
      if (dialog.mode === 'edit_plan') initial.terms = plan.terms.map((t) => ({ id: t.id, remaining_principal: t.remaining, statement_id: t.statement_id, fee_amount: t.fee_amount, fee_date: dayjs(t.fee_date) }));
    }
    form.setFieldsValue(initial);
    // Reset once for an explicit user action; keep the request key and draft on retry.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dialog, form]);
  useEffect(()=>{if(!dialog)return;if(categories.length){if(!form.getFieldValue('category_id'))form.setFieldValue('category_id',categories[0].value);if(!form.getFieldValue('fee_category_id'))form.setFieldValue('fee_category_id',categories[0].value);}if(assets.length&&!form.getFieldValue('from_account_id'))form.setFieldValue('from_account_id',assets[0].value);},[dialog,cats.data,accounts.data,form]);

  const settlementInput = (v: Values) => ({ request_key: dialog?.requestKey, version: v.version ?? plan?.version, principal: v.principal, fee_amount: v.fee_amount || '0', fee_account_id: v.fee_account_id, fee_category_id: v.fee_category_id, from_account_id: v.from_account_id, entry_date: v.entry_date?.format('YYYY-MM-DD'), reason: v.reason });
  const loadPreview = async () => { if (!plan) return; setPreviewing(true); try { const v = await form.validateFields(); const result = await liabilityWrite<SettlementPreview>('post', `/liabilities/installments/${plan.id}/settlement-preview`, settlementInput(v)); setPreview(result); form.setFieldValue('version', result.version); } catch (e) { message.error(errMsg(e)); } finally { setPreviewing(false); } };
  const submit = async () => {
    if (!dialog || write.isPending) return;
    try {
      const v = await form.validateFields(); const base = { request_key: dialog.requestKey, reason: v.reason || '' }; let input: unknown; let url = ''; let method: 'post' | 'put' | 'patch' = 'post';
      if (dialog.mode === 'repay') { url = '/liabilities/repayments'; input = { ...base, account_id: d.account.id, from_account_id: v.from_account_id, amount: v.amount, entry_date: v.entry_date?.format('YYYY-MM-DD'), mode: v.mode, targets: v.mode === 'manual' ? v.targets ?? [] : [] }; }
      if (dialog.mode === 'allocation') { url = `/liabilities/repayments/${dialog.txnId}/allocations`; method = 'put'; input = { ...base, mode: v.mode, targets: v.mode === 'manual' ? v.targets ?? [] : [] }; }
      if (dialog.mode === 'reconcile') { url = `/statements/${dialog.billId}/reconcile`; input = { ...base, reference_amount: v.reference_amount || undefined, complete: !!v.complete, moves: v.moves ?? [] }; }
      if (dialog.mode === 'create_plan') {
        url = '/liabilities/installments'; input = { ...base, account_id: d.account.id, source_txn_id: v.source_txn_id, amount: v.amount, entry_date: v.entry_date?.format('YYYY-MM-DD'), category_id: v.category_id, note: v.note, platform:v.platform, term_count: v.term_count, first_statement_date: v.first_statement_date?.format('YYYY-MM-DD'), fee_mode: v.fee_mode, fee_input: v.fee_input || '0', fee_account_id: v.fee_account_id, fee_category_id: v.fee_category_id, fee_date: v.fee_date?.format('YYYY-MM-DD'), fee_dates: Object.keys(feeDates).length && v.fee_mode !== 'upfront' ? schedule.map((x) => x.feeDate) : undefined };
      }
      if (dialog.mode === 'edit_plan' && plan) {
        const refund = plan.status === 'needs_reconcile'; method = refund ? 'post' : 'patch'; url = `/liabilities/installments/${plan.id}${refund ? '/refund-adjustments' : ''}`;
        const terms = (v.terms ?? []).map((t) => { const old = plan.terms.find((x) => x.id === t.id)!; return { id: t.id, remaining_principal: t.remaining_principal !== undefined && cents(t.remaining_principal) !== cents(old.remaining) ? t.remaining_principal : undefined, statement_id: t.statement_id !== old.statement_id ? t.statement_id : undefined, fee_amount: t.fee_amount !== undefined && cents(t.fee_amount) !== cents(old.fee_amount) ? t.fee_amount : undefined, fee_date: t.fee_date?.format('YYYY-MM-DD') !== old.fee_date ? t.fee_date?.format('YYYY-MM-DD') : undefined, fee_txn_id: t.fee_txn_id }; });
        input = { ...base, version: v.version, refund_txn_id: v.refund_txn_id, terms };
      }
      if (dialog.mode === 'settle') { if (!preview) { message.info('请先核对结清预览'); return; } url = `/liabilities/installments/${dialog.planId}/settle`; input = settlementInput(v); }
      if (dialog.mode === 'reverse') { url = `/liabilities/operations/${dialog.operationId}/reverse`; input = base; }
      await write.mutateAsync({ method, url, input }); message.success('已保存，账单与资金记录已更新'); onClose();
    } catch (e) { if(e&&typeof e==='object'&&'errorFields' in e)return;message.error(errMsg(e)); }
  };
  if (!dialog) return null;
  const allocatingCredit = d.credits.find((c) => c.source_id === dialog.txnId);
  let previewLeft = cents(dialog.mode === 'allocation' ? allocatingCredit?.amount : values?.amount);
  const repaymentRows: { key: number; bill: string; amount: string }[] = [];
  let allocationError = '';
  const eligible = d.statements.filter((b) => !!b.issued_at).sort((a,b)=>a.due_date.localeCompare(b.due_date)||a.id-b.id);
  const available = new Map(eligible.map((b)=>[b.id,cents(b.remaining) + (allocatingCredit ? d.allocations.filter((a)=>a.credit_event_id === allocatingCredit.id && d.items.find((i)=>i.id===a.debt_item_id)?.statement_id === b.id).reduce((sum,a)=>sum+cents(a.amount),0n) : 0n)]));
  const targets = values?.mode === 'manual' ? values.targets ?? [] : values?.mode === 'hold' ? [] : eligible.map((b)=>({statement_id:b.id,amount:money(available.get(b.id) ?? 0n)}));
  for (const target of targets) {
    const b = eligible.find((x)=>x.id===target.statement_id); const left = available.get(target.statement_id) ?? 0n;
    let allocated = cents(target.amount);
    if (values?.mode === 'manual' && (allocated > previewLeft || allocated > left || allocated < 0n)) allocationError = '分配金额不能超过本次还款或账单未还额';
    if (values?.mode !== 'manual') allocated = allocated < previewLeft ? allocated : previewLeft;
    if (b && allocated > 0n) { repaymentRows.push({key:repaymentRows.length,bill:`${b.cycle_end} · ${b.due_date} 到期`,amount:money(allocated)}); previewLeft -= allocated; available.set(b.id,left-allocated); }
  }
  const reason = (required = false) => <Form.Item name="reason" label="核对 / 操作说明" rules={required ? [{ required: true, message: '请填写核对依据或操作原因' }] : undefined}><Input.TextArea maxLength={255} rows={2} /></Form.Item>;
  const allocationFields = <>
    <Form.Item name="mode" label="还款分配"><Select options={[{ value: 'auto', label: '先还最早到期账单' }, { value: 'manual', label: '手动指定账单' }, { value: 'hold', label: '保留为待抵扣资金' }]} /></Form.Item>
    {values?.mode === 'manual' && <Form.List name="targets">{(fields, { add, remove }) => <>{fields.map((f) => <Space key={f.key} align="baseline" className="liability-form-row"><Form.Item name={[f.name, 'statement_id']} rules={[{ required: true }]}><Select aria-label="指定账单" placeholder="指定账单" options={payableBills} style={{ minWidth: 190 }} /></Form.Item><Form.Item name={[f.name, 'amount']} rules={[{ required: true }, ...amountRules]}><Input aria-label="分配金额" placeholder="金额" inputMode="decimal" /></Form.Item><Button onClick={() => remove(f.name)}>移除</Button></Space>)}<Button onClick={() => add()}>添加账单分配</Button></>}</Form.List>}
    <p className="small muted">未分配金额保留待抵扣。还款本金不会计入收支和预算。</p>
    {allocationError ? <Alert type="warning" title={allocationError}/> : <><Table size="small" pagination={false} dataSource={repaymentRows} columns={[{title:'本次分配预览',dataIndex:'bill'},{title:'抵扣金额',render:(_,r)=><Money value={r.amount}/>}]}/><p>本次保留待抵扣 <Money value={money(previewLeft)}/>。提交时按最新账单重新核验。</p></>}
  </>;
  return <Modal open title={titles[dialog.mode]} onCancel={onClose} onOk={submit} confirmLoading={write.isPending} width={dialog.mode === 'create_plan' || dialog.mode === 'edit_plan' ? 820 : 620} styles={{ body: { maxHeight: '65dvh', overflowY: 'auto' } }} okText={dialog.mode === 'settle' ? '确认结清' : '保存'}>
    <Form form={form} layout="vertical" onValuesChange={() => { if (dialog.mode === 'settle') setPreview(null); }}>
      <Form.Item name="version" hidden><InputNumber /></Form.Item>
      {dialog.mode === 'repay' && <><Form.Item name="from_account_id" label="还款资金账户" rules={[{ required: true }]}><Select options={assets} /></Form.Item><Form.Item name="amount" label="还款金额" rules={[{ required: true }, ...amountRules]}><Input inputMode="decimal" /></Form.Item><Form.Item name="entry_date" label="实际还款日期" rules={[{ required: true }]}><DatePicker /></Form.Item>{allocationFields}{reason()}</>}
      {dialog.mode === 'allocation' && <>{allocationFields}{reason(true)}</>}
      {dialog.mode === 'reconcile' && <>
        <p>系统账单 <Money value={bill?.amount} /> · 原出账快照 <Money value={bill?.snapshot_amount} /></p>
        <Form.Item name="reference_amount" label="平台账单参考金额（仅用于核对）" rules={amountRules}><Input inputMode="decimal" /></Form.Item>
        {values?.reference_amount && <p>与系统差异 <Money value={money(cents(values.reference_amount) - cents(bill?.amount))} /></p>}
        <Space wrap><Button onClick={onAddExpense}>补记实际消费 / 费用</Button><span className="small muted">记录真实金额与日期，必要时在下面调整账单归属。</span></Space>
        {!!values?.moves?.length && <p className="small muted">调整预览：{values.moves.map((m)=>{const item=d.items.find((i)=>i.id===m.item_id);return item?`¥${item.amount}：${d.statements.find((b)=>b.id===item.statement_id)?.cycle_end} → ${d.statements.find((b)=>b.id===m.statement_id)?.cycle_end ?? '请选择账单'}`:'';}).filter(Boolean).join('；')}。消费日期、余额和收支账期保持不变。</p>}
        <Form.List name="moves">{(fields, { add, remove }) => <>{fields.map((f) => <Space key={f.key} align="baseline" className="liability-form-row"><Form.Item name={[f.name, 'item_id']} rules={[{ required: true }]}><Select placeholder="选择消费" options={d.items.filter((i) => !i.plan_id && editable(d.transactions.find((t) => t.id === i.source_id)?.created_by ?? -1)).map((i) => ({ value: i.id, label: `${d.transactions.find((t) => t.id === i.source_id)?.note || '消费'} · ¥${i.amount}` }))} style={{ minWidth: 180 }} /></Form.Item><Form.Item name={[f.name, 'statement_id']} rules={[{ required: true }]}><Select placeholder="实际归属账单" options={bills.filter((b) => d.statements.find((x) => x.id === b.value)?.kind === 'monthly')} style={{ minWidth: 190 }} /></Form.Item><Button onClick={() => remove(f.name)}>移除</Button></Space>)}<Button className="liability-toolbar" onClick={() => add()}>调整消费归属</Button></>}</Form.List>
        {reason((values?.moves?.length ?? 0) > 0)}<Form.Item name="complete" valuePropName="checked"><Checkbox>差异已解决，标记核对完成</Checkbox></Form.Item><p className="small muted">核对不直接覆盖应还金额，也不改变消费日期或收支账期。</p>
      </>}
      {dialog.mode === 'create_plan' && <>
        <Form.Item name="source_txn_id" label="消费来源"><Select allowClear placeholder="新消费（记录一次本金支出）" options={availableExpenses.map((t) => ({ value: t.id, label: `${dayjs(t.entry_date).format('MM-DD')} ${t.note || '消费'} · ¥${t.amount}` }))} onChange={(id: number | undefined) => { const t = availableExpenses.find((x) => x.id === id); if (t) form.setFieldsValue({ entry_date: dayjs(t.entry_date), first_statement_date: dayjs(t.entry_date), category_id: t.category_id ?? undefined }); }} /></Form.Item>
        {sourceExpense ? <Alert type="info" title={<>原消费 <Money value={sourceExpense.amount} />，本次分期未还本金 <Money value={originalRemaining} /></>} description="已还款或已退款部分保留在历史记录中；请填写本金核对说明。" /> : <><Form.Item name="amount" label="消费本金" rules={[{ required: true }, ...amountRules]}><Input inputMode="decimal" /></Form.Item><Space className="liability-form-row"><Form.Item name="entry_date" label="实际消费日期" rules={[{ required: true }]}><DatePicker /></Form.Item><Form.Item name="category_id" label="消费分类" rules={[{ required: true }]}><Select options={categories} style={{ minWidth: 190 }} /></Form.Item></Space><Form.Item name="note" label="消费备注"><Input maxLength={255} /></Form.Item><Form.Item name="platform" label="平台 / 商家"><Input maxLength={128}/></Form.Item></>}
        <Space className="liability-form-row"><Form.Item name="term_count" label="分期期数" rules={[{ required: true }]}><InputNumber min={1} max={120} /></Form.Item><Form.Item name="first_statement_date" label="首期归属日期" rules={[{ required: true }]}><DatePicker /></Form.Item></Space>
        <Form.Item name="fee_mode" label="手填手续费方式"><Select options={[{ value: 'per_term', label: '每期固定手续费' }, { value: 'total_spread', label: '总手续费按期分摊' }, { value: 'upfront', label: '一次性收取' }]} /></Form.Item>
        <Space className="liability-form-row"><Form.Item name="fee_input" label={feeMode === 'per_term' ? '每期手续费' : '总手续费'} rules={[{ required: true }, ...amountRules]}><Input inputMode="decimal" /></Form.Item><Form.Item name="fee_account_id" label="费用支付账户" rules={[{ required: true }]}><Select options={feeAccounts} style={{ minWidth: 190 }} /></Form.Item></Space>
        <Form.Item name="fee_category_id" label="手续费支出分类" rules={[{ required: true }]}><Select options={categories} /></Form.Item>
        {feeMode === 'upfront' && <Form.Item name="fee_date" label="一次性实际收取日期" rules={[{ required: true }]}><DatePicker /></Form.Item>}
        <p>本金合计 <Money value={originalRemaining} /> · 手续费预估合计 <Money value={sumMoney(feeParts)} /></p>
        <Table size="small" pagination={false} scroll={{ x: 580 }} dataSource={schedule} columns={[{ title: '期次', dataIndex: 'number' }, { title: '出账 / 到期', render: (_, r) => <>{r.close}<div className="small muted">{r.due}</div></> }, { title: '本金', render: (_, r) => <Money value={r.amount} /> }, { title: '手续费', render: (_, r) => <Money value={r.fee} /> }, { title: '收取日期', render: (_, r) => feeMode === 'upfront' ? r.feeDate : <DatePicker value={r.feeDate ? dayjs(r.feeDate) : null} allowClear={false} onChange={(date) => { if (date) setFeeDates((dates) => ({ ...dates, [r.key]: date.format('YYYY-MM-DD') })); }} /> }]} />
        {reason(!!sourceExpense && cents(sourceExpense.amount) !== cents(originalRemaining))}<p className="small muted">本金支出只记录一次。手续费按所填实际日期生成独立支出；未来费用先作预估。</p>
      </>}
      {dialog.mode === 'edit_plan' && plan && <>
        {plan.status === 'needs_reconcile' && <Form.Item name="refund_txn_id" label="已到账的本金退款记录" rules={[{ required: true }]}><Select options={d.transactions.filter((t) => t.kind === 'refund' && t.related_txn_id === plan.source_txn_id).map((t) => ({ value: t.id, label: `${dayjs(t.entry_date).format('YYYY-MM-DD')} · ¥${t.amount}` }))} /></Form.Item>}
        <Alert type="info" title="以平台实际剩余计划为依据" description="各期未还本金合计须保持真实金额；已发生费用请通过实际退款或合规修正处理。" />
        <Form.List name="terms">{(fields) => <>{fields.map((f) => { const old = plan.terms[f.name]; const issued = !!d.statements.find((b) => b.id === old.statement_id)?.issued_at; const canChangePrincipal = !issued || plan.status === 'needs_reconcile'; return <div key={f.key} className="liability-term-edit"><h4>第 {old.number} 期{old.blocked_reason && <span className="liability-problem"> · {old.blocked_reason}</span>}</h4><Form.Item name={[f.name, 'id']} hidden><InputNumber /></Form.Item><Space className="liability-form-row"><Form.Item name={[f.name, 'remaining_principal']} label="未还本金" rules={amountRules}><Input inputMode="decimal" disabled={!canChangePrincipal} /></Form.Item><Form.Item name={[f.name, 'statement_id']} label="归属账单"><Select options={bills} disabled={issued} style={{ minWidth: 210 }} /></Form.Item></Space><Space className="liability-form-row"><Form.Item name={[f.name, 'fee_amount']} label="手续费" rules={amountRules}><Input inputMode="decimal" disabled={!!old.fee_txn_id} /></Form.Item><Form.Item name={[f.name, 'fee_date']} label="实际收取日期"><DatePicker disabled={!!old.fee_txn_id} /></Form.Item></Space>{!old.fee_txn_id && <Form.Item name={[f.name, 'fee_txn_id']} label="如已手工记账，关联已有费用流水"><Select allowClear onChange={(id:number|undefined)=>{const t=(feeCandidates.data?.items??d.transactions).find((x)=>x.id===id);if(t){form.setFieldValue(['terms',f.name,'fee_amount'],t.amount);form.setFieldValue(['terms',f.name,'fee_date'],dayjs(t.entry_date));}}} options={(feeCandidates.data?.items ?? d.transactions).filter((t) => t.kind === 'expense' && !t.installment_plan_id && !t.liability_term_id && !t.liability_operation_id && t.account_id === plan.fee_account_id && editable(t.created_by)).map((t) => ({ value: t.id, label: `${dayjs(t.entry_date).format('YYYY-MM-DD')} · ¥${t.amount} · ${t.note || '支出'}` }))} /></Form.Item>}</div>; })}</>}</Form.List>
        <p>当前待还本金合计 <Money value={sumMoney(plan.terms.map((t) => t.remaining))} /> · 调整后 <Money value={sumMoney((values?.terms ?? []).map((t) => t.remaining_principal ?? '0'))} /></p>{reason(true)}
      </>}
      {dialog.mode === 'settle' && plan && <>
        <Form.Item name="principal" label="平台实际剩余本金" rules={[{ required: true }, ...amountRules]}><Input inputMode="decimal" /></Form.Item>
        <Form.Item name="from_account_id" label="本金还款账户" rules={[{ required: true }]}><Select options={assets} /></Form.Item>
        <Space className="liability-form-row"><Form.Item name="fee_amount" label="实际结清手续费（手填）" rules={[{ required: true }, ...amountRules]}><Input inputMode="decimal" /></Form.Item><Form.Item name="fee_account_id" label="结清费支付账户" rules={[{ required: true }]}><Select options={feeAccounts} style={{ minWidth: 190 }} /></Form.Item></Space>
        <Form.Item name="fee_category_id" label="手续费支出分类" rules={[{ required: true }]}><Select options={categories} /></Form.Item><Form.Item name="entry_date" label="实际结清日期" rules={[{ required: true }]}><DatePicker /></Form.Item>{reason()}
        <Button onClick={loadPreview} loading={previewing}>核对结清预览</Button>{preview && <div className="liability-preview"><p>已还本金 <Money value={preview.paid_principal} /> · 未还本金 <Money value={preview.principal} /> · 已发生未付费用 <Money value={preview.outstanding_fees} /></p><p>使用待抵扣资金 <Money value={preview.pending_credit} /> · 本次转账 <Money value={preview.transfer_amount} /></p><p>另记结清手续费 <Money value={preview.fee_amount} /> · 取消未来未发生费用 <Money value={preview.cancelled_fees} /></p></div>}
      </>}
      {dialog.mode === 'reverse' && <><Alert type="warning" title="撤销后恢复真实欠款与未来计划" description="相关资金流水必须仍在开放账期，且你有权修改全部关联记录。历史操作会保留。" />{reason(true)}</>}
    </Form>
  </Modal>;
}
