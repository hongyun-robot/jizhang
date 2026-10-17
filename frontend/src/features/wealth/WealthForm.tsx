import { isCashAccount } from '../../domain/accounts';
import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Form, Input, Modal, Switch } from 'antd';
import { wealthToday } from '../../domain/wealth';
import { wealthOpeningUnitCost } from '../../domain/wealth-orders';
import { DateInput } from '../../components/DatePicker';
import type { WealthEvent, WealthEventKind, WealthInput, WealthPosition } from '../../api/wealth';
import { useWealthProducts, useWealthMutations, useWealthQuotes } from '../../api/wealth';
import { useAccounts } from '../../api/accounts';
import { useScope } from '../../api/keys';
import { errMsg } from '../../api/client';
import AccountSelect from '../../components/AccountSelect';
import ResponsiveSelect from '../../components/ResponsiveSelect';
import SelectionScope from '../../components/SelectionScope';
import { submissionGate } from '../../lib/submission';
import { useVisualViewportKeyboard } from '../../hooks/useVisualViewportKeyboard';

export const WEALTH_EVENT_LABEL: Record<WealthEventKind, string> = { opening: '期初持仓', buy: '申购', redeem: '赎回', cash_dividend: '现金分红', reinvest: '红利再投资', settlement: '登记到账', subscribe: '登记待确认申购', confirm_buy: '确认申购份额', cancel_buy: '取消或确认失败', refund_buy: '登记申购退款', request_redeem: '登记转出申请', confirm_redeem: '确认转出结果', cancel_redeem: '取消转出申请' };
export const dateText = (s?: string) => s?.slice(0, 10) || '—';
interface Values { code: string; display_name: string; shares: string; total_cost: string; opening_nav: string; amount: string; fee: string; trade_nav: string; effective_date: string; rights_date: string; registration_date: string; cash_date: string; cash_account_id?: number; settled: boolean; note: string }
const quantityRules = [{ required: true, message: '请填写实际确认值' }, { pattern: /^[0-9]+(?:\.[0-9]{1,8})?$/, message: '最多八位小数' }];
const moneyRules = [{ required: true, message: '请填写实际金额' }, { pattern: /^[0-9]+(?:\.[0-9]{1,2})?$/, message: '金额最多两位小数' }];

