import { useInvalidation } from './cache';
import { writeEmptyApi } from './request';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { Account, PrincipalRemainder } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

// ---------- 查询 ----------

export function useAccounts() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('accounts', scope),
    queryFn: ({ signal }) => unwrap<Account[]>('/accounts', signal),
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}

// ---------- 账户 / 分类写操作（管理员） ----------

export interface AccountInput {
  request_key?: string;
  name: string;
  type: 'asset' | 'liability';
  asset_class?: 'cash' | 'fund';
  opening_balance?: string;
  repayment_day?: number;
  liability_type?: string;
  statement_day?: number;
  repayment_month_offset?: number;
  principal_remainder?: PrincipalRemainder;
  sort?: number;
  status?: 'active' | 'disabled';
}

export function useAccountMutations() {
  const done = useInvalidation('account');
  const create = useMutation({
    mutationFn: async (input: AccountInput) => {
      const data = await writeApi<Account>('post', '/accounts', input);
      return data;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({ id, input }: { id: number; input: AccountInput }) => {
      const data = await writeApi<Account>('put', `/accounts/${id}`, input);
      return data;
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      await writeEmptyApi('delete', `/accounts/${id}`);
    },
    onSuccess: done,
  });
  return { create, update, remove };
}
