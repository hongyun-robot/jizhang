import { isCashAccount } from '../../domain/accounts';
import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Checkbox, Form, Input, Modal, Space, Switch } from 'antd';
import dayjs from 'dayjs';
import { DateInput } from '../../components/DatePicker';
import type { FundDividend, FundEvent, FundEventKind, FundInput, FundInstrument, FundPosition } from '../../api/funds';
import { searchFund, useFundMutations } from '../../api/funds';
import { useAccounts } from '../../api/accounts';
import { useScope } from '../../api/keys';
import { errMsg } from '../../api/client';
import AccountSelect from '../../components/AccountSelect';
import SelectionScope from '../../components/SelectionScope';
import { submissionGate } from '../../lib/submission';
import { useVisualViewportKeyboard } from '../../hooks/useVisualViewportKeyboard';

export const FUND_EVENT_LABEL: Record<FundEventKind, string> = { opening: '期初持仓', buy: '买入', redeem: '赎回', cash_dividend: '现金分红', reinvest: '红利再投资', settlement: '登记到账' };
export const dateText = (s?: string) => s?.slice(0, 10) || '—';
interface Values { code: string; display_name: string; basis_confirmed: boolean; shares: string; cost_price: string; amount: string; fee: string; trade_nav: string; effective_date: string; rights_date: string; registration_date: string; cash_date: string; cash_account_id?: number; settled: boolean; note: string }
const quantityRules = [{ required: true, message: '请填写实际确认值' }, { pattern: /^[0-9]+(?:\.[0-9]{1,8})?$/, message: '请填写非负数字，最多八位小数' }];
export default function FundForm({ accountId, position, kind, amend = false, settlement, dividend, onClose }: { accountId: number; position?: FundPosition; kind: FundEventKind; amend?: boolean; settlement?: FundEvent; dividend?: FundDividend; onClose: () => void }) {
  const { message } = App.useApp();
  const [form] = Form.useForm<Values>();
  const values = Form.useWatch([], form) as Values | undefined;
  const accounts = useAccounts();
  const { save } = useFundMutations();
  const scope = useScope();
  const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true);
  const key = useRef(crypto.randomUUID());
  const gate = useRef(submissionGate());
  const lookupRequest = useRef(0);
  const [matches, setMatches] = useState<FundInstrument[]>([]);
  const [looking, setLooking] = useState(false);
  const [creating, setCreating] = useState(false);
  useVisualViewportKeyboard(true);
  const isOpening = kind === 'opening';
  const isDividend = kind === 'cash_dividend' || kind === 'reinvest';
  const hasShares = isOpening || kind === 'buy' || kind === 'redeem' || kind === 'reinvest';
  const hasTradePrice = kind === 'buy' || kind === 'redeem' || kind === 'reinvest';
  const hasCash = kind === 'buy' || kind === 'settlement' || ((kind === 'redeem' || kind === 'cash_dividend') && values?.settled !== false);
  useEffect(() => { alive.current = true; return () => { alive.current = false; lookupRequest.current++; }; }, []);
  const initial: Partial<Values> = {
    code: position?.code || '', display_name: amend ? position?.display_name : '',
    basis_confirmed: !!position, shares: amend ? position?.shares : '', cost_price: amend ? position?.cost_price : '',
    amount: '', fee: '0', trade_nav: '',
    effective_date: amend ? position?.effective_date : dividend ? dateText(dividend.ex_date) : dayjs().format('YYYY-MM-DD'),
    rights_date: dividend ? dateText(dividend.ex_date) : dayjs().format('YYYY-MM-DD'),
    registration_date: dividend ? dateText(dividend.registration_date) : '',
    cash_date: dayjs().format('YYYY-MM-DD'), settled: true, note: '',
  };
  const lookup = async () => {
    const q = form.getFieldValue('code')?.trim();
    if (!q) return;
    const request = ++lookupRequest.current;
    setLooking(true);
    try {
      const found = await searchFund(q);
      if (!alive.current || currentScope.current !== scope || request !== lookupRequest.current || form.getFieldValue('code')?.trim() !== q) return;
      setMatches(found);
      if (!found.length) message.info('未找到适用的场外基金，请核对代码');
      const exact = found.find((item) => item.ticker === q || item.thscode === q);
      if (exact) form.setFieldValue('code', exact.thscode);
    } catch (e) { if (alive.current && currentScope.current === scope && request === lookupRequest.current) message.error(errMsg(e)); }
    finally { if (alive.current && request === lookupRequest.current) setLooking(false); }
  };
  const submit = () => gate.current(async () => {
    try {
      const v = await form.validateFields();
      const input: FundInput = { request_key: key.current, kind, code: position?.code || v.code.trim(), effective_date: v.effective_date, note: v.note || '' };
      if (!position || isOpening) { input.display_name = v.display_name || ''; input.valuation_basis = 'cny_otc_unit_nav'; input.basis_confirmed = !!v.basis_confirmed; }
      input.shares = hasShares ? v.shares : '0';
      if (isOpening) input.cost_price = v.cost_price;
      else if (kind !== 'settlement') input.amount = v.amount;
      if (hasTradePrice) { input.trade_nav = v.trade_nav; input.fee = v.fee || '0'; }
      if (isDividend) { input.rights_date = v.rights_date; input.registration_date = v.registration_date || undefined; if (kind === 'cash_dividend') input.effective_date = v.rights_date; }
      if (hasCash) { input.cash_date = kind === 'settlement' ? undefined : v.cash_date; input.cash_account_id = v.cash_account_id; }
      if (kind === 'settlement') { input.source_event_id = settlement?.id; input.shares = '0'; }
      const path = amend ? `/funds/positions/${position?.id}` : isOpening ? `/funds/accounts/${accountId}/positions` : position ? `/funds/positions/${position.id}/events` : `/funds/accounts/${accountId}/events`;
      await save.mutateAsync({ path, input, method: amend ? 'put' : 'post' });
      if (!alive.current || currentScope.current !== scope) return;
      message.success('基金记录已保存'); onClose();
    } catch (e) { if (e && typeof e === 'object' && 'errorFields' in e) return; if (alive.current && currentScope.current === scope) message.error(errMsg(e)); }
  });
  return <SelectionScope active>
    <Modal open title={amend ? '更正期初持仓' : FUND_EVENT_LABEL[kind]} onCancel={onClose} onOk={submit} okText="保存记录" confirmLoading={save.isPending} okButtonProps={{ disabled: save.isPending || creating }} destroyOnHidden styles={{ body: { maxHeight: 'min(70dvh, calc(100dvh - var(--kb-inset, 0px) - 160px))', overflowY: 'auto' } }}>
      <Form form={form} initialValues={initial} layout="vertical" name="fund-operation">
        {position ? <p className="muted">{position.name} · {position.code}</p> : <>
          <Form.Item name="code" label="基金代码" rules={[{ required: true, message: '请输入基金代码' }]}>
            <Input placeholder="输入六位场外基金代码" onChange={() => { lookupRequest.current++; setMatches([]); setLooking(false); }} suffix={<Button size="small" type="text" loading={looking} onClick={() => void lookup()}>检索</Button>} />
          </Form.Item>
          {matches.length > 0 && <Space wrap className="fund-search-results">{matches.map((item) => <Button key={item.thscode} size="small" onClick={() => form.setFieldValue('code', item.thscode)}>{item.name} · {item.thscode}</Button>)}</Space>}
          <Form.Item name="display_name" label="显示名称（可选）"><Input maxLength={128} placeholder="可填写你常用的基金名称" /></Form.Item>
        </>}
        {(!position || isOpening) && <Form.Item name="basis_confirmed" valuePropName="checked" rules={[{ validator: (_, value) => value ? Promise.resolve() : Promise.reject(new Error('请确认估值口径')) }]}>
          <Checkbox>这是人民币场外净值型基金</Checkbox>
        </Form.Item>}
        {(!position || isOpening) && <p className="small muted">适用于股票、混合、债券和 ETF 联接等净值型持仓；货币基金、场内买卖和外币份额需使用其他估值方式。</p>}
        {isOpening && <Alert type="info" showIcon title="录入已有持仓" description="不会再次扣银行卡。历史从生效日期开始；缺少上一净值日的持仓时，不反算个人日盈亏。" />}
        <div className="fund-form-grid">
          {hasShares && <Form.Item name="shares" label={isOpening ? '已确认持有份额' : kind === 'redeem' ? '实际赎回份额' : '实际确认份额'} rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
          {isOpening && <Form.Item name="cost_price" label="持仓成本价" rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
          {!isOpening && kind !== 'settlement' && <Form.Item name="amount" label={kind === 'buy' ? '实际总投入（含费用）' : kind === 'redeem' ? '赎回成交金额（未扣费用）' : '实际分红金额'} rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
          {hasTradePrice && <Form.Item name="trade_nav" label="实际成交净值" rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
          {hasTradePrice && <Form.Item name="fee" label="实际费用" rules={quantityRules}><Input inputMode="decimal" /></Form.Item>}
          {kind !== 'cash_dividend' && <Form.Item name="effective_date" label={isOpening ? '持仓生效日期' : kind === 'settlement' ? '实际到账日期' : '份额生效日期'} rules={[{ required: true }]}><DateInput /></Form.Item>}
          {isDividend && <><Form.Item name="rights_date" label="除息 / 权益生效日期" rules={[{ required: true }]}><DateInput /></Form.Item><Form.Item name="registration_date" label="权益登记日期（可选）"><DateInput /></Form.Item></>}
        </div>
        {dividend && <Alert type="info" title={`公告每十份分红 ${dividend.per_ten_cash} 元`} description="请以个人实际权益、金额与分红方式核对，公告不会自动生成个人资金记录。" />}
        {(kind === 'redeem' || kind === 'cash_dividend') && <Form.Item name="settled" label="资金已到账" valuePropName="checked"><Switch /></Form.Item>}
        {hasCash && <>
          {kind !== 'settlement' && <Form.Item name="cash_date" label={kind === 'buy' ? '实际付款日期' : '实际到账日期'} rules={[{ required: true }]}><DateInput /></Form.Item>}
          <Form.Item name="cash_account_id" label={kind === 'buy' ? '付款账户' : '到账账户'} rules={[{ required: true, message: '请选择本人现金资产账户' }]}>
            <AccountSelect assetOnly sheetTitle="本人现金资产账户" active options={(accounts.data ?? []).filter((a) => isCashAccount(a) && a.status === 'active').map((a) => ({ value: a.id, label: a.name }))} onCreatingChange={setCreating} loading={accounts.isLoading} errorMessage={accounts.isError ? errMsg(accounts.error) : undefined} onRetry={() => void accounts.refetch()} />
          </Form.Item>
        </>}
        {values?.settled === false && (kind === 'redeem' || kind === 'cash_dividend') && <Alert type="info" title="保留为待结算资产" description="实际到账后，在事件记录中登记到账日期和账户，资金只转入一次。" />}
        <Form.Item name="note" label="备注"><Input maxLength={255} /></Form.Item>
      </Form>
    </Modal>
  </SelectionScope>;
}
