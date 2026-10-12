import SelectionScope from '../components/SelectionScope';
import RecordActions from '../features/records/RecordActions';
import { RecordRows, recordColumns } from '../features/records/RecordViews';
import FilterControls from '../features/records/FilterControls';
import { useRecordsState } from '../features/records/useRecordsState';
import { useRecordLocator } from '../features/records/useRecordLocator';
import {
  useListGestures,
  PTR_H,
  PTR_TRIGGER,
} from '../features/records/useListGestures';

import { summarizeFilters } from '../features/records/filters';
import { categoryIndex as flattenForest } from '../domain/categories';
import { useMemo, useState, type HTMLAttributes } from 'react';

import {
  App,
  Badge,
  Button,
  Drawer,
  Empty,
  Grid,
  Pagination,
  Skeleton,
  Table,
} from 'antd';
import {
  DownloadOutlined,
  FilterOutlined,
  PlusOutlined,
} from '@ant-design/icons';

import { errMsg } from '../api/client';
import { exportTxns, useDeleteTxn } from '../api/transactions';
import { useAccounts } from '../api/accounts';
import { useCategoryForest } from '../api/categories';
import type { Transaction } from '../api/types';
import { useAuth } from '../stores/auth';
import { canRecord } from '../domain/permissions';

import QueryState from '../components/QueryState';
import TxnFormModal, { type TxnRefill } from '../components/TxnFormModal';

const PAGE_SIZE = 20;

