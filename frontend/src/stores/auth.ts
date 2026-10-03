import { create } from 'zustand';

export type Role = 'admin' | 'recorder' | 'member' | 'viewer';
export type UserStatus = 'active' | 'disabled';

/** 可记账角色：与后端 `service.CanCreateTransaction` 一一对应（周期记账等入口共用）。 */
export const RECORDABLE_ROLES: readonly Role[] = ['admin', 'recorder', 'member'];

/** 是否可记账（含周期模板自助管理，specs/recurring-entries）。 */
export function canRecord(role: Role | null | undefined): boolean {
  return !!role && RECORDABLE_ROLES.includes(role);
}

export interface User {
  id: number;
  username: string;
  role: Role;
  status: UserStatus;
}

interface AuthState {
  /** access token 仅存内存（设计 D2），刷新页面靠 refresh cookie 恢复 */
  token: string | null;
  user: User | null;
  /** 是否已完成一次会话恢复尝试（防止守卫重复刷新） */
  booted: boolean;
  setSession: (token: string, user: User | null) => void;
  setToken: (token: string) => void;
  setUser: (user: User) => void;
  markBooted: () => void;
  clear: () => void;
}

export const useAuth = create<AuthState>((set) => ({
  token: null,
  user: null,
  booted: false,
  setSession: (token, user) => set({ token, user, booted: true }),
  setToken: (token) => set({ token }),
  setUser: (user) => set({ user }),
  markBooted: () => set({ booted: true }),
  clear: () => set({ token: null, user: null, booted: true }),
}));
