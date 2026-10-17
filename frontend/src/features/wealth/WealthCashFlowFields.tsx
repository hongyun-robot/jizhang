import { useEffect, useRef, useState } from 'react';
import { Alert, Input, Select } from 'antd';
import dayjs from 'dayjs';
import DatePicker from '../../components/DatePicker';
import type { TxnInput } from '../../api/transactions';
import type { WealthCashFlow } from '../../api/types';
import '../../styles/wealth.css';

export default function WealthCashFlowFields({ accountId, outgoing, refund, existing, entryDate, onChange }: {
  accountId: number; outgoing: boolean; refund: boolean; existing?: WealthCashFlow; entryDate: string;
  onChange: (value: TxnInput['wealth_cash']) => void;
}) {
  const [clock, setClock] = useState(existing?.application_time ?? (entryDate === dayjs().format('YYYY-MM-DD') ? dayjs().format('HH:mm:ss') : '12:00:00'));
  const [mode, setMode] = useState<'quick' | 'ordinary'>(existing?.transfer_mode ?? 'quick');
  const [confirmation, setConfirmation] = useState(existing?.confirmation_date?.slice(0, 10));
  const [rights, setRights] = useState(existing?.rights_end_date?.slice(0, 10));
  const clockEdited = useRef(!!existing);
  useEffect(() => { if (!clockEdited.current) setClock(entryDate === dayjs().format('YYYY-MM-DD') ? dayjs().format('HH:mm:ss') : '12:00:00'); }, [entryDate]);
  useEffect(() => { onChange({ account_id: accountId, application_time: clock, transfer_mode: outgoing ? mode : 'quick', confirmation_date: confirmation, rights_end_date: outgoing && mode === 'ordinary' ? rights : undefined }); }, [accountId, clock, mode, outgoing, confirmation, rights, onChange]);
  return <div className="wealth-cash-form">
    <Alert type="info" showIcon title={outgoing ? '理财零钱默认快速转出，当天停止对应收益' : refund ? '退款资金不会自动再次申购' : '理财零钱转入：确认后才计收益'} description={outgoing ? '优先使用待确认资金，剩余部分减少已确认计收益资金。' : '未提供确认日期时按规则估算，可按机构的实际确认日期修正。'} />
    <label>申请时间<Input aria-label="理财零钱申请时间" type="time" step={1} value={clock} onChange={(e) => { clockEdited.current = true; setClock(e.target.value.length === 5 ? `${e.target.value}:00` : e.target.value); }} /></label>
    {outgoing ? <>
      <label>转出方式<Select aria-label="理财零钱转出方式" value={mode} options={[{ value: 'quick', label: '快速转出（默认）' }, { value: 'ordinary', label: '普通转出（按实际收益截止日）' }]} onChange={setMode} /></label>
      {mode === 'ordinary' && <label>实际收益截止日<DatePicker aria-label="实际收益截止日" value={rights ? dayjs(rights) : null} onChange={(d) => setRights(d?.format('YYYY-MM-DD'))} /></label>}
    </> : !refund && <label>实际份额确认日（可选）<DatePicker aria-label="实际份额确认日" value={confirmation ? dayjs(confirmation) : null} disabledDate={(d) => d.isBefore(dayjs(entryDate), 'day')} onChange={(d) => setConfirmation(d?.format('YYYY-MM-DD'))} /></label>}
  </div>;
}
