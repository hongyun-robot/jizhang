import { isCashAccount } from '../../domain/accounts';
import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Form, Input, Modal, Space } from 'antd';
import { DateInput } from '../../components/DatePicker';
import ResponsiveSelect from '../../components/ResponsiveSelect';
import AccountSelect from '../../components/AccountSelect';
import SelectionScope from '../../components/SelectionScope';
import { useAccounts } from '../../api/accounts';
import { useWealthProducts, useWealthQuotes, useWealthMutations, type WealthPosition, type WealthOrder, type WealthOrderKind, type WealthEvent, type WealthInput, type WealthOrderData } from '../../api/wealth';
import { requestJson } from '../../api/request';
import { useScope } from '../../api/keys';
import { errMsg } from '../../api/client';
import { wealthToday } from '../../domain/wealth';
import { suggestedWealthShares, suggestedWealthProceeds, wealthOrderBalances } from '../../domain/wealth-orders';
import { WEALTH_EVENT_LABEL } from './WealthForm';
import { submissionGate } from '../../lib/submission';
import { useVisualViewportKeyboard } from '../../hooks/useVisualViewportKeyboard';

interface Props {accountId: number; position?: WealthPosition; kind: WealthOrderKind; order?: WealthOrder; source?: WealthEvent; onClose: () => void}
const quantity = [{ required: true, message: '请填写银行确认值' }, { pattern: /^\d+(\.\d{1,8})?$/, message: '最多八位小数' }];
const money = [{ required: true, message: '请填写实际金额' }, { pattern: /^\d+(\.\d{1,2})?$/, message: '金额最多两位小数' }];

