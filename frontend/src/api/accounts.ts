import { useInvalidation } from './cache';
import { writeEmptyApi } from './request';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { Account } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

// ---------- 查询 ----------

export function useAccounts() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('accounts', scope),
    queryFn: ({ signal }) => unwrap<Account[]>('/accounts', signal),
  });
}

// ---------- 账户 / 分类写操作（管理员） ----------

export interface AccountInput {
  name: string;
  type: 'asset' | 'liability';
  opening_balance?: string;
  repayment_day?: number;
  liability_type?: string;
  statement_day?: number;
  repayment_month_offset?: number;
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
