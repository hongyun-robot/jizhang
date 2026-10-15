import { useState } from 'react';
import { App, Button, Card, Grid, Popconfirm, Table, Tag } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useAccounts, useAccountMutations, useReorderAccounts } from '../api/accounts';
import { useLiabilityTypes } from '../api/liability';
import type { Account, AccountType } from '../api/types';
import { errMsg } from '../api/client';
import { cents, money } from '../lib/money';
import Money from '../components/Money';
import AccountEditor from '../components/AccountEditor';
import { QueryBoundary } from '../components/QueryState';
import { useCatalogOrder } from '../hooks/useCatalogOrder';
import { OrderStatus, SortableCollection, SortableHandle, SortableItem, SortableTableRow } from '../components/sorting/SortableCollection';

export default function Accounts() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const isMobile = !Grid.useBreakpoint().md;
  const accounts = useAccounts();
  const { remove } = useAccountMutations();
  const reorder = useReorderAccounts();
  const order = useCatalogOrder(accounts.data ?? [], 'accounts', reorder.mutateAsync, accounts.refetch);
  const blocked = order.busy || reorder.directoryBusy || remove.isPending;
  const types = useLiabilityTypes();
  const labels = Object.fromEntries((types.data ?? []).map((t) => [t.code, t.name]));
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Account | null>(null);
  const openCreate = () => { if (!blocked) { setEditing(null); setOpen(true); } };
  const openEdit = (account: Account) => { if (!blocked) { setEditing(account); setOpen(true); } };
  const doDelete = async (a: Account) => {
    if (blocked) return;
    try {
      await remove.mutateAsync(a.id);
      message.success(`已删除「${a.name}」`);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  /** 负债欠款按红字“欠款”呈现；资产为负同样红字。 */
  const renderBalance = (a: Account) => {
    if ((a.asset_class === 'fund' || a.asset_class === 'gold') && a.valuation_status === 'no_price') return <span className="muted">暂无法估值</span>;
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
    { title: '', key: 'sort', width: 44, render: (_: unknown, a: Account) => <SortableHandle name={a.name} /> },
    {
      title: '账户',
      dataIndex: 'name',
      key: 'name',
      width: '38%',
      render: (name: string, a: Account) =>
        a.asset_class === 'gold' ? <Button type="link" onClick={() => navigate(`/gold/${a.id}`)}>{name}</Button> : a.asset_class === 'fund' ? <Button type="link" onClick={() => navigate(`/funds/${a.id}`)}>{name}</Button> : a.type === 'liability' ? (
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
          <Button type="link" size="small" disabled={blocked} onClick={() => openEdit(a)}>
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
            <Button type="link" size="small" danger disabled={blocked}>
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
        <Button type="primary" icon={<PlusOutlined />} disabled={blocked} onClick={openCreate}>
          新建账户
        </Button>
      </div>

      <QueryBoundary query={accounts}>
        <Card size="small" className="panel panel--measure">
          <OrderStatus {...order} />
          <SortableCollection ids={order.ids} disabled={blocked || open} onOrder={order.commit}>
          {isMobile ? (
            <div className="acct-list">
              {order.rows.map((a) => (
                <SortableItem id={a.id} className="acct-row" key={a.id}>
                  <SortableHandle name={a.name} />
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
                      {a.asset_class === 'gold' && <Button type="link" size="small" onClick={(e) => { e.stopPropagation(); navigate(`/gold/${a.id}`); }}>黄金持仓与盈亏</Button>}
                      {a.asset_class === 'fund' && <Button type="link" size="small" onClick={(e) => { e.stopPropagation(); navigate(`/funds/${a.id}`); }}>持仓与盈亏</Button>}
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
                      <Button type="link" size="small" danger disabled={blocked}>
                        删除
                      </Button>
                    </Popconfirm>
                  </div>
                </SortableItem>
              ))}
            </div>
          ) : (
            <Table
              rowKey="id"
              size="small"
              loading={accounts.isLoading}
              dataSource={order.rows}
              components={{ body: { row: SortableTableRow } }}
              columns={columns}
              pagination={false}
            />
          )}
          </SortableCollection>
        </Card>
      </QueryBoundary>

      {open && <AccountEditor allowFunds key={editing?.id ?? 'new'} account={editing ?? undefined} onClose={() => setOpen(false)} />}
    </div>
  );
}
