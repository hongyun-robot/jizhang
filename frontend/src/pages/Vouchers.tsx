import { useEffect, useRef, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import { useSearchParams } from 'react-router-dom';
import {
  Alert,
  App,
  Button,
  DatePicker,
  Drawer,
  Empty,
  Grid,
  Input,
  InputNumber,
  Modal,
  Pagination,
  Popconfirm,
  Select,
  Skeleton,
  Tag,
} from 'antd';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { errMsg } from '../api/client';
import {
  useAccounts,
  useEnsureVouchers,
  useVoucherDetail,
  useVoucherMutations,
  useVouchers,
} from '../api/hooks';
import type { Voucher, VoucherAction } from '../api/types';
import { canRecord, useAuth } from '../stores/auth';
import Money from '../components/Money';
import VoucherFormModal, {
  voucherInputOf,
  voucherToday,
} from '../components/VoucherFormModal';

const ACTION: Record<VoucherAction, string> = {
  purchase: '购券',
  redeem: '核销',
  refund: '退未用部分',
  writeoff: '作废',
};
function stateLabel(v: Voucher): string {
  if (v.status === 'settled')
    return v.settlement === 'used'
      ? '已全部核销'
      : v.settlement === 'refunded'
        ? '已退款结清'
        : '已作废结清';
  return v.status === 'failed'
    ? '处理失败'
    : v.status === 'expired'
      ? '过期待处理'
      : '待使用';
}
function autoLabel(v: Voucher): string {
  return !v.auto_refund_enabled
    ? '自动退款关闭'
    : v.auto_refund_suspended
      ? '自动退款已暂停'
      : '到期自动退款';
}
function moneyFen(value: string): bigint {
  const [whole, fraction = ''] = value.split('.');
  return BigInt(whole) * 100n + BigInt((fraction + '00').slice(0, 2));
}
function operationCost(v: Voucher, count: number): string {
  if (count === v.remaining_units) return v.remaining_amount;
  const fen =
    moneyFen(v.next_unit_amount) *
    BigInt(Number.isSafeInteger(count) && count > 0 ? count : 0);
  return `${fen / 100n}.${String(fen % 100n).padStart(2, '0')}`;
}

export default function Vouchers() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const user = useAuth((s) => s.user);
  const [params, setParams] = useSearchParams();
  const rawId = Number(params.get('id'));
  const id = Number.isSafeInteger(rawId) && rawId > 0 ? rawId : null;
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const list = useVouchers({ keyword, status, page, page_size: 20 });
  const detail = useVoucherDetail(id);
  const accounts = useAccounts();
  const mutations = useVoucherMutations();
  const ensure = useEnsureVouchers();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Voucher | null>(null);
  const [operation, setOperation] = useState<Exclude<
    VoucherAction,
    'purchase'
  > | null>(null);
  const [date, setDate] = useState<Dayjs>(voucherToday);
  const [units, setUnits] = useState(1);
  const attempt = useRef<string | null>(null);
  const voucher = detail.data;
  const writable = canRecord(user?.role);
  useEffect(() => {
    setOperation(null);
  }, [id]);

  const begin = (action: Exclude<VoucherAction, 'purchase'>) => {
    setOperation(action);
    setUnits(1);
    setDate(voucherToday());
    attempt.current = null;
  };
  const submit = async () => {
    if (!voucher || !operation) return;
    if (
      operation === 'redeem' &&
      (!Number.isSafeInteger(units) ||
        units < 1 ||
        units > voucher.remaining_units)
    ) {
      message.warning('核销次数不能超过剩余次数');
      return;
    }
    const input = {
      entry_date: date.format('YYYY-MM-DD'),
      ...(operation === 'redeem' ? { units } : {}),
    };
    const requestKey =
      attempt.current ?? (attempt.current = crypto.randomUUID());
    try {
      await mutations.operate.mutateAsync({
        id: voucher.id,
        action: operation,
        input: { ...input, request_key: requestKey },
      });
      message.success(`${ACTION[operation]}已记录`);
      setOperation(null);
    } catch (err) {
      message.error(errMsg(err));
    }
  };
  const undo = async (eventId: number) => {
    if (!voucher) return;
    try {
      await mutations.undo.mutateAsync({ id: voucher.id, eventId });
      message.success('已撤销，券和账务同步恢复');
    } catch (err) {
      message.error(errMsg(err));
    }
  };
  const remove = async () => {
    if (!voucher) return;
    try {
      await mutations.remove.mutateAsync(voucher.id);
      setParams({}, { replace: true });
      message.success('券已删除，购买扣款已撤销');
    } catch (err) {
      message.error(errMsg(err));
    }
  };
  const resume = async () => {
    if (!voucher) return;
    try {
      await mutations.update.mutateAsync({
        id: voucher.id,
        input: {
          ...voucherInputOf(voucher),
          auto_refund_enabled: true,
          resume_auto_refund: true,
        },
      });
      message.success('已重新启用自动退款');
    } catch (err) {
      message.error(errMsg(err));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">先购买 · 使用时计消费</div>
          <h2>我的券</h2>
        </div>
        <div className="head-actions">
          {writable && (
            <Button
              aria-label="检查到期券"
              icon={<ReloadOutlined />}
              loading={ensure.isPending}
              onClick={() =>
                ensure.mutate(undefined, {
                  onSuccess: (r) =>
                    message.success(
                      r.failed
                        ? `已处理 ${r.generated} 张，${r.failed} 张待处理`
                        : `已处理 ${r.generated} 张到期券`,
                    ),
                  onError: (e) => message.error(errMsg(e)),
                })
              }
            >
              检查到期券
            </Button>
          )}
          {writable && (
            <Button
              aria-label="登记购券"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(null);
                setFormOpen(true);
              }}
            >
              登记购券
            </Button>
          )}
        </div>
      </div>
      <div className="voucher-summary panel">
        <span>未用券金额</span>
        <Money value={list.data?.unused_amount ?? '0'} />
        <span className="muted">按剩余实付成本计算，含过期待处理券</span>
      </div>
      <div className="filter-bar">
        <Input.Search
          placeholder="券名称 / 平台"
          allowClear
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value);
            setPage(1);
          }}
          style={{ maxWidth: 260 }}
        />
        <Select
          value={status}
          onChange={(v) => {
            setStatus(v);
            setPage(1);
          }}
          style={{ minWidth: 150 }}
          options={[
            { value: '', label: '全部状态' },
            { value: 'pending', label: '待使用' },
            { value: 'expired', label: '过期待处理' },
            { value: 'failed', label: '处理失败' },
            { value: 'settled', label: '已结清' },
          ]}
        />
      </div>
      {list.isError && (
        <Alert
          type="error"
          showIcon
          title={errMsg(list.error)}
          action={<Button onClick={() => void list.refetch()}>重试</Button>}
        />
      )}
      <div className="voucher-list panel">
        {list.isLoading ? (
          <Skeleton active />
        ) : (list.data?.items ?? []).length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无券" />
        ) : (
          list.data!.items.map((v) => (
            <button
              className="voucher-row"
              key={v.id}
              onClick={() => setParams({ id: String(v.id) })}
            >
              <div className="voucher-row-main">
                <div className="voucher-row-title">
                  <strong>{v.name}</strong>
                  <Tag
                    color={
                      v.status === 'failed'
                        ? 'error'
                        : v.status === 'expired'
                          ? 'warning'
                          : undefined
                    }
                  >
                    {stateLabel(v)}
                  </Tag>
                </div>
                <div className="muted">
                  {v.platform || '未填写平台'} · 已用 {v.used_units}/
                  {v.total_units} 次 · 购买 {v.purchase_date.slice(0, 10)}
                </div>
                <div className="voucher-expiry">
                  {v.expires_on
                    ? `有效期至 ${v.expires_on.slice(0, 10)} · ${autoLabel(v)}`
                    : '未设置有效期'}
                </div>
                {v.last_error && v.status === 'failed' && (
                  <div className="voucher-error">{v.last_error}</div>
                )}
              </div>
              <div className="voucher-row-amount">
                <span className="muted">剩余 {v.remaining_units} 次</span>
                <Money value={v.remaining_amount} />
                <span className="muted">实付 ¥{v.paid_amount}</span>
              </div>
            </button>
          ))
        )}
      </div>
      <div className="pager">
        <Pagination
          current={page}
          total={list.data?.total ?? 0}
          pageSize={20}
          showSizeChanger={false}
          onChange={setPage}
          showTotal={(n) => `共 ${n} 张`}
        />
      </div>

      <Drawer
        open={id !== null}
        onClose={() => setParams({}, { replace: true })}
        title={voucher?.name ?? '券详情'}
        size={screens.md ? 560 : '100%'}
      >
        {detail.isLoading && <Skeleton active />}
        {detail.isError && (
          <Alert type="error" showIcon title={errMsg(detail.error)} />
        )}
        {voucher && (
          <div className="voucher-detail">
            <div className="voucher-detail-head">
              <Tag>{stateLabel(voucher)}</Tag>
              <span>{voucher.platform || '未填写平台'}</span>
            </div>
            <div className="voucher-metrics">
              <div>
                <span>未用金额 · {voucher.remaining_units} 次</span>
                <Money value={voucher.remaining_amount} />
              </div>
              <div>
                <span>实付 · {voucher.total_units} 次</span>
                <Money value={voucher.paid_amount} />
              </div>
              <div>
                <span>已核销 · {voucher.used_units} 次</span>
                <Money value={voucher.used_amount} />
              </div>
              <div>
                <span>已退回 / 已作废</span>
                <span>
                  <Money value={voucher.refunded_amount} /> /{' '}
                  <Money value={voucher.written_off_amount} />
                </span>
              </div>
            </div>
            <div className="voucher-facts">
              <div>购买日：{voucher.purchase_date.slice(0, 10)}</div>
              <div>
                支付账户：
                {accounts.data?.find((a) => a.id === voucher.account_id)
                  ?.name ?? voucher.account_id}
              </div>
              <div>有效期：{voucher.expires_on?.slice(0, 10) ?? '未设置'}</div>
              <div>{autoLabel(voucher)}</div>
              {voucher.note && <div>备注：{voucher.note}</div>}
            </div>
            {voucher.last_error && voucher.status === 'failed' && (
              <Alert
                type="warning"
                showIcon
                title="自动退款待处理"
                description={voucher.last_error}
              />
            )}
            {voucher.auto_refund_suspended && (
              <Alert
                type="info"
                showIcon
                title="自动退款已暂停"
                description="撤销后不会立即再次退款。核实后可手动处理或重新启用。"
              />
            )}
            <div className="voucher-actions">
              {voucher.can_operate && (
                <>
                  <Button type="primary" onClick={() => begin('redeem')}>
                    核销
                  </Button>
                  <Button onClick={() => begin('refund')}>退未用部分</Button>
                  <Button onClick={() => begin('writeoff')}>作废</Button>
                </>
              )}
              {voucher.can_edit && (
                <Button
                  onClick={() => {
                    setEditing(voucher);
                    setFormOpen(true);
                  }}
                >
                  编辑券
                </Button>
              )}
              {voucher.can_edit &&
                voucher.auto_refund_enabled &&
                voucher.auto_refund_suspended &&
                voucher.remaining_units > 0 && (
                  <Popconfirm
                    title="重新启用到期自动退款？"
                    description="已过期的券会按有效期补记退款。"
                    onConfirm={resume}
                  >
                    <Button loading={mutations.update.isPending}>
                      重新启用自动退款
                    </Button>
                  </Popconfirm>
                )}
              {voucher.can_delete && (
                <Popconfirm title="删除券并撤销购买扣款？" onConfirm={remove}>
                  <Button danger loading={mutations.remove.isPending}>
                    删除券
                  </Button>
                </Popconfirm>
              )}
            </div>
            <h3>操作记录</h3>
            <div className="voucher-events">
              {[...voucher.events].reverse().map((event) => (
                <div
                  className={`voucher-event${event.voided_at ? ' is-voided' : ''}`}
                  key={event.id}
                >
                  <div>
                    <strong>{ACTION[event.action]}</strong> · {event.units} 次{' '}
                    {event.source === 'auto' && <Tag>到期自动登记</Tag>}
                    <div className="muted">
                      {event.entry_date.slice(0, 10)}
                      {event.voided_at
                        ? ` · 已撤销 ${dayjs(event.voided_at).format('MM-DD HH:mm')}`
                        : ''}
                    </div>
                  </div>
                  <div className="voucher-event-end">
                    <Money value={event.amount} />
                    {event.can_undo && (
                      <Popconfirm
                        title={`撤销这次${ACTION[event.action]}？`}
                        description={
                          event.source === 'auto'
                            ? '余额将回退，自动退款会暂停。'
                            : '恢复对应份额，并还原消费或余额。'
                        }
                        onConfirm={() => undo(event.id)}
                      >
                        <Button
                          type="link"
                          size="small"
                          loading={mutations.undo.isPending}
                        >
                          撤销
                        </Button>
                      </Popconfirm>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </Drawer>

      <Modal
        open={operation !== null}
        onCancel={() => setOperation(null)}
        title={operation ? ACTION[operation] : ''}
        okText="确认记录"
        cancelText="取消"
        onOk={submit}
        confirmLoading={mutations.operate.isPending}
      >
        {voucher && operation && (
          <div className="voucher-operation">
            <p>
              {voucher.name} · 剩余 {voucher.remaining_units} 次
            </p>
            <label>
              归属日期
              <DatePicker
                value={date}
                allowClear={false}
                onChange={(d) => d && setDate(d)}
                style={{ width: '100%' }}
                disabledDate={(d) =>
                  d.format('YYYY-MM-DD') < voucher.purchase_date.slice(0, 10) ||
                  (operation === 'redeem' &&
                    !!voucher.expires_on &&
                    d.format('YYYY-MM-DD') > voucher.expires_on.slice(0, 10))
                }
              />
            </label>
            {operation === 'redeem' && (
              <label>
                核销次数
                <InputNumber
                  min={1}
                  max={voucher.remaining_units}
                  precision={0}
                  value={units}
                  onChange={(n) => n !== null && setUnits(Number(n))}
                  style={{ width: '100%' }}
                />
              </label>
            )}
            <div>
              本次{operation === 'refund' ? '退款' : '消费'}金额：
              <Money
                value={
                  operation === 'redeem'
                    ? operationCost(voucher, units)
                    : voucher.remaining_amount
                }
              />
            </div>
            <p className="muted">
              {operation === 'refund'
                ? '退回全部未用部分，恢复原支付账户余额；已核销的消费保留。'
                : operation === 'writeoff'
                  ? '将全部剩余部分作废，剩余实付成本计入作废日支出和预算。'
                  : '按实付成本分摊计入核销日消费，不再次扣余额。'}
            </p>
          </div>
        )}
      </Modal>
      <VoucherFormModal
        open={formOpen}
        onClose={() => setFormOpen(false)}
        editing={editing}
      />
    </div>
  );
}
