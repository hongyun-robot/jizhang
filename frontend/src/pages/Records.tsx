import { useEffect, useMemo, useState, type HTMLAttributes } from 'react';
import dayjs from 'dayjs';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  App,
  Button,
  Cascader,
  Empty,
  Grid,
  Input,
  InputNumber,
  Pagination,
  Popconfirm,
  Segmented,
  Select,
  Skeleton,
  Table,
  Tag,
} from 'antd';
import { DownloadOutlined, PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import {
  exportTxns,
  useAccounts,
  useCategoryForest,
  useDeleteTxn,
  useTxnList,
} from '../api/hooks';
import type { CategoryTreeNode, DateRange, TxnKind, Transaction } from '../api/types';
import { useAuth } from '../stores/auth';
import Money from '../components/Money';
import PeriodPicker, { presetRange } from '../components/PeriodPicker';
import TxnFormModal from '../components/TxnFormModal';

const PAGE_SIZE = 20;

interface FilterState {
  range: DateRange;
  kind: TxnKind | '';
  account_id?: number;
  category_id?: number;
  amount_min?: string;
  amount_max?: string;
  keyword: string;
}

/** 扁平化分类树 → id 显示信息（含一级名，用于「餐饮/午餐」展示与 Cascader 选项）。 */
function flattenForest(forest?: CategoryTreeNode[] | null) {
  const info = new Map<number, { name: string; full: string; kind: string; parent?: number }>();
  const groups: { value: number; label: string; children?: { value: number; label: string }[] }[] = [];
  let seq = -1; // 分组节点 value 恒为负，永不与真实分类 id 冲突
  for (const wrapper of forest ?? []) {
    for (const root of wrapper.children ?? []) {
      const kids = (root.children ?? []).filter((c) => c.id);
      if (!kids.length && root.id) {
        // 无子分类的一级直接可选：用真实 id
        groups.push({ value: root.id, label: root.name });
        info.set(root.id, { name: root.name, full: root.name, kind: root.kind });
        continue;
      }
      groups.push({
        value: seq--, // 分组节点 value 恒为负，永不与真实分类 id 冲突
        label: root.kind === 'income' ? `收 · ${root.name}` : root.name,
        children: kids.map((c) => {
          info.set(c.id, {
            name: c.name,
            full: `${root.name}/${c.name}`,
            kind: root.kind,
            parent: root.id,
          });
          return { value: c.id, label: c.name };
        }),
      });
    }
  }
  return { info, options: groups };
}

/** 流水列表（4.5）：共享区间选择器 + 组合筛选 + offset 分页 + 记一笔/编辑/删除。 */
export default function Records() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const user = useAuth((s) => s.user);

  const accounts = useAccounts();
  const forest = useCategoryForest();
  const delTxn = useDeleteTxn();

  const [filters, setFilters] = useState<FilterState>({
    range: presetRange('thisMonth'),
    kind: '',
    keyword: '',
  });
  const [page, setPage] = useState(1);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Transaction | null>(null);

  const { info: catInfo, options: catOptions } = useMemo(
    () => flattenForest(forest.data),
    [forest.data],
  );
  const acctName = useMemo(() => {
    const m = new Map<number, string>();
    for (const a of accounts.data ?? []) m.set(a.id, a.name);
    return m;
  }, [accounts.data]);

  const params = useMemo(
    () => ({
      start_date: filters.range.start,
      end_date: filters.range.end,
      kind: filters.kind || undefined,
      account_id: filters.account_id,
      category_id: filters.category_id,
      amount_min: filters.amount_min,
      amount_max: filters.amount_max,
      keyword: filters.keyword || undefined,
      page,
      page_size: PAGE_SIZE,
    }),
    [filters, page],
  );
  const list = useTxnList(params);

  // 首页「最近流水」点击 → 跳转本页并定位高亮该笔（home-dashboard 场景）
  const location = useLocation();
  const navigate = useNavigate();
  const locateId = (location.state as { locateId?: number } | null)?.locateId;
  useEffect(() => {
    if (!locateId || !list.data || list.isLoading) return;
    const el = document.querySelector(`[data-txn-id="${locateId}"]`);
    if (el) {
      el.scrollIntoView({ block: 'center' });
      el.classList.add('flash');
      const timer = setTimeout(() => el.classList.remove('flash'), 1800);
      navigate('/records', { replace: true, state: null });
      return () => clearTimeout(timer);
    }
    // 目标不在当前区间/页内也清掉状态，避免反复触发
    navigate('/records', { replace: true, state: null });
  }, [locateId, list.data, list.isLoading, navigate]);

  const patch = (p: Partial<FilterState>) => {
    setFilters((f) => ({ ...f, ...p }));
    setPage(1);
  };

  const canEdit = (t: Transaction) => {
    if (!user || user.role === 'viewer') return false;
    if (user.role === 'admin' || user.role === 'recorder') return true;
    return t.created_by === user.id;
  };

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (t: Transaction) => {
    setEditing(t);
    setFormOpen(true);
  };
  const doDelete = async (t: Transaction) => {
    try {
      await delTxn.mutateAsync(t.id);
      message.success('已删除，余额与统计同步更新');
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  // 导出当前筛选结果（specs/transactions CSV 导出；四档角色均可读）
  const [exporting, setExporting] = useState(false);
  const doExport = async () => {
    setExporting(true);
    try {
      await exportTxns(params);
    } catch (e) {
      message.error(errMsg(e));
    } finally {
      setExporting(false);
    }
  };

  const kindTag = (k: TxnKind) =>
    k === 'expense' ? '支出' : k === 'income' ? '收入' : '转账';
  const tone = (k: TxnKind) => (k === 'expense' ? 'expense' : k === 'income' ? 'income' : 'plain');

  const amountOf = (t: Transaction) =>
    t.kind === 'income' ? t.amount : `-${t.amount}`;

  const actions = (t: Transaction) =>
    canEdit(t) ? (
      <>
        <Button type="link" size="small" onClick={() => openEdit(t)}>
          编辑
        </Button>
        <Popconfirm title="删除这笔流水？" onConfirm={() => doDelete(t)}>
          <Button type="link" size="small" danger>
            删除
          </Button>
        </Popconfirm>
      </>
    ) : null;

  const columns = [
    {
      title: '日期',
      dataIndex: 'entry_date',
      key: 'date',
      width: 110,
      render: (v: string) => dayjs(v).format('YYYY-MM-DD'),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 116,
      render: (k: TxnKind, t: Transaction) => (
        <>
          {kindTag(k)}
          {t.source === 'recurring' && <Tag className="src-tag">自动</Tag>}
        </>
      ),
    },
    {
      title: '分类',
      key: 'category',
      width: 140,
      render: (_: unknown, t: Transaction) =>
        t.kind === 'transfer'
          ? '—'
          : (t.category_id ? catInfo.get(t.category_id)?.full : undefined) ?? '—',
    },
    {
      title: '账户',
      key: 'account',
      width: 120,
      render: (_: unknown, t: Transaction) =>
        t.kind === 'transfer'
          ? `${acctName.get(t.account_id) ?? t.account_id} → ${acctName.get(t.transfer_account_id ?? 0) ?? ''}`
          : acctName.get(t.account_id) ?? t.account_id,
    },
    {
      title: '金额',
      dataIndex: 'amount',
      key: 'amount',
      align: 'right' as const,
      width: 120,
      render: (_: string, t: Transaction) => <Money value={amountOf(t)} tone={tone(t.kind)} />,
    },
    { title: '备注', dataIndex: 'note', key: 'note', ellipsis: true },
    { title: '', key: 'actions', width: 130, render: (_: unknown, t: Transaction) => actions(t) },
  ];

  const accountOptions = (accounts.data ?? []).map((a) => ({ value: a.id, label: a.name }));

  return (
    <div className="page">
      <div className="page-head">
        <h2>流水</h2>
        <div className="head-actions">
          <Button
            icon={<DownloadOutlined />}
            loading={exporting}
            onClick={doExport}
            aria-label="导出"
          >
            导出
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            记一笔
          </Button>
        </div>
      </div>

      {/* 组合筛选：区间与统计页共用同一选择器（specs/transactions） */}
      <div className="filter-bar">
        <PeriodPicker value={filters.range} onChange={(range) => patch({ range })} />
        <Segmented
          size="small"
          value={filters.kind}
          options={[
            { value: '', label: '全部' },
            { value: 'expense', label: '支出' },
            { value: 'income', label: '收入' },
            { value: 'transfer', label: '转账' },
          ]}
          onChange={(v) => patch({ kind: v as TxnKind | '' })}
        />
        <Select
          size="small"
          allowClear
          placeholder="账户"
          style={{ minWidth: 110 }}
          options={accountOptions}
          onChange={(v) => patch({ account_id: v })}
        />
        <Cascader
          size="small"
          allowClear
          placeholder="分类"
          style={{ minWidth: 130 }}
          options={catOptions}
          onChange={(v) => patch({ category_id: v.length ? (v[v.length - 1] as number) : undefined })}
        />
        <Input
          size="small"
          allowClear
          placeholder="备注关键字"
          style={{ width: 130 }}
          value={filters.keyword}
          onChange={(e) => patch({ keyword: e.target.value })}
        />
        <span className="amount-range">
          <InputNumber
            size="small"
            placeholder="金额≥"
            style={{ width: 96 }}
            min={0}
            onChange={(v) => patch({ amount_min: v !== null && v !== undefined ? String(v) : undefined })}
          />
          <span className="sep">–</span>
          <InputNumber
            size="small"
            placeholder="金额≤"
            style={{ width: 96 }}
            min={0}
            onChange={(v) => patch({ amount_max: v !== null && v !== undefined ? String(v) : undefined })}
          />
        </span>
      </div>

      {isMobile ? (
        <div className="txn-list">
          {!list.data && !list.isError ? (
            <div className="txn-skeleton">
              <Skeleton active paragraph={{ rows: 5 }} title={false} />
            </div>
          ) : (
            (list.data?.items ?? []).map((t) => (
              <div className="txn-row" key={t.id} data-txn-id={t.id}>
                <div className="txn-main">
                  <div className="txn-title">
                    <span className="txn-cat">
                      {t.kind === 'transfer'
                        ? '转账'
                        : (t.category_id ? catInfo.get(t.category_id)?.full : undefined) ?? '未分类'}
                      {t.source === 'recurring' && <Tag className="src-tag">自动</Tag>}
                    </span>
                    <Money value={amountOf(t)} tone={tone(t.kind)} />
                  </div>
                  <div className="txn-sub">
                    {dayjs(t.entry_date).format('MM-DD')} ·{' '}
                    {t.kind === 'transfer'
                      ? `${acctName.get(t.account_id) ?? ''} → ${acctName.get(t.transfer_account_id ?? 0) ?? ''}`
                      : acctName.get(t.account_id) ?? ''}
                    {t.note ? ` · ${t.note}` : ''}
                  </div>
                </div>
                <div className="txn-actions">{actions(t)}</div>
              </div>
            ))
          )}
          {!list.isLoading && (list.data?.items ?? []).length === 0 && (
            <div className="list-empty">
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无数据" />
            </div>
          )}
        </div>
      ) : (
        <div className="panel">
          <Table
            rowKey="id"
            size="small"
            loading={list.isLoading}
            dataSource={list.data?.items ?? []}
            columns={columns}
            pagination={false}
            scroll={{ x: 860 }}
            onRow={(record) =>
              ({ 'data-txn-id': record.id }) as HTMLAttributes<HTMLTableRowElement>
            }
          />
        </div>
      )}

      <div className="pager">
        <Pagination
          current={page}
          pageSize={PAGE_SIZE}
          total={list.data?.total ?? 0}
          onChange={(p) => setPage(p)}
          showSizeChanger={false}
          showTotal={(t) => `共 ${t} 笔`}
          size="small"
        />
      </div>

      <TxnFormModal open={formOpen} onClose={() => setFormOpen(false)} editing={editing} />
    </div>
  );
}
