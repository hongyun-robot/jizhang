import { create } from 'zustand';
import type { SessionUser as User } from '../domain/identity';
export type { Role, UserStatus, SessionUser as User } from '../domain/identity';
export { canRecord, RECORDABLE_ROLES } from '../domain/permissions';

interface AuthState {
  /** access token 仅存内存（设计 D2），刷新页面靠 refresh cookie 恢复 */
  token: string | null;
  user: User | null;
  /** 是否已完成一次会话恢复尝试（防止守卫重复刷新） */
  booted: boolean;
  epoch: number;
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
  epoch: 0,
  setSession: (token, user) => set({ token, user, booted: true }),
  setToken: (token) => set({ token }),
  setUser: (user) => set({ user }),
  markBooted: () => set({ booted: true }),
  clear: () =>
    set((state) => ({
      token: null,
      user: null,
      booted: true,
      epoch: state.epoch + 1,
    })),
}));
