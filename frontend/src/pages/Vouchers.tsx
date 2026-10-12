import ResponsiveSelect from '../components/ResponsiveSelect';
import VoucherList from '../features/vouchers/VoucherList';
import VoucherDetail from '../features/vouchers/VoucherDetail';
import VoucherOperation from '../features/vouchers/VoucherOperation';
import { ACTION } from '../features/vouchers/display';

import QueryState, { QueryBoundary } from '../components/QueryState';
import { useEffect, useRef, useState } from 'react';
import { type Dayjs } from 'dayjs';
import { useSearchParams } from 'react-router-dom';
import { App, Button, Drawer, Grid, Input, Pagination } from 'antd';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { errMsg } from '../api/client';
import { useAccounts } from '../api/accounts';
import {
  useEnsureVouchers,
  useVoucherDetail,
  useVoucherMutations,
  useVouchers,
} from '../api/vouchers';
import type { Voucher, VoucherAction } from '../api/types';
import { canRecord, useAuth } from '../stores/auth';
import Money from '../components/Money';
import VoucherFormModal, {
  voucherInputOf,
  voucherToday,
} from '../components/VoucherFormModal';

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
                        : `已处理 ${r.generated} 张到期券`
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
        {list.data ? (
          <Money value={list.data.unused_amount} />
        ) : (
          <span className="muted">—</span>
        )}
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
        <ResponsiveSelect
          sheetTitle="券状态"
          mobileSearch={false}
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

      <QueryBoundary query={list}>
        <VoucherList
          list={list}
          onSelect={(id) => setParams({ id: String(id) })}
        />
      </QueryBoundary>
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
        <QueryState
          loading={detail.isLoading}
          error={detail.error}
          hasData={detail.data !== undefined}
          onRetry={() => void detail.refetch()}
        />

        {voucher && (
          <VoucherDetail
            voucher={voucher}
            accounts={accounts}
            pending={{
              update: mutations.update.isPending,
              remove: mutations.remove.isPending,
              undo: mutations.undo.isPending,
            }}
            onEdit={(value) => {
              setEditing(value);
              setFormOpen(true);
            }}
            begin={begin}
            undo={undo}
            remove={remove}
            resume={resume}
          />
        )}
      </Drawer>

      <VoucherOperation
        voucher={voucher}
        operation={operation}
        date={date}
        units={units}
        setDate={setDate}
        setUnits={setUnits}
        setOperation={setOperation}
        pending={mutations.operate.isPending}
        submit={submit}
      />
      <VoucherFormModal
        open={formOpen}
        onClose={() => setFormOpen(false)}
        editing={editing}
      />
    </div>
  );
}
