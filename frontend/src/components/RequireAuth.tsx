import { useEffect, useState } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { Spin } from 'antd';

import { api, ensureFreshToken } from '../api/client';
import { useAuth, type User } from '../stores/auth';

/**
 * 路由守卫：无内存 token 时用 refresh cookie 恢复一次会话，
 * 成则放行，败则重定向登录页（specs/ui-shell 未登录访问场景）。
 */
export default function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuth((s) => s.token);
  const user = useAuth((s) => s.user);
  const booted = useAuth((s) => s.booted);
  const setSession = useAuth((s) => s.setSession);
  const clear = useAuth((s) => s.clear);
  const location = useLocation();
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    let cancelled = false;
    if (user) {
      setChecking(false);
      return;
    }
    if (!token && booted) {
      setChecking(false); // 恢复失败过 → 走重定向
      return;
    }
    (async () => {
      try {
        const t = await ensureFreshToken();
        const { data } = await api.get<{ code: number; data: User }>('/auth/me');
        if (cancelled) return;
        if (data.code === 0 && data.data) {
          setSession(t, data.data);
        } else {
          clear();
        }
      } catch {
        if (!cancelled) clear();
      } finally {
        if (!cancelled) setChecking(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token, user, booted, setSession, clear]);

  if (checking) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100dvh' }}>
        <Spin size="large" />
      </div>
    );
  }
  if (!token || !user) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  return <>{children}</>;
}
