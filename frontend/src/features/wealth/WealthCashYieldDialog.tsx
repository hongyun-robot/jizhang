import { useEffect, useRef, useState } from 'react';
import { Alert, App, Input, Modal } from 'antd';
import { useWealthCashMutations, useWealthCashYield } from '../../api/wealth';
import { errMsg } from '../../api/client';
import { QueryBoundary } from '../../components/QueryState';
import '../../styles/wealth.css';

export default function WealthCashYieldDialog({ id, onClose, balance }: { id: number; onClose: () => void; balance?: string }) {
  const query = useWealthCashYield(id);
  const { edit } = useWealthCashMutations();
  const { message } = App.useApp();
  const [amount, setAmount] = useState(balance ?? '');
  const [note, setNote] = useState('');
  const busy = useRef(false);
  const request = useRef({ payload: '', key: '' });
  const revision = useRef<number | undefined>(undefined);
  if (query.data && revision.current === undefined) revision.current = query.data.revision;
  useEffect(() => { if (query.data) { setAmount(balance ?? query.data.amount); setNote(query.data.note); } }, [query.data?.id, balance]);
  const save = async () => {
    if (busy.current || !query.data) return;
    if (!/^-?\d+(\.\d{1,2})?$/.test(amount)) { message.error('请输入金额，最多两位小数；负数表示亏损'); return; }
    const payload = { expected_revision: revision.current!, ...(balance === undefined ? { amount } : { balance: amount }), note };
    const encoded = JSON.stringify(payload);
    if (request.current.payload !== encoded) request.current = { payload: encoded, key: crypto.randomUUID() };
    busy.current = true;
    try { await edit.mutateAsync({ id, input: { ...payload, request_key: request.current.key } }); message.success(balance === undefined ? '收益已更新，账户余额同步调整' : '余额已校准，差额已记录到收益流水'); onClose(); }
    catch (error) { message.error(errMsg(error)); }
    finally { busy.current = false; }
  };
  return <Modal open title={balance === undefined ? '修改理财收益' : '校准账户余额'} onCancel={() => { if (!edit.isPending) onClose(); }} onOk={() => void save()} confirmLoading={edit.isPending} okButtonProps={{ disabled: !query.data }} destroyOnHidden>
    <QueryBoundary query={query}>{query.data && <div className="wealth-cash-form">
      <p className="muted">净值区间 {query.data.previous_nav_date.slice(0, 10)} → {query.data.nav_date.slice(0, 10)}，自动估算 ¥{query.data.calculated_amount}</p>
      <label>{balance === undefined ? '实际收益（元，亏损填负数）' : '当前实际余额（元）'}<Input aria-label={balance === undefined ? '实际理财收益' : '当前实际余额'} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} /></label>
      <label>备注<Input.TextArea aria-label="收益调整备注" value={note} maxLength={255} onChange={(e) => setNote(e.target.value)} /></label>
      <Alert type="info" showIcon title={balance === undefined ? '手动金额将保留，后续同步只更新自动估算供核对。可填 0，收益明细仍会保留。' : '差额会记入这笔收益，后续同步保留手动金额。若遗漏了收支，请先补记收支。'} />
    </div>}</QueryBoundary>
  </Modal>;
}
