import { useIsMutating, useMutation, useQueryClient } from '@tanstack/react-query';
import { queryKeys, useScope } from './keys';
import { requestJson } from './request';
import { useAuth } from '../stores/auth';
import { orderItems, type OrderInput, type OrderResult } from '../lib/ordering';
import type { Account, CategoryForest, CategoryKind, VoucherPlatform } from './types';

type Directory = 'accounts' | 'categories' | 'voucher-platforms';

export function useTxnReorder() {
  const epoch = useScope();
  const client = useQueryClient();
  const key = ['transaction-reorder', epoch] as const;
  const mutation = useMutation({
    mutationKey: key,
    retry: false,
    mutationFn: async (input: OrderInput) => {
      if (epoch !== useAuth.getState().epoch) throw new Error('会话已结束');
      await client.cancelQueries({ queryKey: queryKeys.root('transactions', epoch) });
      const options = { method: 'put', data: input, _epoch: epoch };
      return requestJson<OrderResult>('/transactions/reorder', options);
    },
    onSuccess: async () => {
      if (epoch !== useAuth.getState().epoch) return;
      await client.invalidateQueries({ queryKey: queryKeys.root('transactions', epoch) });
    },
  });
  return { ...mutation, directoryBusy: useIsMutating({ mutationKey: key }) > 0 };
}

export function useCatalogReorder(resource: Directory, category?: { kind: CategoryKind; parent_id: number | null }) {
  const epoch = useScope();
  const client = useQueryClient();
  const key = ['catalog-reorder', epoch, resource] as const;
  const mutation = useMutation({
    mutationKey: key,
    retry: false,
    mutationFn: async (input: OrderInput) => {
      if (epoch !== useAuth.getState().epoch) throw new Error('会话已结束');
      await client.cancelQueries({ queryKey: queryKeys.root(resource, epoch) });
      const options = { method: 'put', data: { ...input, ...category }, _epoch: epoch };
      return requestJson<OrderResult>(`/${resource}/reorder`, options);
    },
    onSuccess: async (result) => {
      await client.cancelQueries({ queryKey: queryKeys.root(resource, epoch) });
      if (epoch !== useAuth.getState().epoch) return;
      const order = <T extends { id: number; sort: number }>(rows: T[]) =>
        orderItems(rows, result.ordered_ids).map((row, sort) => ({ ...row, sort }));
      if (resource === 'categories' && category) {
        client.setQueryData<CategoryForest>(queryKeys.key(resource, epoch), (forest) =>
          forest?.map((tree) => ({ ...tree, children: tree.children?.map((root) =>
            root.id === category.parent_id ? { ...root, children: order(root.children ?? []) } : root
          ) })).map((tree) => category.parent_id === null && tree.children?.[0]?.kind === category.kind
            ? { ...tree, children: order(tree.children) } : tree));
      } else {
        client.setQueryData<Array<Account | VoucherPlatform>>(queryKeys.key(resource, epoch),
          (rows) => rows && order(rows));
      }
      await client.invalidateQueries({ queryKey: queryKeys.root(resource, epoch) });
    },
  });
  return { ...mutation, directoryBusy: useIsMutating({ mutationKey: key }) > 0 };
}
