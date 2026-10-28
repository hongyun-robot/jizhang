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
  sessionID: string | null;
  accessExpiresAt: number | null;
  deviceType: 'phone' | 'tablet' | 'desktop' | null;
  endReason: string | null;
  validationPending: boolean;
  validationError: Error | null;
  setSession: (token: string, user: User | null, sessionID?: string, deviceType?: 'phone' | 'tablet' | 'desktop', expiresAt?: number) => void;
  setToken: (token: string) => void;
  setUser: (user: User) => void;
  markBooted: () => void;
  clear: (reason?: string) => void;
}

export const useAuth = create<AuthState>((set) => ({
  token: null,
  user: null,
  booted: false,
  epoch: 0,
  sessionID: null,
  accessExpiresAt: null,
  deviceType: null,
  endReason: null,
  validationPending: false,
  validationError: null,
  setSession: (token, user, sessionID, deviceType, expiresAt) => set((state) => ({ token, user, sessionID: sessionID ?? state.sessionID, accessExpiresAt:expiresAt ?? (token === state.token ? state.accessExpiresAt : null), deviceType: deviceType ?? state.deviceType, booted: true, endReason: null })),
  setToken: (token) => set({ token }),
  setUser: (user) => set({ user }),
  markBooted: () => set({ booted: true }),
  clear: (reason) =>
    set((state) => ({
      token: null,
      user: null,
      booted: true,
      epoch: state.epoch + 1,
      sessionID: null,
      accessExpiresAt: null,
      deviceType: null,
      endReason: reason ?? null,
      validationPending: false,
      validationError: null,
    })),
}));
