import SelectionScope from '../components/SelectionScope';
import ResponsiveSelect from '../components/ResponsiveSelect';
import { submissionGate } from '../lib/submission';
import { useRef as submissionRef } from 'react';
import { cents, money } from '../lib/money';
import { QueryBoundary } from '../components/QueryState';
import { useEffect, useRef, useState } from 'react';
import {
  App,
  Button,
  Card,
  Form,
  Grid,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Switch,
  Alert,
  Table,
  Tag,
} from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import {
  useAccountMutations,
  useAccounts,
  type AccountInput,
} from '../api/accounts';
import type { Account, AccountType } from '../api/types';
import Money from '../components/Money';
import { useNavigate } from 'react-router-dom';
import { useLiabilityDetail, useLiabilityTypes } from '../api/liability';
import dayjs from 'dayjs';
import { plannedBills, ruleError } from '../lib/liability';
import { popupInParent } from '../lib/popup';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';

interface FormValues {
  name: string;
  type: AccountType;
  opening_balance?: string;
  repayment_day?: number;
  liability_type?: string;
  statement_day?: number;
  repayment_month_offset?: number;
  sort?: number;
  status?: boolean;
}

/** 账户管理（仅管理员，specs/accounts）：余额为负按欠款呈现。 */
export default function Accounts() {
  const submitGate = submissionRef(submissionGate());

  const navigate = useNavigate();
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const accounts = useAccounts();
  const types = useLiabilityTypes();
  const labels: Record<string, string> = Object.fromEntries(
    (types.data ?? []).map((t) => [t.code, t.name])
  );
  const defaultType = types.data?.[0]?.code;
  const { create, update, remove } = useAccountMutations();

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
      name: '',
      type: 'asset',
      opening_balance: '0',
      sort: 0,
      liability_type: defaultType,
      statement_day: 31,
      repayment_day: 10,
      repayment_month_offset: 1,
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
      opening_balance: a.opening_balance,
      repayment_day: a.repayment_day ?? undefined,
      liability_type: a.liability_type || defaultType,
      statement_day: a.billing_enabled ? undefined : 31,
      repayment_month_offset: 1,
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
          name: v.name,
          type: v.type,
          opening_balance: v.opening_balance ?? '0',
          sort: v.sort ?? 0,
          status: v.status === false ? 'disabled' : 'active',
        };
        if (v.type === 'liability') {
          const rules = editingDetail.data?.rules;
          const last = rules?.at(-1);
          input.opening_balance = '0';
          input.liability_type = v.liability_type;
          input.repayment_day = v.repayment_day;
          input.statement_day = v.statement_day;
          input.repayment_month_offset = v.repayment_month_offset;
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
          message.success('账户已更新');
        } else {
          await create.mutateAsync(input);
          message.success('账户已创建');
        }
        setOpen(false);
      } catch (e) {
        if (e && typeof e === 'object' && 'errorFields' in e) return;
        message.error(errMsg(e));
      }
    });

  const doDelete = async (a: Account) => {
    if (remove.isPending) return; // 防重：确认进行中忽略重复触发
    try {
      await remove.mutateAsync(a.id);
      message.success(`已删除「${a.name}」`);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  /** 负债欠款按红字“欠款”呈现；资产为负同样红字。 */
  const renderBalance = (a: Account) => {
    const n = cents(a.balance ?? '0');
    if (n < 0n) {
      return (
        <span className="money money-expense">
          {a.type === 'liability' ? '欠 ' : '-'}¥
          <Money value={money(-n)} tone="plain" currency={false} />
        </span>
      );
    }
    return <Money value={money(n)} tone="plain" />;
  };

  const columns = [
    {
      title: '账户',
      dataIndex: 'name',
      key: 'name',
      width: '38%',
      render: (name: string, a: Account) =>
        a.type === 'liability' ? (
          <>
            <Button
              type="link"
              onClick={() => navigate(`/liabilities/${a.id}`)}
            >
              {name}
            </Button>
            <div className="small muted">
              {labels[a.liability_type ?? ''] || '消费信用'} ·{' '}
              {a.billing_enabled ? '查看账单与分期' : '待配置账单'}
            </div>
          </>
        ) : (
          name
        ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      width: '9%',
      render: (t: AccountType) =>
        t === 'asset' ? <Tag>资产</Tag> : <Tag color="volcano">负债</Tag>,
    },
    {
      title: '余额',
      key: 'balance',
      width: '15%',
      align: 'right' as const,
      render: (_: unknown, a: Account) => renderBalance(a),
    },
    {
      title: '还款日',
      dataIndex: 'repayment_day',
      key: 'repayment_day',
      width: '12%',
      render: (d?: number | null) => (d ? `每月 ${d} 日` : '—'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: '9%',
      render: (s: string) =>
        s === 'active' ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '',
      key: 'actions',
      width: '17%',
      align: 'right' as const,
      render: (_: unknown, a: Account) => (
        <>
          <Button type="link" size="small" onClick={() => openEdit(a)}>
            编辑
          </Button>
          <Popconfirm
            title={`删除「${a.name}」？`}
            onConfirm={() => doDelete(a)}
            okButtonProps={{
              loading: remove.isPending,
              disabled: remove.isPending,
            }}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ];

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            ACCOUNTS · {(accounts.data ?? []).length} 个账户
          </div>
          <h2>账户管理</h2>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建账户
        </Button>
      </div>

      <QueryBoundary query={accounts}>
        <Card size="small" className="panel panel--measure">
          {isMobile ? (
            <div className="acct-list">
              {(accounts.data ?? []).map((a) => (
                <div className="acct-row" key={a.id}>
                  {/* 行级点击语义化：与 CategoryPicker 同样的 role/键盘处理，焦点环由通配 :focus-visible 兜底 */}
                  <div
                    className="acct-main"
                    role="button"
                    tabIndex={0}
                    onClick={() => openEdit(a)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        openEdit(a);
                      }
                    }}
                  >
                    <div className="acct-name">
                      {a.name}
                      <Tag
                        color={a.type === 'liability' ? 'volcano' : undefined}
                        style={{ marginLeft: 8 }}
                      >
                        {a.type === 'asset' ? '资产' : '负债'}
                      </Tag>
                      {a.status === 'disabled' && <Tag>停用</Tag>}
                    </div>
                    <div className="acct-meta">
                      {a.repayment_day ? `还款日 每月${a.repayment_day}日` : ''}
                      {a.type === 'liability' && (
                        <Button
                          type="link"
                          size="small"
                          onClick={(e) => {
                            e.stopPropagation();
                            navigate(`/liabilities/${a.id}`);
                          }}
                        >
                          账单与分期
                        </Button>
                      )}
                    </div>
                  </div>
                  <div className="acct-right">
                    <div className="acct-balance">{renderBalance(a)}</div>
                    <Popconfirm
                      title={`删除「${a.name}」？`}
                      onConfirm={() => doDelete(a)}
                      okButtonProps={{
                        loading: remove.isPending,
                        disabled: remove.isPending,
                      }}
                    >
                      <Button type="link" size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <Table
              rowKey="id"
              size="small"
              loading={accounts.isLoading}
              dataSource={accounts.data ?? []}
              columns={columns}
              pagination={false}
            />
          )}
        </Card>
      </QueryBoundary>

      <SelectionScope active={open}>
        <Modal
          open={open}
          onCancel={() => setOpen(false)}
          onOk={submit}
          title={editing ? '编辑账户' : '新建账户'}
          confirmLoading={create.isPending || update.isPending}
          destroyOnHidden
        >
          <Form
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
              rules={[{ required: true, max: 64 }]}
            >
              <Input placeholder="如：银行卡 / 信用卡 / 花呗 / 抖音月付" />
            </Form.Item>
            <Form.Item name="type" label="类型">
              <Segmented
                disabled={!!editing?.has_financial_history}
                onChange={(value) => {
                  if (value === 'liability')
                    form.setFieldsValue({
                      opening_balance: '0',
                      liability_type: defaultType,
                      statement_day: 31,
                      repayment_day: 10,
                      repayment_month_offset: 1,
                    });
                }}
                options={[
                  { value: 'asset', label: '资产' },
                  { value: 'liability', label: '负债' },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="opening_balance"
              label="期初余额（新负债账户从零开始）"
            >
              <Input
                placeholder="0.00"
                inputMode="decimal"
                disabled={
                  !!editing?.has_financial_history ||
                  editing?.type === 'liability'
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
    </div>
  );
}
