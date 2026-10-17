import { useEffect, useRef } from 'react';
import { Alert, App, Form, Input, Modal } from 'antd';
import { wealthToday } from '../../domain/wealth';
import { DateInput } from '../../components/DatePicker';
import SelectionScope from '../../components/SelectionScope';
import { useWealthMutations, type WealthManual } from '../../api/wealth';
import { useScope } from '../../api/keys';
import { errMsg } from '../../api/client';
import { submissionGate } from '../../lib/submission';
import { useVisualViewportKeyboard } from '../../hooks/useVisualViewportKeyboard';

export default function ManualNAVForm({ productId, record, withdraw = false, onClose }: { productId: number; record?: WealthManual; withdraw?: boolean; onClose: () => void }) {
  const [form] = Form.useForm<{ nav_date: string; unit_nav: string; source_note: string }>();
  const { message } = App.useApp();
  const { manual } = useWealthMutations();
  const scope = useScope(); const currentScope = useRef(scope); currentScope.current = scope;
  const alive = useRef(true); const key = useRef(crypto.randomUUID()); const gate = useRef(submissionGate());
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useVisualViewportKeyboard(true);
  const submit = () => gate.current(async () => {
    try {
      const v = await form.validateFields();
      await manual.mutateAsync({ productId, input: { ...v, unit_nav: withdraw ? undefined : v.unit_nav, request_key: key.current, expected_revision: record?.revision || 0, withdraw } });
      if (!alive.current || currentScope.current !== scope) return;
      message.success(withdraw ? '本人补录已撤回' : '本人补录已保存'); onClose();
    } catch (e) { if (e && typeof e === 'object' && 'errorFields' in e) return; if (alive.current && currentScope.current === scope) message.error(errMsg(e)); }
  });
  return <SelectionScope active><Modal open title={withdraw ? '撤回本人净值补录' : record ? '更正本人净值补录' : '补录缺失净值'} onCancel={onClose} onOk={submit} okButtonProps={{ 'aria-label': withdraw ? '确认撤回' : '保存补录', loading: manual.isPending, disabled: manual.isPending }} okText={withdraw ? '确认撤回' : '保存补录'} destroyOnHidden>
    <Alert type="info" title="作用于本人全部账户中的该产品" description="其他账号不受影响。只补缺失日期；自动同日净值到齐后优先采用自动值，原补录留审计且不能再更改。" />
    <Form form={form} layout="vertical" name="wealth-manual-nav" initialValues={{ nav_date: record?.nav_date.slice(0, 10) || wealthToday(), unit_nav: record?.unit_nav || '', source_note: record ? '' : '' }}>
      <Form.Item name="nav_date" label="真实净值日期" rules={[{ required: true }]}><DateInput disabled={!!record} /></Form.Item>
      {!withdraw && <Form.Item name="unit_nav" label="单位净值（不填累计净值）" rules={[{ required: true }, { pattern: /^\d+(?:\.\d{1,8})?$/, message: '请填写正数，最多八位小数' }]}><Input inputMode="decimal" /></Form.Item>}
      <Form.Item name="source_note" label={withdraw ? '撤回原因' : record ? '来源及更正原因' : '来源说明'} rules={[{ required: true, whitespace: true, message: '请说明核对来源或原因' }]}><Input.TextArea maxLength={255} rows={3} /></Form.Item>
    </Form>
  </Modal></SelectionScope>;
}
