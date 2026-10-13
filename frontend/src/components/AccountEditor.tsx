import { useEffect, useRef, useState } from 'react';
import { App, Alert, Button, Form, Input, InputNumber, Modal, Segmented, Switch } from 'antd';
import SelectionScope from './SelectionScope';
import ResponsiveSelect from './ResponsiveSelect';
import PrincipalRemainderField from './PrincipalRemainderField';
import { useAccountMutations, type AccountInput } from '../api/accounts';
import { useLiabilityDetail, useLiabilityTypes } from '../api/liability';
import type { Account, AccountType, PrincipalRemainder } from '../api/types';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import { submissionGate } from '../lib/submission';
import { useRef as submissionRef } from 'react';
import { plannedBills, ruleError } from '../lib/liability';
import { popupInParent } from '../lib/popup';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import dayjs from 'dayjs';

interface FormValues {
  name: string;
  type: AccountType;
  asset_class?: 'cash' | 'fund';
  opening_balance?: string;
  repayment_day?: number;
  liability_type?: string;
  statement_day?: number;
  repayment_month_offset?: number;
  principal_remainder?: PrincipalRemainder;
  sort?: number;
  status?: boolean;
}

export interface AccountEditorProps {
  account?: Account;
  initialName?: string;
  assetOnly?: boolean;
  allowFunds?: boolean;
  fundOnly?: boolean;
  onClose: () => void;
  onCreated?: (account: Account) => void;
  onPending?: (pending: boolean) => void;
}
export default function AccountEditor({ account, initialName = '', assetOnly = false, allowFunds = false, fundOnly = false, onClose, onCreated, onPending }: AccountEditorProps) {
  const scope = useScope();
  const scopeRef = useRef(scope);
  scopeRef.current = scope;
  const alive = useRef(true);
  const requestKey = useRef(crypto.randomUUID());
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);
  const submitGate = submissionRef(submissionGate());

  const { message } = App.useApp();
  const types = useLiabilityTypes();
  const defaultType = types.data?.[0]?.code;
  const { create, update } = useAccountMutations();

  const [open, setOpen] = useState(false);
  useVisualViewportKeyboard(open);
  const [editing, setEditing] = useState<Account | null>(null);
  const [form] = Form.useForm<FormValues>();
  const values = Form.useWatch([], form) as FormValues | undefined;
  const editingDetail = useLiabilityDetail(
    editing?.type === 'liability' ? editing.id : 0
  );
  const touchedFields = useRef(new Set<string>());
  const filledAccount = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (
      open &&
      defaultType &&
      form.getFieldValue('type') === 'liability' &&
      !form.getFieldValue('liability_type')
    )
      form.setFieldValue('liability_type', defaultType);
  }, [open, defaultType, form]);
  const ruleValid =
    values?.type === 'liability' &&
    !ruleError(
      values.statement_day,
      values.repayment_day,
      values.repayment_month_offset
    );
  const currentEnd = plannedBills(
    editingDetail.data?.rules ?? [],
    dayjs().format('YYYY-MM-DD'),
    1
  )[0]?.close;
  const boundary = currentEnd
    ? dayjs(currentEnd).add(1, 'day').format('YYYY-MM-DD')
    : dayjs().format('YYYY-MM-DD');
  const nextRule = ruleValid
    ? [
        {
          id: 0,
          version: 9999,
          effective_from: boundary,
          statement_day: values!.statement_day!,
          repayment_day: values!.repayment_day!,
          repayment_month_offset: values!.repayment_month_offset!,
        },
      ]
    : [];
  const futureChanges = (editingDetail.data?.statements ?? [])
    .filter(
      (b) => !b.issued_at && b.kind === 'monthly' && b.cycle_end >= boundary
    )
    .map((b) => ({
      old: b,
      preview: plannedBills(
        nextRule,
        b.cycle_start < boundary ? boundary : b.cycle_start,
        1
      )[0],
    }));
  useEffect(() => {
    if (
      !open ||
      !editing ||
      editing.type !== 'liability' ||
      filledAccount.current === editing.id ||
      editingDetail.data?.account.id !== editing.id
    )
      return;
    const rule = editingDetail.data.rules.at(-1);
    if (rule) {
      const defaults = {
        statement_day: rule.statement_day,
        repayment_day: rule.repayment_day,
        repayment_month_offset: rule.repayment_month_offset,
      };
      form.setFieldsValue(
        Object.fromEntries(
          Object.entries(defaults).filter(
            ([key]) => !touchedFields.current.has(key)
          )
        )
      );
    }
    filledAccount.current = editing.id;
  }, [open, editing, editingDetail.data, form]);

  const openCreate = () => {
    setEditing(null);
    touchedFields.current.clear();
    form.resetFields();
    form.setFieldsValue({
      name: initialName,
      type: 'asset',
      asset_class: fundOnly ? 'fund' : 'cash',
      opening_balance: '0',
      sort: 0,
      liability_type: defaultType,
      statement_day: 31,
      repayment_day: 10,
      repayment_month_offset: 1,
      principal_remainder: 'last',
    });
    setOpen(true);
  };
  const openEdit = (a: Account) => {
    filledAccount.current = undefined;
    setEditing(a);
    touchedFields.current.clear();
    form.resetFields();
    form.setFieldsValue({
      name: a.name,
      type: a.type,
      asset_class: a.asset_class || 'cash',
      opening_balance: a.opening_balance,
      repayment_day: a.repayment_day ?? undefined,
      liability_type: a.liability_type || defaultType,
      statement_day: a.billing_enabled ? undefined : 31,
      repayment_month_offset: 1,
      principal_remainder: a.principal_remainder ?? 'last',
      sort: a.sort,
      status: a.status === 'active',
    });
    setOpen(true);
  };

  const submit = () =>
    submitGate.current(async () => {
      try {
        const v = await form.validateFields();
        if (v.type === 'liability') {
          const error = ruleError(
            v.statement_day,
            v.repayment_day,
            v.repayment_month_offset
          );
          if (error) {
            message.warning(error);
            return;
          }
        }
        const input: AccountInput = {
          name: v.name.trim(),
          request_key: editing ? undefined : requestKey.current,
          type: v.type,
          asset_class: v.type === 'asset' ? v.asset_class || 'cash' : undefined,
          opening_balance: v.opening_balance ?? '0',
          sort: v.sort ?? 0,
          status: v.status === false ? 'disabled' : 'active',
        };
        if (input.asset_class === 'fund') input.opening_balance = '0';
        if (v.type === 'liability') {
          const rules = editingDetail.data?.rules;
          const last = rules?.at(-1);
          input.opening_balance = '0';
          input.liability_type = v.liability_type;
          input.repayment_day = v.repayment_day;
          input.statement_day = v.statement_day;
          input.repayment_month_offset = v.repayment_month_offset;
          input.principal_remainder = v.principal_remainder;
          if (
            editing?.billing_enabled &&
            last &&
            last.statement_day === v.statement_day &&
            last.repayment_day === v.repayment_day &&
            last.repayment_month_offset === v.repayment_month_offset &&
            editing.liability_type === input.liability_type
          ) {
            input.statement_day = undefined;
            input.repayment_month_offset = undefined;
            input.repayment_day = editing.repayment_day ?? undefined;
          }
        }
        if (editing) {
          await update.mutateAsync({ id: editing.id, input });
          if (!alive.current || scopeRef.current !== scope) return;
          message.success('账户已更新');
        } else {
          const saved = await create.mutateAsync(input);
          if (!alive.current || scopeRef.current !== scope) return;
          onCreated?.(saved);
          message.success('账户已创建');
        }
        onClose();
      } catch (e) {
        if (e && typeof e === 'object' && 'errorFields' in e) return;
        if (alive.current && scopeRef.current === scope) message.error(errMsg(e));
      }
    });

  useEffect(() => { if (account) openEdit(account); else openCreate(); }, []);
  useEffect(() => { onPending?.(create.isPending || update.isPending); return () => onPending?.(false); }, [create.isPending, update.isPending, onPending]);
  return (
      <SelectionScope active={open}>
        <Modal
          open={open}
          onCancel={onClose}
          onOk={submit}
          title={editing ? '编辑账户' : '新建账户'}
          okText="保存"
          modalRender={(content) => <div style={{ display: 'contents' }} onKeyDown={(event) => event.stopPropagation()}>{content}</div>}
          okButtonProps={{ loading: create.isPending || update.isPending, disabled: create.isPending || update.isPending }}
          destroyOnHidden
          styles={{ body: { maxHeight: 'min(65dvh, calc(100dvh - var(--kb-inset, 0px) - 160px))', overflowY: 'auto' } }}
        >
          <Form
            name="account-editor"
            form={form}
            layout="vertical"
            onValuesChange={(changed) => {
              Object.keys(changed).forEach((key) =>
                touchedFields.current.add(key)
              );
            }}
          >
            <Form.Item
              name="name"
              label="名称"
              rules={[{ required: true, whitespace: true }, { validator: (_, value: string) => Array.from((value ?? '').trim()).length <= 64 ? Promise.resolve() : Promise.reject(new Error('名称最多 64 字')) }]}
            >
              <Input placeholder="如：银行卡 / 信用卡 / 花呗 / 抖音月付" />
            </Form.Item>
            <Form.Item name="type" label="类型">
              <Segmented
                disabled={assetOnly || fundOnly || !!editing?.has_financial_history}
                onChange={(value) => {
                  if (value === 'liability')
                    form.setFieldsValue({
                      opening_balance: '0',
                      liability_type: defaultType,
                      statement_day: 31,
                      repayment_day: 10,
                      repayment_month_offset: 1,
                      principal_remainder: 'last',
                    });
                }}
                options={[
                  { value: 'asset', label: '资产' },
                  { value: 'liability', label: '负债' },
                ]}
              />
            </Form.Item>
            {values?.type === 'asset' && (allowFunds || fundOnly || editing?.asset_class === 'fund') && <Form.Item name="asset_class" label="资产类别"><Segmented disabled={fundOnly || !!editing?.has_financial_history} options={[{ value: 'cash', label: '现金资产' }, { value: 'fund', label: '基金账户' }]} onChange={(value) => { if (value === 'fund') form.setFieldValue('opening_balance', '0'); }} /></Form.Item>}
            {values?.asset_class === 'fund' && values?.type === 'asset' && <Alert type="info" title="在基金详情添加持仓" description="基金账户从零现金开始，市值按确认份额和最新有效净值计算。" />}
            <Form.Item
              name="opening_balance"
              label="期初余额（新负债账户从零开始）"
            >
              <Input
                placeholder="0.00"
                inputMode="decimal"
                disabled={
                  !!editing?.has_financial_history ||
                  values?.type === 'liability' || values?.asset_class === 'fund'
                }
              />
            </Form.Item>
            <Form.Item noStyle shouldUpdate={(a, b) => a.type !== b.type}>
              {({ getFieldValue }) =>
                getFieldValue('type') === 'liability' ? (
                  <>
                    <Alert
                      type="info"
                      title="各平台规则由你填写"
                      description="消费计支出、还款记转账。日期支持 1–31 日，短月份取月底；已有账单保留，修改规则从下一周期生效。"
                    />
                    <Form.Item
                      name="liability_type"
                      label="消费信用类型"
                      rules={[{ required: true }]}
                    >
                      <ResponsiveSelect
                        sheetTitle="消费信用类型"
                        mobileSearch={false}
                        active={open}
                        errorMessage={
                          types.isError ? errMsg(types.error) : undefined
                        }
                        onRetry={() => {
                          void types.refetch();
                        }}
                        getPopupContainer={popupInParent}
                        loading={types.isLoading}
                        options={(types.data ?? []).map((t) => ({
                          value: t.code,
                          label: t.name,
                        }))}
                      />
                    </Form.Item>
                    {types.isError && (
                      <Alert
                        type="error"
                        title="消费信用类型查询失败"
                        description={
                          <Button onClick={() => void types.refetch()}>
                            重新查询
                          </Button>
                        }
                      />
                    )}
                    {types.data?.length === 0 && (
                      <Alert type="warning" title="尚未配置消费信用类型" />
                    )}
                    <Form.Item
                      name="statement_day"
                      label="账单日（1–31）"
                      rules={[{ required: true }]}
                    >
                      <InputNumber min={1} max={31} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item
                      name="repayment_month_offset"
                      label="还款月份"
                      rules={[{ required: true }]}
                    >
                      <ResponsiveSelect
                        sheetTitle="还款月份"
                        mobileSearch={false}
                        active={open}
                        getPopupContainer={popupInParent}
                        options={[
                          { value: 0, label: '出账当月' },
                          { value: 1, label: '出账次月' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item
                      name="repayment_day"
                      label="还款日（1–31）"
                      rules={[{ required: true }]}
                    >
                      <InputNumber
                        min={1}
                        max={31}
                        style={{ width: '100%' }}
                        placeholder="如：10"
                      />
                    </Form.Item>
                    <PrincipalRemainderField />
                    {ruleError(
                      values?.statement_day,
                      values?.repayment_day,
                      values?.repayment_month_offset
                    ) ? (
                      <Alert
                        type="warning"
                        title={ruleError(
                          values?.statement_day,
                          values?.repayment_day,
                          values?.repayment_month_offset
                        )}
                      />
                    ) : (
                      <p className="small muted">
                        周期预览：
                        {plannedBills(
                          [
                            {
                              id: 0,
                              version: 1,
                              effective_from: '2000-01-01',
                              statement_day: values!.statement_day!,
                              repayment_day: values!.repayment_day!,
                              repayment_month_offset:
                                values!.repayment_month_offset!,
                            },
                          ],
                          dayjs().startOf('month').format('YYYY-MM-DD'),
                          3
                        )
                          .map((b) => `${b.close} 出账 → ${b.due} 还款`)
                          .join('；')}
                        。已有账单保持原规则。
                      </p>
                    )}
                    {ruleValid && editing?.billing_enabled && (
                      <p className="small muted">
                        新规则自 {boundary} 生效。
                        {futureChanges
                          .map(({ old, preview }) =>
                            preview
                              ? `未出账 ${old.cycle_end} → ${preview.close}（到期 ${old.due_date} → ${preview.due}）`
                              : ''
                          )
                          .join('；')}
                        。期次本金保持原金额，实际费用日期需要按平台情况核对。
                      </p>
                    )}
                  </>
                ) : null
              }
            </Form.Item>
            <Form.Item name="sort" label="排序（小在前）">
              <InputNumber style={{ width: '100%' }} />
            </Form.Item>
            {editing && (
              <Form.Item name="status" label="启用" valuePropName="checked">
                <Switch />
              </Form.Item>
            )}
          </Form>
        </Modal>
      </SelectionScope>
  );
}
