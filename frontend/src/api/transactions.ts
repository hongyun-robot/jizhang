import { txnQueryParams } from '../features/records/filters';
import { writeEmptyApi } from './request';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api } from './client';
import type { Transaction, TxnListParams, TxnListResp } from './types';
import { readApi as unwrap, writeApi } from './request';
import { useFinancialInvalidate as useTxnInvalidate } from './cache';
import { qs } from './query';
import { queryKeys, useScope } from './keys';

/** 序列化流水查询参数（供 react-query key 稳定）。 */
export function txnQueryUrl(p: TxnListParams): string {
  return `/transactions${qs(txnQueryParams(p))}`;
}

/** 导出当前筛选流水为 CSV（任务 11.2）：与列表同参但不分页，BOM 由后端产出。 */
export async function exportTxns(p: TxnListParams): Promise<void> {
  const url = `/transactions/export${qs(txnQueryParams(p, true))}`;
  const res = await api.get(url, { responseType: 'blob', timeout: 30000 });
  const blob = new Blob([res.data], { type: 'text/csv;charset=utf-8' });
  const cd = String(res.headers['content-disposition'] ?? '');
  const name = /filename="?([^";]+)"?/.exec(cd)?.[1] ?? 'transactions.csv';
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(a.href);
}

export function useTxnList(params: TxnListParams) {
  const scope = useScope();
  const url = txnQueryUrl(params);
  return useQuery({
    queryKey: queryKeys.key('txns', scope, url),
    queryFn: ({ signal }) => unwrap<TxnListResp>(url, signal),
    placeholderData: (prev) => prev,
  });
}

/**
 * 流水列表的无限查询变体（specs/transactions 移动端列表加载）。
 * key = 扁平筛选 + 起始页：分页控件跳页换起始页即换 key —— 命中既有起始页直接取页缓存，
 * 未命中请求该页回填；上拉加载在同一 key 下追加页，不打断缓存。
 * 原 `useTxnList` 保持不动，供统计/首页复用。
 */
export function useTxnListInfinite(params: TxnListParams, startPage: number) {
  const scope = useScope();
  const flatUrl = txnQueryUrl({
    ...params,
    page: undefined,
    page_size: undefined,
  });
  const pageSize = params.page_size ?? 20;
  const client = useQueryClient();
  const key = (page: number) =>
    queryKeys.key('txns-inf', scope, flatUrl, page, pageSize);
  const query = useInfiniteQuery({
    queryKey: queryKeys.key('txns-inf', scope, flatUrl, startPage, pageSize),
    queryFn: ({ pageParam, signal }) =>
      unwrap<TxnListResp>(txnQueryUrl({ ...params, page: pageParam }), signal),
    initialPageParam: startPage,
    // pages[0] 即起始页：下一页 = 起始页 + 已加载页数（key 含起始页，闭包与缓存一致）
    getNextPageParam: (lastPage, allPages) =>
      lastPage.items.length < pageSize
        ? undefined
        : startPage + allPages.length,
    placeholderData: (prev) => prev,
  });
  const resetPages = async (page: number) => {
    const queryKey = key(page);
    await client.cancelQueries({ queryKey, exact: true });
    client.setQueryData<InfiniteData<TxnListResp, number>>(queryKey, (data) =>
      data
        ? {
            pages: data.pages.slice(0, 1),
            pageParams: data.pageParams.slice(0, 1),
          }
        : undefined
    );
  };
  return { ...query, resetPages };
}

// ---------- 流水写操作（成功后失效流水/余额/统计缓存） ----------

export interface TxnInput {
  kind: 'expense' | 'income' | 'transfer' | 'refund';
  amount: string;
  account_id: number;
  transfer_account_id?: number;
  category_id?: number;
  /** 退款关联的原支出流水 id */
  related_txn_id?: number;
  entry_date: string;
  note?: string;
  platform?: string;
}

export function useCreateTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async (input: TxnInput) => {
      const data = await writeApi<Transaction>('post', '/transactions', input);
      return data;
    },
    onSuccess: done,
  });
}

export function useUpdateTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async ({ id, input }: { id: number; input: TxnInput }) => {
      const data = await writeApi<Transaction>(
        'put',
        `/transactions/${id}`,
        input
      );
      return data;
    },
    onSuccess: done,
  });
}

export function useDeleteTxn() {
  const done = useTxnInvalidate();
  return useMutation({
    mutationFn: async (id: number) => {
      await writeEmptyApi('delete', `/transactions/${id}`);
    },
    onSuccess: done,
  });
}
