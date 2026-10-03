import { useState } from 'react';
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
  Table,
  Tag,
} from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import { useAccountMutations, useAccounts, type AccountInput } from '../api/hooks';
import type { Account, AccountType } from '../api/types';
import Money from '../components/Money';

interface FormValues {
  name: string;
  type: AccountType;
  opening_balance?: string;
  repayment_day?: number;
  sort?: number;
  status?: boolean;
}

/** 账户管理（仅管理员，specs/accounts）：余额为负按欠款呈现。 */
export default function Accounts() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const accounts = useAccounts();
  const { create, update, remove } = useAccountMutations();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Account | null>(null);
  const [form] = Form.useForm<FormValues>();

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ name: '', type: 'asset', opening_balance: '0', sort: 0 });
    setOpen(true);
  };
  const openEdit = (a: Account) => {
    setEditing(a);
    form.setFieldsValue({
      name: a.name,
      type: a.type,
      opening_balance: a.opening_balance,
      repayment_day: a.repayment_day ?? undefined,
      sort: a.sort,
      status: a.status === 'active',
    });
    setOpen(true);
  };

  const submit = async () => {
    const v = await form.validateFields();
    const input: AccountInput = {
      name: v.name,
      type: v.type,
      opening_balance: v.opening_balance ?? '0',
      sort: v.sort ?? 0,
      status: v.status === false ? 'disabled' : 'active',
    };
    if (v.type === 'liability' && v.repayment_day) input.repayment_day = v.repayment_day;
    try {
      if (editing) {
        await update.mutateAsync({ id: editing.id, input });
        message.success('账户已更新');
      } else {
        await create.mutateAsync(input);
        message.success('账户已创建');
      }
      setOpen(false);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const doDelete = async (a: Account) => {
    try {
      await remove.mutateAsync(a.id);
      message.success(`已删除「${a.name}」`);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  /** 负债欠款按红字“欠款”呈现；资产为负同样红字。 */
  const renderBalance = (a: Account) => {
    const n = Number(a.balance ?? 0);
    if (n < 0) {
      return (
        <span className="money money-expense">
          {a.type === 'liability' ? '欠 ' : '-'}¥
          <Money value={Math.abs(n)} tone="plain" currency={false} />
        </span>
      );
    }
    return <Money value={n} tone="plain" />;
  };

  const columns = [
    { title: '账户', dataIndex: 'name', key: 'name' },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      render: (t: AccountType) => (t === 'asset' ? <Tag>资产</Tag> : <Tag color="volcano">负债</Tag>),
    },
    { title: '余额', key: 'balance', render: (_: unknown, a: Account) => renderBalance(a) },
    {
      title: '还款日',
      dataIndex: 'repayment_day',
      key: 'repayment_day',
      render: (d?: number | null) => (d ? `每月 ${d} 日` : '—'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => (s === 'active' ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>),
    },
    {
      title: '',
      key: 'actions',
      render: (_: unknown, a: Account) => (
        <>
          <Button type="link" size="small" onClick={() => openEdit(a)}>
            编辑
          </Button>
          <Popconfirm title={`删除「${a.name}」？`} onConfirm={() => doDelete(a)}>
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
        <h2>账户管理</h2>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建账户
        </Button>
      </div>

      <Card size="small" className="panel">
        {isMobile ? (
          <div className="acct-list">
            {(accounts.data ?? []).map((a) => (
              <div className="acct-row" key={a.id}>
                <div className="acct-main" onClick={() => openEdit(a)}>
                  <div className="acct-name">
                    {a.name}
                    <Tag color={a.type === 'liability' ? 'volcano' : undefined} style={{ marginLeft: 8 }}>
                      {a.type === 'asset' ? '资产' : '负债'}
                    </Tag>
                    {a.status === 'disabled' && <Tag>停用</Tag>}
                  </div>
                  <div className="acct-meta">{a.repayment_day ? `还款日 每月${a.repayment_day}日` : ''}</div>
                </div>
                <div className="acct-right">
                  <div className="acct-balance">{renderBalance(a)}</div>
                  <Popconfirm title={`删除「${a.name}」？`} onConfirm={() => doDelete(a)}>
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

      <Modal
        open={open}
        onCancel={() => setOpen(false)}
        onOk={submit}
        title={editing ? '编辑账户' : '新建账户'}
        confirmLoading={create.isPending || update.isPending}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, max: 64 }]}>
            <Input placeholder="如：现金 / 微信 / 信用卡" />
          </Form.Item>
          <Form.Item name="type" label="类型">
            <Segmented
              options={[
                { value: 'asset', label: '资产' },
                { value: 'liability', label: '负债' },
              ]}
            />
          </Form.Item>
          <Form.Item name="opening_balance" label="期初余额（负债可为负）">
            <Input placeholder="0.00" inputMode="decimal" />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.type !== b.type}>
            {({ getFieldValue }) =>
              getFieldValue('type') === 'liability' ? (
                <Form.Item name="repayment_day" label="还款日（1-28）">
                  <InputNumber min={1} max={28} style={{ width: '100%' }} placeholder="如：10" />
                </Form.Item>
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
    </div>
  );
}
