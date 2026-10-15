import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Form, Input, Segmented } from 'antd';
import dayjs from 'dayjs';
import { useAccounts } from '../../api/accounts';
import { useGoldAccounts, useGoldMutations, type GoldAccount, type GoldInput, type GoldKind } from '../../api/gold';
import { errMsg } from '../../api/client';
import { useScope } from '../../api/keys';
import { useEditorSession } from '../../hooks/useEditorSession';
import { submissionGate } from '../../lib/submission';
import { goldNumberValid, goldRatio, goldTotal } from '../../domain/gold';
import { useAuth } from '../../stores/auth';
import { canRecord } from '../../domain/permissions';
import AccountEditor from '../../components/AccountEditor';
import AccountSelect from '../../components/AccountSelect';
import ResponsiveSelect from '../../components/ResponsiveSelect';
import DatePicker from '../../components/DatePicker';
import GoldQuotePanel from './GoldQuotePanel';
import '../../styles/gold.css';

interface Draft { account?: number; grams: string; amount: string; fee: string; date: string; cash?: number; note: string; costMode: 'total' | 'unit' }
const blank = (): Draft => ({ grams: '', amount: '', fee: '', date: dayjs().format('YYYY-MM-DD'), note: '', costMode: 'total' });
export const GOLD_LABEL: Record<GoldKind, string> = { opening: '录入已有持仓', buy: '买入黄金', sell: '卖出黄金' };
export default function GoldForm({ kind, open, active = true, account, amend = false, onSaved }: { kind: GoldKind; open: boolean; active?: boolean; account?: GoldAccount; amend?: boolean; onSaved: () => void }) {
  const scope = useScope();
  const editor = useEditorSession(open && active, `${kind}:${account?.account.id ?? 'select'}:${amend}`);
  const accounts = useAccounts();
  const gold = useGoldAccounts(open && active && !account);
  const { save } = useGoldMutations();
  const user = useAuth((state) => state.user);
  const { message } = App.useApp();
  const [drafts, setDrafts] = useState<Record<GoldKind, Draft>>({ opening: blank(), buy: blank(), sell: blank() });
  const keys = useRef<Record<GoldKind, string>>({ opening: crypto.randomUUID(), buy: crypto.randomUUID(), sell: crypto.randomUUID() });
  const gate = useRef(submissionGate());
  const [creating, setCreating] = useState(false);
  const [cashCreating, setCashCreating] = useState(false);
  const [error, setError] = useState('');
  const d = drafts[kind];
  const patch = (value: Partial<Draft>) => { setError(''); setDrafts((all) => ({ ...all, [kind]: { ...all[kind], ...value } })); };
  useEffect(() => {
    if (!open) return;
    const initial = { opening: blank(), buy: blank(), sell: blank() };
    if (amend && account) { const prior = account.events.find((event) => event.kind === 'opening' && !event.voided_at); if (prior) initial.opening = { ...blank(), grams: prior.grams, amount: prior.cash_amount, date: prior.trade_date.slice(0, 10), note: prior.note }; }
    setDrafts(initial); setError(''); keys.current = { opening: crypto.randomUUID(), buy: crypto.randomUUID(), sell: crypto.randomUUID() };
  }, [open, scope, account?.account.id, amend]);
  const selectedId = account?.account.id ?? d.account;
  const selected = account ?? gold.data?.find((row) => row.account.id === selectedId);
  const cash = (accounts.data ?? []).filter((a) => a.type === 'asset' && (!a.asset_class || a.asset_class === 'cash') && a.status === 'active');
  const allowed = kind === 'opening' || canRecord(user?.role);
  const total = kind === 'opening' && d.costMode === 'unit' ? goldTotal(d.grams, d.amount) : d.amount;
  const perGram = kind === 'opening' && d.costMode === 'unit' ? d.amount : goldRatio(d.amount, d.grams);
  const submit = () => gate.current(async () => {
    if (!allowed || !selectedId || !goldNumberValid(d.grams) || !goldNumberValid(d.amount, kind === 'opening', kind === 'opening' && d.costMode === 'unit' ? 8 : 2) || (kind !== 'opening' && !d.cash) || (d.fee !== '' && !goldNumberValid(d.fee, true, 2))) { setError('请核对黄金账户、克数、成本/实际金额及现金账户。'); return; }
    const current = editor.capture();
    const input: GoldInput = { request_key: keys.current[kind], kind, grams: d.grams, trade_date: d.date, note: d.note, ...(kind === 'opening' ? { cost_mode: d.costMode, cost: d.amount } : { cash_amount: d.amount, cash_account_id: d.cash, ...(d.fee === '' ? {} : { fee: d.fee }) }) };
    try { await save.mutateAsync({ path: `/gold/accounts/${selectedId}/${kind === 'opening' ? 'opening' : 'events'}`, input, amend }); if (!current()) return; keys.current[kind] = crypto.randomUUID(); setDrafts((all) => ({ ...all, [kind]: blank() })); message.success('黄金记录已保存'); onSaved(); }
    catch (e) { if (current()) setError(errMsg(e)); }
  });
  return <div className="gold-form">
    <GoldQuotePanel active={open && active} />
    <Alert type="info" title={kind === 'opening' ? '建立持仓基准，不补扣现金' : '登记已完成交易，不向银行下单'} description={kind === 'sell' ? '填写实际净到账；手续费只作明细，不会再次扣减。' : kind === 'buy' ? '填写含手续费的实际总扣款；参考报价不会自动覆盖金额。' : '选择总成本或平均每克成本，另一项自动换算。'} />
    {error && <Alert type="error" title={error} />}
    <Form layout="vertical" onFinish={submit} disabled={save.isPending}>
      {!account && <Form.Item label="黄金账户" required><ResponsiveSelect active={open && active} sheetTitle="本人黄金账户" value={d.account} onChange={(value: number) => patch({ account: value })} options={(gold.data ?? []).filter((row) => kind === 'sell' ? Number(row.grams) > 0 : row.account.status === 'active').map((row) => ({ value: row.account.id, label: row.account.name }))} /><Button type="link" onClick={() => setCreating(true)}>新建建行积存金账户</Button></Form.Item>}
      <Form.Item label="持有克数 / 成交克数（克）" required><Input aria-label="黄金克数" inputMode="decimal" value={d.grams} onChange={(event) => patch({ grams: event.target.value })} /></Form.Item>
      {kind === 'opening' && <Form.Item label="成本录入方式"><Segmented value={d.costMode} options={[{ value: 'total', label: '总成本' }, { value: 'unit', label: '平均每克成本' }]} onChange={(value) => { const converted = value === 'unit' ? goldRatio(d.amount, d.grams) : goldTotal(d.grams, d.amount); patch({ costMode: value as 'total' | 'unit', amount: converted ?? '' }); }} /></Form.Item>}
      <Form.Item label={kind === 'opening' ? d.costMode === 'total' ? '总成本（元）' : '平均每克成本（元/克）' : kind === 'buy' ? '实际总扣款（含费用，元）' : '实际净到账（元）'} required><Input aria-label="黄金实际金额" inputMode="decimal" value={d.amount} onChange={(event) => patch({ amount: event.target.value })} /></Form.Item>
      <p className="small muted">{kind === 'sell' ? '净每克回款' : '平均每克成本'}：{perGram ?? '—'} 元/克{kind === 'opening' && ` · 确认总成本：${total || '—'} 元`}</p>
      {kind !== 'opening' && <><Form.Item label="已单列手续费（可选，元）"><Input aria-label="黄金手续费" inputMode="decimal" value={d.fee} placeholder="不填写表示未单列" onChange={(event) => patch({ fee: event.target.value })} /></Form.Item><Form.Item label={kind === 'buy' ? '付款现金账户' : '到账现金账户'} required><AccountSelect assetOnly active={open && active} sheetTitle="本人现金资产账户" value={d.cash} options={cash.map((a) => ({ value: a.id, label: a.name }))} onChange={(value: number) => patch({ cash: value })} onCreatingChange={setCashCreating} /></Form.Item></>}
      <Form.Item label={kind === 'opening' ? '持仓生效日期' : '成交及资金日期（同日）'} required><DatePicker value={dayjs(d.date)} allowClear={false} onChange={(date) => date && patch({ date: date.format('YYYY-MM-DD') })} /></Form.Item>
      <Form.Item label="备注"><Input.TextArea value={d.note} maxLength={255} onChange={(event) => patch({ note: event.target.value })} /></Form.Item>
      {kind === 'sell' && selected && <p>可卖出 {selected.grams} 克</p>}
      <Button type="primary" htmlType="submit" loading={save.isPending} disabled={!allowed || cashCreating || creating || (kind === 'sell' && (!selected || Number(selected.grams) <= 0))}>保存黄金记录</Button>
    </Form>
    {creating && <AccountEditor goldOnly onClose={() => setCreating(false)} onCreated={(a) => { patch({ account: a.id }); setCreating(false); }} />}
  </div>;
}
