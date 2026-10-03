import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';

import { useAuth, type Role } from '../stores/auth';

interface RequireRoleProps {
  /** 单角色门禁（既有用法）。 */
  role?: Role;
  /** 多角色门禁：任一匹配即可；与 role 同时给出时以 roles 为准。 */
  roles?: readonly Role[];
  children: ReactNode;
}

/** 角色门禁：不符角色回首页（入口本身也按角色隐藏）。 */
export default function RequireRole({ role, roles, children }: RequireRoleProps) {
  const user = useAuth((s) => s.user);
  const location = useLocation();
  if (!user) return <Navigate to="/login" state={{ from: location }} replace />;
  const allowed = roles ?? (role ? [role] : []);
  if (!allowed.includes(user.role)) return <Navigate to="/" replace />;
  return <>{children}</>;
}