/** 流水列表（4.5）：共享区间选择器 + 组合筛选 + offset 分页 + 记一笔/编辑/删除。 */
export default function Records() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const user = useAuth((s) => s.user);

  const accounts = useAccounts();
  const forest = useCategoryForest();
  const delTxn = useDeleteTxn();

  // 筛选条件与页码的单一事实来源是 URL searchParams（design D1）：
  // 切 Tab、前进后退、分享链接均可恢复；router state 只存一次性定位（locateId）。
  const {
    filters,
    defaultRange,
    page,
    list,
    items,
    total,
    exportParams,
    patch,
    gotoPage,
    loadNext,
    clearFilters,
  } = useRecordsState();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Transaction | null>(null);
  const [refill, setRefill] = useState<TxnRefill | null>(null);

  const { info: catInfo, options: catOptions } = useMemo(
    () => flattenForest(forest.data),
    [forest.data]
  );
  const accountTypes = useMemo(
    () =>
      new Map(
        (accounts.data ?? []).map((account) => [account.id, account.type])
      ),
    [accounts.data]
  );
  const acctName = useMemo(() => {
    const m = new Map<number, string>();
    for (const a of accounts.data ?? []) m.set(a.id, a.name);
    return m;
  }, [accounts.data]);

  useRecordLocator(!!list.data && !list.isLoading, screens.md !== undefined);
  // 移动端筛选收纳（specs/transactions 筛选栏收纳）：底部抽屉 + 单行条件摘要
  const [filterOpen, setFilterOpen] = useState(false);

  /** 生效条件摘要（区间恒显；其余仅显非默认项）与条件数（角标）。 */
  const { summary: filterSummary, count: activeCondCount } = useMemo(
    () => summarizeFilters(filters, defaultRange, acctName, catInfo),
    [filters, defaultRange, acctName, catInfo]
  );

  const {
    sentinelRef,
    pull,
    onTouchStart,
    onTouchMove,
    onTouchEnd,
    onTouchCancel,
  } = useListGestures(isMobile, list, loadNext);

  const openCreate = () => {
    setEditing(null);
    setRefill(null);
    setFormOpen(true);
  };
  const openEdit = (t: Transaction) => {
    setEditing(t);
    setRefill(null);
    setFormOpen(true);
  };
  /** 行内「退款」入口：预填关联原支出及其分类/账户（specs/refunds 两个入口之一）。 */
  const openRefund = (t: Transaction) => {
    setEditing(null);
    setRefill({
      kind: 'refund',
      related_txn_id: t.id,
      account_id: t.account_id,
      category_id: t.category_id ?? undefined,
      related_amount: t.amount,
      refunded: t.refunded_amount ?? '0',
      platform: t.platform,
      installment_plan_id: t.installment_plan_id ?? undefined,
      liability_account_id: t.liability_account_id ?? undefined,
    });
    setFormOpen(true);
  };
  /** 点击「已退 ¥x」→ 定位该支出的关联退款（specs/refunds 可跳转）。 */
  const locateRefunds = (t: Transaction) => {
    patch({
      related_txn_id: t.id,
      kind: '',
      account_id: undefined,
      category_id: undefined,
    });
  };
  const doDelete = async (t: Transaction) => {
    if (delTxn.isPending) return; // 防重：确认进行中忽略重复触发（specs/transactions 删除二次确认防重）
    try {
      await delTxn.mutateAsync(t.id);
      message.success('已删除，余额与统计同步更新');
    } catch (e) {
      message.error(errMsg(e)); // 失败：列表保持原状
    }
  };

  // 导出当前筛选结果（specs/transactions CSV 导出；四档角色均可读）
  const [exporting, setExporting] = useState(false);
  const doExport = async () => {
    setExporting(true);
    try {
      await exportTxns(exportParams);
    } catch (e) {
      message.error(errMsg(e));
    } finally {
      setExporting(false);
    }
  };

  const actions = (transaction: Transaction) => (
    <RecordActions
      transaction={transaction}
      user={user}
      accountTypes={accountTypes}
      pending={delTxn.isPending}
      onEdit={openEdit}
      onRefund={openRefund}
      onDelete={doDelete}
    />
  );

  const columns = recordColumns({ acctName, catInfo, actions, locateRefunds });

  const accountOptions = (accounts.data ?? []).map((a) => ({
    value: a.id,
    label: a.name,
  }));

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            {list.data
              ? `${filters.range.start.replace(/-/g, '.')} – ${filters.range.end.replace(/-/g, '.')} · ${total.toLocaleString('zh-CN')} 笔`
              : '载入中'}
          </div>
          <h2>流水</h2>
        </div>
        <div className="head-actions">
          <Button
            icon={<DownloadOutlined />}
            loading={exporting}
            onClick={doExport}
            aria-label="导出"
          >
            导出
          </Button>
          {canRecord(user?.role) && (
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              记一笔
            </Button>
          )}
        </div>
      </div>

      {/* 组合筛选：桌面渲染既有 filter-bar；移动端收纳为「筛选入口 + 条件摘要」单行 + 底部抽屉 */}
      {list.error && list.data && (
        <QueryState
          error={list.error}
          hasData
          onRetry={() => void list.refetch()}
        />
      )}
      {isMobile ? (
        <div className="filter-entry">
          <Badge count={activeCondCount} size="small">
            <Button
              size="small"
              icon={<FilterOutlined />}
              onClick={() => setFilterOpen(true)}
            >
              筛选
            </Button>
          </Badge>
          <span className="filter-entry-summary">{filterSummary}</span>
        </div>
      ) : (
        <div className="filter-bar">
          <FilterControls
            filters={filters}
            patch={patch}
            accountOptions={accountOptions}
            catOptions={catOptions}
          />
        </div>
      )}
      <SelectionScope active={isMobile && filterOpen}>
        <Drawer
          title="筛选"
          placement="bottom"
          size="60vh"
          open={isMobile && filterOpen}
          onClose={() => setFilterOpen(false)}
        >
          <div className="filter-drawer-controls">
            <FilterControls
              filters={filters}
              patch={patch}
              accountOptions={accountOptions}
              catOptions={catOptions}
              onClear={clearFilters}
              active={isMobile && filterOpen}
            />
          </div>
        </Drawer>
      </SelectionScope>

      {isMobile ? (
        <div
          className="txn-list"
          onTouchStart={onTouchStart}
          onTouchMove={onTouchMove}
          onTouchEnd={onTouchEnd}
          onTouchCancel={onTouchCancel}
        >
          {/* 下拉刷新指示器：只动 transform，状态由 pull/isRefetching 驱动 */}
          <div
            className="ptr-indicator"
            style={{
              transform: `translateY(${list.isRefetching ? 0 : Math.min(pull, PTR_H) - PTR_H}px)`,
            }}
          >
            {list.isRefetching
              ? '刷新中…'
              : pull >= PTR_TRIGGER
                ? '松开刷新'
                : '下拉刷新'}
          </div>
          {/* 错误态单列：请求失败不得渲染成「暂无数据」空态（specs/ui-shell 数据加载错误态） */}
          {list.isError && !list.data ? (
            <QueryState
              error={list.error}
              onRetry={() => void list.refetch()}
            />
          ) : !list.data && !list.isError ? (
            <div className="txn-skeleton">
              <Skeleton active paragraph={{ rows: 5 }} title={false} />
            </div>
          ) : (
            <RecordRows
              items={items}
              acctName={acctName}
              catInfo={catInfo}
              actions={actions}
              locateRefunds={locateRefunds}
            />
          )}
          {!list.isLoading && !list.isError && items.length === 0 && (
            <div className="list-empty">
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无数据"
              />
            </div>
          )}
          {/* 上拉加载：进行中指示 + 底部哨兵（IntersectionObserver 触发 fetchNextPage） */}
          {list.isFetchingNextPage && (
            <div className="load-more" role="status">
              加载中…
            </div>
          )}
          <div
            className="load-sentinel"
            ref={sentinelRef}
            style={{ display: list.hasNextPage ? undefined : 'none' }}
          />
        </div>
      ) : (
        <div className="panel">
          {list.isError && !list.data ? (
            <QueryState
              error={list.error}
              onRetry={() => void list.refetch()}
            />
          ) : (
            <Table
              rowKey="id"
              size="small"
              loading={list.isLoading}
              dataSource={items}
              columns={columns}
              pagination={false}
              scroll={{ x: 990 }}
              onRow={(record) =>
                ({
                  'data-txn-id': record.id,
                }) as HTMLAttributes<HTMLTableRowElement>
              }
            />
          )}
        </div>
      )}

      <div className="pager">
        <Pagination
          current={page}
          pageSize={PAGE_SIZE}
          total={total}
          onChange={gotoPage}
          showSizeChanger={false}
          showTotal={(t) => `共 ${t} 笔`}
          size="small"
        />
      </div>

      <TxnFormModal
        open={formOpen}
        onClose={() => setFormOpen(false)}
        editing={editing}
        refill={refill}
      />
    </div>
  );
}