export default function WealthOrderForm({ accountId, position, kind, order, source, onClose }: Props) {
  const [form] = Form.useForm<WealthInput>();
  const values = Form.useWatch([], form);
  const products = useWealthProducts();
  const cash = useAccounts();
  const { save, refreshProduct } = useWealthMutations();
  const scope = useScope(); const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true); useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const key = useRef(crypto.randomUUID()); const gate = useRef(submissionGate());
  const navEdited = useRef(false), sharesEdited = useRef(false), amountEdited = useRef(false);
  const navDateEdited = useRef(false);
  const [creating, setCreating] = useState(false);
  const [preview, setPreview] = useState<WealthOrderData>();
  const [previewing, setPreviewing] = useState(false);
  const { message } = App.useApp(); useVisualViewportKeyboard(true);
  const subscribing = kind === 'subscribe', confirming = kind === 'confirm_buy', redeeming = kind === 'request_redeem', confirmingOut = kind === 'confirm_redeem', refunding = kind === 'refund_buy';
  const hasNAV = confirming || redeeming || confirmingOut;
  const selected = position?.product || products.data?.find((p) => p.code === values?.code);
  const quotes = useWealthQuotes(selected?.id || 0);
  const navDate = confirming ? values?.trade_date : values?.nav_date;
  const quote = quotes.data?.find((q) => q.nav_date.slice(0, 10) === navDate);
  const shareSuggestion = suggestedWealthShares(order?.amount, values?.trade_nav, values?.fee || '0');
  const balances = wealthOrderBalances(position?.orders || [], position?.shares || '0', values?.effective_date, values?.previous_trade_date, values?.trade_nav);
  useEffect(() => {
    if (hasNAV && !navEdited.current) form.setFieldValue('trade_nav', quote?.unit_nav || '');
  }, [hasNAV, quote?.unit_nav, navDate, form]);
  useEffect(() => {
    if (confirming && !sharesEdited.current) form.setFieldValue('shares', shareSuggestion || '');
    if (confirmingOut && !amountEdited.current) form.setFieldValue('amount', suggestedWealthProceeds(values?.shares, values?.trade_nav, order?.data.failed_principal) || '');
  }, [confirming, confirmingOut, shareSuggestion, values?.shares, values?.trade_nav, order?.data.failed_principal, form]);
  const refresh = async () => {
    if (!selected) return;
    try { const p = await refreshProduct.mutateAsync(selected.id); if (alive.current && currentScope.current === scope) message.info(p.sync_status === 'failed' ? p.last_error || '查询失败，保留草稿' : '已检查历史净值；缺少指定日期时可按银行确认值填写'); }
    catch (error) { if (alive.current && currentScope.current === scope) message.error(errMsg(error)); }
  };
  const validateReason = async () => {
    const v = form.getFieldsValue();
    const changedNAV = (confirming || redeeming) && !!v.trade_nav && (!quote || v.trade_nav && Number(v.trade_nav) !== Number(quote.unit_nav));
    const changedShares = confirming && shareSuggestion && v.shares && Number(v.shares) !== Number(shareSuggestion);
    if ((changedNAV || changedShares || confirming && v.trade_date !== order?.data.trade_date || confirmingOut && Number(v.shares) !== Number(order?.suggested_shares)) && !v.adjustment_reason?.trim()) throw new Error('修改净值、交易日或确认份额时，请填写银行确认说明');
  };
  const submit = () => gate.current(async () => {
    try {
      const v = await form.validateFields(); await validateReason();
      const input: WealthInput = { request_key: key.current, kind, code: selected?.code, effective_date: v.effective_date, note: v.note || '', adjustment_reason: v.adjustment_reason || '' };
      if (subscribing || refunding || redeeming || confirmingOut) input.amount = v.amount;
      if (subscribing) { input.trade_date = v.trade_date; input.expected_date = v.expected_date || undefined; input.application_time = v.application_time; }
      if (subscribing || refunding) input.cash_account_id = v.cash_account_id;
      if (confirming || confirmingOut) { input.shares = v.shares; input.fee = v.fee || '0'; }
      if (hasNAV) input.trade_nav = v.trade_nav || '0';
      if (confirming) { input.trade_date = v.trade_date; input.nav_date = v.trade_date; }
      if (redeeming) { input.trade_date = v.effective_date; input.previous_trade_date = v.previous_trade_date; input.nav_date = v.nav_date || v.previous_trade_date; input.insufficient_policy=v.insufficient_policy || 'cancel'; }
      if (confirmingOut) input.nav_date = v.nav_date;
      if (!subscribing && !redeeming) input.source_event_id = source?.id || order?.event_id;
      const path = subscribing ? `/wealth/accounts/${accountId}/orders` : `/wealth/positions/${position?.id}/events`;
      await save.mutateAsync({ path, input });
      if (alive.current && currentScope.current === scope) { message.success('订单记录已保存'); onClose(); }
    } catch (error) { if (error && typeof error === 'object' && 'errorFields' in error) return; if (alive.current && currentScope.current === scope) message.error(errMsg(error)); }
  });
  const previewOrder = async () => {
    try {
      const v = await form.validateFields(); setPreviewing(true);
      const e = await requestJson<WealthEvent>(`/wealth/positions/${position?.id}/order-preview`, { method: 'post', data: { ...v, kind, trade_date: v.effective_date, request_key: 'preview' } });
      if (alive.current && currentScope.current === scope) setPreview(JSON.parse(e.order_json || '{}') as WealthOrderData);
    } catch (error) { if (alive.current && currentScope.current === scope) message.error(errMsg(error)); }
    finally { if (alive.current) setPreviewing(false); }
  };
  const today = wealthToday();
  return <SelectionScope active><Modal open title={WEALTH_EVENT_LABEL[kind]} onCancel={onClose} onOk={submit} okText="保存订单记录" okButtonProps={{ 'aria-label': '保存订单记录', loading: save.isPending, disabled: creating || save.isPending }} destroyOnHidden styles={{ body: { maxHeight: 'min(70dvh, calc(100dvh - var(--kb-inset, 0px) - 160px))', overflowY: 'auto' } }}>
    <Form form={form} layout="vertical" initialValues={{ code: position?.code, effective_date: today, trade_date: order?.data.trade_date || today, application_time: new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).format(new Date()), previous_trade_date: '', nav_date: confirmingOut ? today : '', shares: confirmingOut ? order?.suggested_shares : '', amount: refunding ? order?.refund_due : '', cash_account_id: refunding ? source?.cash_account_id : undefined, fee: '0' }} onValuesChange={(changed) => {
      if ('trade_nav' in changed) navEdited.current = true;
      if ('shares' in changed) sharesEdited.current = true;
      if ('amount' in changed) amountEdited.current = true;
      if ('trade_date' in changed || 'nav_date' in changed) { navEdited.current = false; form.setFieldValue('trade_nav', ''); }
      if ('nav_date' in changed) navDateEdited.current = true;
      if ('previous_trade_date' in changed && !navDateEdited.current) {navEdited.current=false;form.setFieldValue('nav_date', changed.previous_trade_date);}
      setPreview(undefined);
    }}>
      <Alert type="info" title={subscribing ? '仅登记已实际付款的申请，确认时不再扣款' : refunding ? '只登记银行已实际退回的金额' : confirming ? '净值日采用申请交易日，不采用确认日' : redeeming ? '可转出金额只包含乙、甲；当日丙类仅可撤销' : confirmingOut ? '以管理人确认的份额和实际毛额为准' : '取消本身不代表资金已退回'} description="本页登记银行事实，不向银行下单。预计日期仅作提示；未确认本金和未到账退款不计算净值收益。" />
      {subscribing && !position && <Form.Item name="code" label="理财产品" rules={[{ required: true }]}><ResponsiveSelect sheetTitle="已接入理财产品" aria-label="选择待确认申购产品" options={(products.data || []).filter((p) => p.admission_status === 'enabled').map((p) => ({ value: p.code, label: `${p.name} · ${p.code}` }))} loading={products.isLoading} /></Form.Item>}
      {selected && <p>{selected.name} · {selected.code}</p>}
      {redeeming && <div className="wealth-form-grid"><p>丙类当日待确认：{balances.current} 元，仅可撤销</p><p>乙类上一交易日可分配：{balances.previous} 元</p><p>甲类可用确认份额：{balances.confirmedShares} 份；参考金额 {balances.confirmedAmount ? `${balances.confirmedAmount} 元` : '等待指定日期净值'}</p></div>}
      {subscribing || refunding || redeeming || confirmingOut ? <Form.Item name="amount" label={subscribing ? '实际已付款金额' : refunding ? '本次实际退款金额' : redeeming ? '可转出申请金额（不含当日待确认）' : '银行确认转出毛额（未扣费用）'} rules={money}><Input inputMode="decimal" /></Form.Item> : null}
      <Form.Item name="effective_date" label={subscribing ? '实际付款日期' : refunding ? '实际退款到账日期' : redeeming ? '转出申请交易日（银行记录）' : confirming ? '实际份额确认日期' : confirmingOut ? '实际转出确认日期' : '实际取消或失败日期'} rules={[{ required: true }]}><DateInput /></Form.Item>
      {(subscribing || confirming) && <Form.Item name="trade_date" label="申请所属交易日（银行记录）" rules={[{ required: true }]}><DateInput /></Form.Item>}
      {subscribing && <><Form.Item name="application_time" label="银行申请时间（后进先出依据）" rules={[{ required: true }, { pattern: /^([01]\d|2[0-3]):[0-5]\d:[0-5]\d$/, message: '请输入 HH:mm:ss' }]}><Input placeholder="09:00:56" /></Form.Item><Form.Item name="expected_date" label="预计确认日期（仅提醒，可空）"><DateInput /></Form.Item></>}
      {redeeming && <Form.Item name="previous_trade_date" label="上一交易日（银行记录）" rules={[{ required: true }]}><DateInput /></Form.Item>}
      {redeeming && <Form.Item name="insufficient_policy" label="可转出余额不足时" initialValue="cancel"><ResponsiveSelect sheetTitle="余额不足处理" options={[{value:'cancel',label:'取消转出申请'},{value:'all',label:'按全部可转出余额登记'}]} /></Form.Item>}
      {(redeeming || confirmingOut) && <Form.Item name="nav_date" label={confirmingOut ? '转出开放日净值日期' : '银行可获取的净值日期'} rules={confirmingOut ? [] : [{ required: true }]}><DateInput /></Form.Item>}
      {hasNAV && <><p className="small muted">{quote ? `指定日期净值 ${quote.unit_nav} · ${quote.source === 'automatic' ? '自动同步' : '本人补录'}` : '指定日期暂无净值，不使用其他日期替代。可刷新历史或填写银行确认值。'}</p><Button aria-label="刷新指定日期净值" onClick={() => void refresh()} loading={refreshProduct.isPending}>刷新指定日期净值</Button><Form.Item name="trade_nav" label="成交单位净值（可修改）" rules={confirming ? quantity : [{ pattern: /^\d+(\.\d{1,8})?$/, message: '最多八位小数' }]}><Input inputMode="decimal" /></Form.Item></>}
      {(confirming || confirmingOut) && <><Form.Item name="shares" label="银行实际确认份额（可修改）" rules={quantity}><Input inputMode="decimal" /></Form.Item><Form.Item name="fee" label="实际手续费（已含扣款或净额）" rules={money}><Input inputMode="decimal" /></Form.Item>{confirming && <Button onClick={() => { sharesEdited.current = false; form.setFieldValue('shares', shareSuggestion || ''); }}>按申请金额和净值重新计算份额</Button>}</>}
      {(subscribing || refunding) && <Form.Item name="cash_account_id" label={subscribing ? '实际付款账户' : '实际退款到账账户'} rules={[{ required: true }]}><AccountSelect assetOnly active sheetTitle="本人现金账户" aria-label="选择订单现金账户" options={(cash.data || []).filter((a) => isCashAccount(a) && a.status === 'active').map((a) => ({ value: a.id, label: a.name }))} loading={cash.isLoading} onCreatingChange={setCreating} /></Form.Item>}
      <Form.Item name="adjustment_reason" label="银行确认或调整说明"><Input maxLength={255} placeholder="手动净值、份额尾差、取消或确认失败原因" /></Form.Item>
      <Form.Item name="note" label="备注"><Input maxLength={255} /></Form.Item>
      {redeeming && <><Space><Button onClick={() => void previewOrder()} loading={previewing}>核对后进先出分配</Button></Space>{preview?.cancelled_for_insufficient && <Alert type="warning" title="余额不足，本申请将记录为取消" />}{preview?.allocations?.map((a, i) => <p className="small" key={i}>{a.source_id ? `乙类申购 #${a.source_id}` : '甲类已确认份额'}：{a.amount} 元 · {a.source_id ? '待原申购确认后计算份额' : `${a.shares} 份`}</p>)}</>}
    </Form>
  </Modal></SelectionScope>;
}
