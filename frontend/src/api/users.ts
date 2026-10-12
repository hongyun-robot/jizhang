import { writeEmptyApi } from './request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Role, User } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

// ---------- 用户管理（仅管理员，specs/identity/user-management，任务 2.5） ----------

export function useUsers() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('users', scope),
    queryFn: ({ signal }) => unwrap<User[]>('/users', signal),
  });
}

export interface CreateUserInput {
  username: string;
  password: string;
  role: Role;
}

export function useUserMutations() {
  const scope = useScope();
  const qc = useQueryClient();
  // 角色/状态变化会改变权限与可见入口，统一刷新用户列表
  const done = () =>
    void qc.invalidateQueries({ queryKey: queryKeys.root('users', scope) });
  const create = useMutation({
    mutationFn: async (input: CreateUserInput) => {
      const data = await writeApi<User>('post', '/users', input);
      return data;
    },
    onSuccess: done,
  });
  const setRole = useMutation({
    mutationFn: async ({ id, role }: { id: number; role: Role }) => {
      const data = await writeApi<User>('patch', `/users/${id}/role`, {
        role,
      });
      return data;
    },
    onSuccess: done,
  });
  const setStatus = useMutation({
    mutationFn: async ({
      id,
      status,
    }: {
      id: number;
      status: 'active' | 'disabled';
    }) => {
      const data = await writeApi<User>('patch', `/users/${id}/status`, {
        status,
      });
      return data;
    },
    onSuccess: done,
  });
  /** 重置密码：后端会使该用户已签发 token 全部失效（含当前会话，若改的是自己）。 */
  const resetPassword = useMutation({
    mutationFn: async ({ id, password }: { id: number; password: string }) => {
      await writeEmptyApi('post', `/users/${id}/password`, {
        password,
      });
    },
    onSuccess: done,
  });
  return { create, setRole, setStatus, resetPassword };
}