export default function WealthForm({ accountId, position, kind, amend = false, settlement, onClose }: { accountId: number; position?: WealthPosition; kind: WealthEventKind; amend?: boolean; settlement?: WealthEvent; onClose: () => void }) {
  const { message } = App.useApp();
  const [form] = Form.useForm<Values>();
  const values = Form.useWatch([], form) as Values | undefined;
  const accounts = useAccounts();
  const products = useWealthProducts();
  const { save, refreshProduct } = useWealthMutations();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  const key = useRef(crypto.randomUUID());
  const gate = useRef(submissionGate());
  const openingNAVEdited = useRef(false);
  const openingNAVContext = useRef('');
  const [creating, setCreating] = useState(false);
  useVisualViewportKeyboard(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const isOpening = kind === 'opening';
  const isDividend = kind === 'cash_dividend' || kind === 'reinvest';
  const hasShares = isOpening || kind === 'buy' || kind === 'redeem' || kind === 'reinvest';
  const hasCash = kind === 'buy' || kind === 'settlement' || ((kind === 'redeem' || kind === 'cash_dividend') && values?.settled === true);
  const initial: Partial<Values> = { code: position?.code || '', display_name: amend ? position?.display_name : '', shares: amend ? position?.shares : '', total_cost: amend ? position?.cost : '', amount: '', fee: '0', trade_nav: '', effective_date: amend ? position?.effective_date : wealthToday(), rights_date: wealthToday(), registration_date: '', cash_date: wealthToday(), settled: false, note: '' };
  const submit = () => gate.current(async () => {
    try {
      const v = await form.validateFields();
      const input: WealthInput = { request_key: key.current, kind, code: position?.code || v.code, effective_date: v.effective_date, note: v.note || '' };
      if (!position || isOpening) input.display_name = v.display_name || '';
      input.shares = hasShares ? v.shares : '0';
      if (isOpening) { input.total_cost = v.total_cost; if(v.opening_nav?.trim()) input.opening_nav=v.opening_nav.trim(); }
      else if (kind !== 'settlement') input.amount = v.amount;
      if (kind === 'buy') input.trade_nav = v.trade_nav;
      if (kind === 'buy' || kind === 'redeem') input.fee = v.fee || '0';
      if (isDividend) { input.rights_date = v.rights_date; input.registration_date = v.registration_date || undefined; if (kind === 'cash_dividend') input.effective_date = v.rights_date; }
      if (hasCash) { input.cash_date = kind === 'settlement' ? undefined : v.cash_date; input.cash_account_id = v.cash_account_id; }
      if (kind === 'settlement') input.source_event_id = settlement?.id;
      const path = amend ? `/wealth/positions/${position?.id}` : isOpening ? `/wealth/accounts/${accountId}/positions` : position ? `/wealth/positions/${position.id}/events` : `/wealth/accounts/${accountId}/events`;
      await save.mutateAsync({ path, input, method: amend ? 'put' : 'post' });
      if (!alive.current || currentScope.current !== scope) return;
      message.success('理财记录已保存'); onClose();
    } catch (e) { if (e && typeof e === 'object' && 'errorFields' in e) return; if (alive.current && currentScope.current === scope) message.error(errMsg(e)); }
  });
  const admitted = (products.data ?? []).filter((p) => p.admission_status === 'enabled');
  const selectedProduct = position?.product || admitted.find((p) => p.code === values?.code);
  const quotes = useWealthQuotes(selectedProduct?.id || 0);
  const latest = quotes.data?.at(-1);
  const selectedDate = values?.effective_date || initial.effective_date;
  const sameDay = quotes.data?.find((q) => dateText(q.nav_date) === selectedDate);
  useEffect(() => {
    if (!isOpening) return;
    const context = `${selectedProduct?.id || 0}:${selectedDate || ''}`;
    if (openingNAVContext.current !== context) {openingNAVContext.current=context;openingNAVEdited.current=false;}
    if (!openingNAVEdited.current) {
      const stored = amend && position?.opening_nav_date === selectedDate ? position?.opening_nav : undefined;
      form.setFieldValue('opening_nav', stored || sameDay?.unit_nav || '');
    }
  }, [isOpening, selectedProduct?.id, selectedDate, sameDay?.unit_nav, amend, position?.opening_nav_date, position?.opening_nav, form]);
  const refreshQuote = async () => {
    if (!selectedProduct) return;
    try { const result=await refreshProduct.mutateAsync(selectedProduct.id); if (!alive.current || currentScope.current !== scope) return; if (result.sync_status==='failed') message.warning(result.last_error || '查询失败，保留旧净值'); else message.info(result.sync_status==='refreshing'?'净值查询进行中':'已检查净值状态，冷却期间使用缓存'); }
    catch(e){if(alive.current && currentScope.current===scope) message.error(errMsg(e));}
  };
  return <SelectionScope active><Modal open title={amend ? '更正期初持仓' : WEALTH_EVENT_LABEL[kind]} onCancel={onClose} onOk={submit} okText="保存记录" okButtonProps={{ 'aria-label': '保存记录', loading: save.isPending, disabled: save.isPending || creating || (!position && !admitted.length) }} destroyOnHidden styles={{ body: { maxHeight: 'min(70dvh, calc(100dvh - var(--kb-inset, 0px) - 160px))', overflowY: 'auto' } }}>
    <Form form={form} initialValues={initial} layout="vertical" name="wealth-operation" onValuesChange={(changed) => {if ('opening_nav' in changed) openingNAVEdited.current=true;}}>
      {position ? <p className="muted">{position.name} · {position.code} · {position.product.currency}</p> : <>
        <Form.Item name="code" label="理财产品" rules={[{ required: true, message: '请选择已接入产品' }]}><ResponsiveSelect sheetTitle="已接入理财产品" aria-label="选择已接入理财产品" placeholder="请选择已接入产品" options={admitted.map((p) => ({ value: p.code, label: `${p.name} · ${p.code} · ${p.currency}` }))} loading={products.isLoading} errorMessage={products.isError ? errMsg(products.error) : undefined} onRetry={() => void products.refetch()} /></Form.Item>
        {!products.isLoading && !admitted.length && <Alert type="info" title="暂无开放新增的产品" description="已接入且开放新增的产品会显示在这里。可以先建立空理财账户。" />}
        <Form.Item name="display_name" label="显示名称（可选）"><Input maxLength={128} /></Form.Item>
      </>}
      {isOpening && <Alert type="info" title="录入已有持仓，不再次扣款" description="填写实际总成本和确认份额，单位成本自动计算。参考净值只用于估值；缺少净值仍可保存。" />}
      {kind === 'buy' && <Alert type="info" title="只登记银行已确认的申购" description="付款日不得晚于份额确认日。费用包含在总扣款中，不重复扣款；请按实际成交净值核对。" />}
      {isDividend && <Alert type="info" title="请按本人实际权益核对" description="公共净值不能自动识别个人分红。分红或再投资只确认一次收益。" />}
      {selectedProduct && <div className="wealth-toolbar"><p className="small muted" data-testid="wealth-reference-nav">{isOpening ? `所选日期 ${selectedDate || '—'} 的净值：${sameDay?.unit_nav || '暂无同日数据'} · ${sameDay?.source === 'automatic' ? '自动同步' : sameDay ? '本人补录' : '可手工填写或留空'}。净值不参与总成本计算。` : `参考单位净值 ${latest?.unit_nav || '暂无'} · ${dateText(latest?.nav_date)}。实际成交值请另行填写。`}</p><Button aria-label="刷新参考净值" loading={refreshProduct.isPending} onClick={() => void refreshQuote()}>刷新参考净值</Button></div>}
      <div className="wealth-form-grid">
        {hasShares && <Form.Item name="shares" label={isOpening ? '已确认持有份额' : kind === 'redeem' ? '实际赎回份额' : '实际确认份额'} rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
        {isOpening && <><Form.Item name="total_cost" label="实际总成本（元）" rules={moneyRules}><Input inputMode="decimal" placeholder="例如 1.16；零成本请填 0" /></Form.Item><Form.Item label="单位成本（自动计算）" extra="总成本 ÷ 确认份额，显示八位小数；保存以实际总成本为准。"><Input aria-label="单位成本（自动计算）" value={wealthOpeningUnitCost(values?.total_cost, values?.shares) || ''} readOnly placeholder="填写总成本和份额后自动计算" /></Form.Item></>}
        {isOpening && <Form.Item name="opening_nav" label="单位净值（可填写）" extra="默认带入持仓生效日的同日净值，允许修改或留空；保存为建账参考记录，不改写行情或成本。" rules={[{pattern:/^[0-9]+(?:\.[0-9]{1,8})?$/,message:'净值最多八位小数'}]}><Input inputMode="decimal" /></Form.Item>}
        {!isOpening && kind !== 'settlement' && <Form.Item name="amount" label={kind === 'buy' ? '实际总扣款（含费用）' : kind === 'redeem' ? '赎回成交毛额（未扣费用）' : '实际分红金额'} rules={moneyRules}><Input inputMode="decimal" /></Form.Item>}
        {kind === 'buy' && <Form.Item name="trade_nav" label="实际成交净值" rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
        {(kind === 'buy' || kind === 'redeem') && <Form.Item name="fee" label="实际费用（已含在扣款或净额）" rules={moneyRules}><Input inputMode="decimal" /></Form.Item>}
        {kind !== 'cash_dividend' && <Form.Item name="effective_date" label={isOpening ? '持仓生效日期' : kind === 'settlement' ? '实际到账日期' : '份额确认日期'} rules={[{ required: true }]}><DateInput /></Form.Item>}
        {isDividend && <><Form.Item name="rights_date" label="权益生效日期" rules={[{ required: true }]}><DateInput /></Form.Item><Form.Item name="registration_date" label="权益登记日期（可选）"><DateInput /></Form.Item></>}
      </div>
      {(kind === 'redeem' || kind === 'cash_dividend') && <Form.Item name="settled" label="款项已实际到账" valuePropName="checked"><Switch /></Form.Item>}
      {hasCash && <>{kind !== 'settlement' && <Form.Item name="cash_date" label={kind === 'buy' ? '实际付款日期' : '实际到账日期'} rules={[{ required: true }]}><DateInput /></Form.Item>}
        <Form.Item name="cash_account_id" label={kind === 'buy' ? '付款账户' : '到账账户'} rules={[{ required: true, message: '请选择本人现金账户' }]}><AccountSelect assetOnly sheetTitle="本人现金资产账户" aria-label="选择本人现金资产账户" placeholder="请选择本人现金账户" active options={(accounts.data ?? []).filter((a) => isCashAccount(a) && a.status === 'active').map((a) => ({ value: a.id, label: a.name }))} onCreatingChange={setCreating} loading={accounts.isLoading} errorMessage={accounts.isError ? errMsg(accounts.error) : undefined} onRetry={() => void accounts.refetch()} /></Form.Item></>}
      {(kind === 'redeem' || kind === 'cash_dividend') && !hasCash && <p className="small muted">尚未到账的净额保留为应收资产。实际到账后，在事件记录中登记；不要填写未来预计日期。</p>}
      <Form.Item name="note" label="备注"><Input maxLength={255} /></Form.Item>
    </Form>
  </Modal></SelectionScope>;
}
