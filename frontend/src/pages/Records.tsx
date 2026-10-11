import { useEffect, useMemo, useRef, useState, type HTMLAttributes } from 'react';
import dayjs from 'dayjs';
import { useLocation, useNavigate, useNavigationType, useSearchParams } from 'react-router-dom';
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
  useTxnListInfinite,
} from '../api/hooks';
import type { CategoryTreeNode, DateRange, TxnKind, Transaction, TxnListParams } from '../api/types';
import { useAuth } from '../stores/auth';
import Money from '../components/Money';
import PeriodPicker, { presetRange } from '../components/PeriodPicker';
import QueryState from '../components/QueryState';
import TxnFormModal, { type TxnRefill } from '../components/TxnFormModal';
import { VOUCHER_PURPOSE_LABEL } from '../labels';

const PAGE_SIZE = 20;
/** 下拉刷新指示器高度与触发阈值（高度与 styles.css 的 .ptr-indicator 一致）。 */
const PTR_H = 32;
const PTR_TRIGGER = 56;

interface FilterState {
  range: DateRange;
  kind: TxnKind | '';
  account_id?: number;
  category_id?: number;
  amount_min?: string;
  amount_max?: string;
  keyword: string;
  /** 定位关联到某支出的退款（>0 时忽略日期区间，跨期也可跳转） */
  related_txn_id?: number;
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

const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;
const KINDS: TxnKind[] = ['expense', 'income', 'transfer', 'refund'];

/** 筛选状态 → URL 查询串（design D1）：区间用 from/to，其余键与 params 同名；默认值不写入 URL。 */
function serializeFilters(f: FilterState, page: number, def: DateRange): URLSearchParams {
  const sp = new URLSearchParams();
  if (f.range.start !== def.start || f.range.end !== def.end) {
    sp.set('from', f.range.start);
    sp.set('to', f.range.end);
  }
  if (f.kind) sp.set('kind', f.kind);
  if (f.account_id) sp.set('account_id', String(f.account_id));
  if (f.category_id) sp.set('category_id', String(f.category_id));
  if (f.keyword) sp.set('keyword', f.keyword);
  if (f.amount_min) sp.set('amount_min', f.amount_min);
  if (f.amount_max) sp.set('amount_max', f.amount_max);
  if (f.related_txn_id) sp.set('related_txn_id', String(f.related_txn_id));
  if (page > 1) sp.set('page', String(page));
  return sp;
}

/** URL 查询串 → 筛选状态；缺省/非法值回落默认区间与空条件。 */
function parseFilters(sp: URLSearchParams, def: DateRange): FilterState {
  const from = sp.get('from');
  const to = sp.get('to');
  const posInt = (v: string | null) => {
    const n = Number(v);
    return v && Number.isSafeInteger(n) && n > 0 ? n : undefined;
  };
  const kind = sp.get('kind') as TxnKind | null;
  return {
    range: from && to && ISO_DATE.test(from) && ISO_DATE.test(to) ? { start: from, end: to } : def,
    kind: kind && KINDS.includes(kind) ? kind : '',
    account_id: posInt(sp.get('account_id')),
    category_id: posInt(sp.get('category_id')),
    amount_min: sp.get('amount_min') || undefined,
    amount_max: sp.get('amount_max') || undefined,
    keyword: sp.get('keyword') ?? '',
    related_txn_id: posInt(sp.get('related_txn_id')),
  };
}

/** 页码同样序列化进 URL；1 页为默认不写入。 */
function parsePage(sp: URLSearchParams): number {
  const n = Number(sp.get('page'));
  return Number.isSafeInteger(n) && n > 1 ? n : 1;
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

  // 筛选条件与页码的单一事实来源是 URL searchParams（design D1）：
  // 切 Tab、前进后退、分享链接均可恢复；router state 只存一次性定位（locateId）。
  const [searchParams, setSearchParams] = useSearchParams();
  const defaultRange = useMemo(() => presetRange('thisMonth'), []);
  const filters = useMemo(
    () => parseFilters(searchParams, defaultRange),
    [searchParams, defaultRange],
  );
  const page = useMemo(() => parsePage(searchParams), [searchParams]);
  // 无限查询的起始页：进页/分享链接以 URL 页码起步，分页跳页时切换（进查询 key，见 useTxnListInfinite）
  const [startPage, setStartPage] = useState(page);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Transaction | null>(null);
  const [refill, setRefill] = useState<TxnRefill | null>(null);

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
      // 定位关联退款时不限日期（退款可落在原支出之后的账期）
      start_date: filters.related_txn_id ? undefined : filters.range.start,
      end_date: filters.related_txn_id ? undefined : filters.range.end,
      kind: filters.kind || undefined,
      account_id: filters.account_id,
      category_id: filters.category_id,
      amount_min: filters.amount_min,
      amount_max: filters.amount_max,
      keyword: filters.keyword || undefined,
      related_txn_id: filters.related_txn_id,
      page,
      page_size: PAGE_SIZE,
    }),
    [filters, page],
  );
  const list = useTxnListInfinite(params, startPage);
  // 扁平展示：已加载页按序展开；total 取末页（服务端每页都带 total）
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const total = useMemo(() => {
    const pages = list.data?.pages;
    if (!pages?.length) return 0;
    return pages[pages.length - 1].total;
  }, [list.data]);
  // 导出走扁平筛选（不含 page/page_size）：URL 页码只服务列表，不改变 CSV 导出语义
  const exportParams = useMemo<TxnListParams>(() => {
    const rest: TxnListParams = { ...params };
    delete rest.page;
    delete rest.page_size;
    return rest;
  }, [params]);

  // 浏览器后退/前进（POP）把起始页同步回 URL 页码；滚动加载的 REPLACE 不触发，避免打断追加缓存
  const navigationType = useNavigationType();
  useEffect(() => {
    if (navigationType !== 'POP') return;
    setStartPage((sp) => (sp === page ? sp : page));
  }, [navigationType, page]);

  // 首页「最近流水」点击 → 跳转本页并定位高亮该笔（home-dashboard 场景）
  const location = useLocation();
  const navigate = useNavigate();
  const locateId = (location.state as { locateId?: number } | null)?.locateId;
  useEffect(() => {
    if (!locateId || !list.data || list.isLoading) return;
    // 断点未定时首帧按移动形态渲染（isMobile=!screens.md），该列表会随形态切换被卸载；
    // 等形态稳定再定位，避免闪烁加在即将卸载的节点上（查询缓存命中时最容易踩中）
    if (screens.md === undefined) return;
    const el = document.querySelector(`[data-txn-id="${locateId}"]`);
    if (el) {
      el.scrollIntoView({ block: 'center' });
      el.classList.add('flash');
      const timer = setTimeout(() => el.classList.remove('flash'), 1800);
      // 清一次性定位 state 时必须带上当前 search，否则会把 URL 里的筛选条件一并清掉（design D1 高风险点）
      navigate({ pathname: '/records', search: location.search }, { replace: true, state: null });
      return () => clearTimeout(timer);
    }
    // 目标不在当前区间/页内也清掉状态，避免反复触发
    navigate({ pathname: '/records', search: location.search }, { replace: true, state: null });
  }, [locateId, list.data, list.isLoading, navigate, location.search, screens.md]);

  const patch = (p: Partial<FilterState>) => {
    // 条件变化回到第 1 页（page=1 默认不写入 URL）；push 历史让「改筛选 → 后退」可恢复
    setSearchParams(serializeFilters({ ...filters, ...p }, 1, defaultRange), { replace: false });
    setStartPage(1); // 新条件从首页起步，丢弃旧条件的追加缓存
  };

  /** 分页控件切页（兜底路径）：换起始页（key 命中直取/未命中回填）、push 历史、加载后回顶。 */
  const gotoPage = (p: number) => {
    setStartPage(p);
    setSearchParams(serializeFilters(filters, p, defaultRange), { replace: false });
    window.scrollTo({ top: 0, behavior: 'instant' as ScrollBehavior });
  };

  /** 上拉加载：追加成功后把 URL 页码同步到已加载到的页（replace，滚动不产生历史条目）。 */
  const loadNext = async () => {
    const res = await list.fetchNextPage();
    const pages = res.data?.pages;
    if (!res.isSuccess || !pages?.length) return;
    let idx = pages.length - 1;
    while (idx > 0 && !pages[idx].items.length) idx--; // 末页可能为空（总数恰为整页时最后一页取不到）
    const loaded = startPage + idx;
    if (loaded !== page) {
      setSearchParams(serializeFilters(filters, loaded, defaultRange), { replace: true });
    }
  };

  // 上拉加载哨兵：观察器只随移动形态创建一次，回调经 ref 读取最新状态，
  // 避免「追加中 isFetchingNextPage 变化 → 重建观察器 → 立刻又触发」的重复加载
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const fetchingNextRef = useRef(false);
  fetchingNextRef.current = list.isFetchingNextPage;
  const hasNextRef = useRef(false);
  hasNextRef.current = list.hasNextPage;
  const loadNextRef = useRef<() => void>(() => {});
  loadNextRef.current = () => void loadNext();

  useEffect(() => {
    const el = sentinelRef.current;
    if (!isMobile || !el) return;
    const ob = new IntersectionObserver(
      (entries) => {
        const e = entries[entries.length - 1];
        if (!e?.isIntersecting) return;
        // 未滚动过（如跳页回顶后）不自动续载，避免跳页后立刻又加载相邻页的跳页感（design D2）
        if (window.scrollY <= 0) return;
        if (!hasNextRef.current || fetchingNextRef.current) return;
        loadNextRef.current();
      },
      { rootMargin: '240px 0px' },
    );
    ob.observe(el);
    return () => ob.disconnect();
  }, [isMobile]);

  // 下拉刷新：仅列表顶部（scrollY===0）下拉生效；指示器只动 transform，
  // prefers-reduced-motion 下无过渡（样式在 styles.css）；失败时 isError 走 QueryState + 重试
  const [pull, setPull] = useState(0);
  const pullStartY = useRef(0);
  const pulling = useRef(false);
  const onTouchStart = (e: React.TouchEvent) => {
    if (window.scrollY > 0 || list.isRefetching || list.isError) return;
    pulling.current = true;
    pullStartY.current = e.touches[0]?.clientY ?? 0;
  };
  const onTouchMove = (e: React.TouchEvent) => {
    if (!pulling.current) return;
    const dy = (e.touches[0]?.clientY ?? 0) - pullStartY.current;
    setPull(dy <= 0 ? 0 : Math.min(dy * 0.5, PTR_H * 2));
  };
  const onTouchEnd = () => {
    if (!pulling.current) return;
    pulling.current = false;
    const triggered = pull >= PTR_TRIGGER;
    setPull(0);
    if (triggered) void list.refetch();
  };

  const canEdit = (t: Transaction) => {
    if(t.installment_plan_id||t.liability_term_id||t.liability_operation_id)return false;
    if (t.voucher_event_id || (t.purpose && t.purpose !== 'regular')) return false;
    if (!user || user.role === 'viewer') return false;
    if (user.role === 'admin' || user.role === 'recorder') return true;
    return t.created_by === user.id;
  };

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
      refunded: Number(t.refunded_amount ?? 0),
      platform: t.platform,
      installment_plan_id:t.installment_plan_id??undefined,
      liability_account_id:t.liability_account_id??undefined,
    });
    setFormOpen(true);
  };
  /** 点击「已退 ¥x」→ 定位该支出的关联退款（specs/refunds 可跳转）。 */
  const locateRefunds = (t: Transaction) => {
    patch({ related_txn_id: t.id, kind: '', account_id: undefined, category_id: undefined });
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

  const kindTag = (k: TxnKind) =>
    k === 'expense' ? '支出' : k === 'income' ? '收入' : k === 'refund' ? '退款' : '转账';
  const tone = (k: TxnKind) =>
    k === 'expense' ? 'expense' : k === 'income' || k === 'refund' ? 'income' : 'plain';

  // 金额带方向：收入/退款为正，支出/转账为负（退款恒正，specs/refunds 金额恒正）
  const amountOf = (t: Transaction) =>
    t.kind === 'income' || t.kind === 'refund' ? t.amount : `-${t.amount}`;

  const actions = (t: Transaction) =>
    <>{t.liability_account_id && !t.installment_plan_id && !t.liability_term_id && !t.liability_operation_id && <Button type="link" size="small" onClick={()=>navigate(`/liabilities/${t.liability_account_id}?source=${t.id}`)}>账单</Button>}{t.installment_plan_id||t.liability_term_id||t.liability_operation_id ? <Button type="link" size="small" onClick={()=>navigate(`/liabilities/${t.liability_account_id??(t.kind==='transfer'?t.transfer_account_id:t.account_id)}?source=${t.id}`)}>查看负债记录</Button> : t.voucher_id ? <Button type="link" size="small" onClick={() => navigate(`/vouchers?id=${t.voucher_id}`)}>查看券</Button> : canEdit(t) ? (
      <span className="row-actions">
        <Button type="link" size="small" onClick={() => openEdit(t)}>
          编辑
        </Button>
        {t.kind === 'expense' && (
          <Button type="link" size="small" onClick={() => openRefund(t)}>
            退款
          </Button>
        )}
        {t.kind === 'expense' && accounts.data?.find((a)=>a.id===t.account_id)?.type==='liability' && <Button type="link" size="small" onClick={()=>navigate(`/liabilities/${t.account_id}`,{state:{liabilityDraft:{source_txn_id:t.id,entry_date:t.entry_date}}})}>转分期</Button>}
        <Popconfirm
          title="删除这笔流水？"
          onConfirm={() => doDelete(t)}
          okButtonProps={{ loading: delTxn.isPending, disabled: delTxn.isPending }}
        >
          <Button type="link" size="small" danger>
            删除
          </Button>
        </Popconfirm>
      </span>
    ) : null}</>;

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
          {(t.purpose && VOUCHER_PURPOSE_LABEL[t.purpose]) || kindTag(k)}
          {t.source === 'recurring' && <Tag className="src-tag">自动</Tag>}
          {t.source === 'voucher_auto' && <Tag className="src-tag">到期自动</Tag>}
          {t.source === 'liability_auto' && <Tag className="src-tag">分期费用</Tag>}
          {k === 'expense' && Number(t.refunded_amount ?? 0) > 0 && (
            <Tag
              className="refunded-tag"
              color="green"
              title="点击查看关联退款"
              role="button"
              tabIndex={0}
              onClick={() => locateRefunds(t)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  locateRefunds(t);
                }
              }}
            >
              已退 ¥{Number(t.refunded_amount).toLocaleString('zh-CN')}
            </Tag>
          )}
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
      render: (_: string, t: Transaction) => <><Money value={amountOf(t)} tone={tone(t.kind)} />{t.voucher_event_id && <div className="txn-effects">余额 {t.balance_effect} · 消费 {t.expense_effect}</div>}</>,
    },
    { title: '平台 / 商家', dataIndex: 'platform', key: 'platform', width: 130, ellipsis: true },
    { title: '备注', dataIndex: 'note', key: 'note', ellipsis: true },
    { title: '', key: 'actions', width: 130, render: (_: unknown, t: Transaction) => actions(t) },
  ];

  const accountOptions = (accounts.data ?? []).map((a) => ({ value: a.id, label: a.name }));

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
            { value: 'refund', label: '退款' },
          ]}
          onChange={(v) => patch({ kind: v as TxnKind | '' })}
        />
        {filters.related_txn_id ? (
          <Tag
            closable
            color="green"
            onClose={() => patch({ related_txn_id: undefined })}
          >
            关联退款 #{filters.related_txn_id}
          </Tag>
        ) : null}
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
          placeholder="备注 / 平台 / 商家"
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
        <div
          className="txn-list"
          onTouchStart={onTouchStart}
          onTouchMove={onTouchMove}
          onTouchEnd={onTouchEnd}
        >
          {/* 下拉刷新指示器：只动 transform，状态由 pull/isRefetching 驱动 */}
          <div
            className="ptr-indicator"
            style={{
              transform: `translateY(${list.isRefetching ? 0 : Math.min(pull, PTR_H) - PTR_H}px)`,
            }}
          >
            {list.isRefetching ? '刷新中…' : pull >= PTR_TRIGGER ? '松开刷新' : '下拉刷新'}
          </div>
          {/* 错误态单列：请求失败不得渲染成「暂无数据」空态（specs/ui-shell 数据加载错误态） */}
          {list.isError ? (
            <QueryState error={list.error} onRetry={() => void list.refetch()} />
          ) : !list.data && !list.isError ? (
            <div className="txn-skeleton">
              <Skeleton active paragraph={{ rows: 5 }} title={false} />
            </div>
          ) : (
            items.map((t) => (
              <div className="txn-row" key={t.id} data-txn-id={t.id}>
                <div className="txn-main">
                  <div className="txn-title">
                    <span className="txn-cat">
                      {(t.purpose && VOUCHER_PURPOSE_LABEL[t.purpose]) || (t.kind === 'transfer'
                        ? '转账'
                        : (t.category_id ? catInfo.get(t.category_id)?.full : undefined) ?? '未分类')}
                      {t.source === 'recurring' && <Tag className="src-tag">自动</Tag>}
                      {t.source === 'voucher_auto' && <Tag className="src-tag">到期自动</Tag>}
                      {t.source === 'liability_auto' && <Tag className="src-tag">分期费用</Tag>}
                      {t.kind === 'expense' && Number(t.refunded_amount ?? 0) > 0 && (
                        <Tag
                          className="refunded-tag"
                          color="green"
                          title="点击查看关联退款"
                          role="button"
                          tabIndex={0}
                          onClick={() => locateRefunds(t)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault();
                              locateRefunds(t);
                            }
                          }}
                        >
                          已退 ¥{Number(t.refunded_amount).toLocaleString('zh-CN')}
                        </Tag>
                      )}
                    </span>
                    <Money value={amountOf(t)} tone={tone(t.kind)} />
                  </div>
                  <div className="txn-sub">
                    {dayjs(t.entry_date).format('MM-DD')} ·{' '}
                    {t.kind === 'transfer'
                      ? `${acctName.get(t.account_id) ?? ''} → ${acctName.get(t.transfer_account_id ?? 0) ?? ''}`
                      : acctName.get(t.account_id) ?? ''}
                    {t.platform ? ` · ${t.platform}` : ''}
                    {t.note ? ` · ${t.note}` : ''}
                  </div>
                  {t.voucher_event_id && <div className="txn-effects">余额 {t.balance_effect} · 消费 {t.expense_effect}</div>}
                </div>
                <div className="txn-actions">{actions(t)}</div>
              </div>
            ))
          )}
          {!list.isLoading && !list.isError && items.length === 0 && (
            <div className="list-empty">
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无数据" />
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
          {list.isError ? (
            <QueryState error={list.error} onRetry={() => void list.refetch()} />
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
                ({ 'data-txn-id': record.id }) as HTMLAttributes<HTMLTableRowElement>
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
