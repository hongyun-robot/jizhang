import type { Role } from '../domain/identity';

export const navItems = [
  { key: 'dash', path: '/', label: '记一笔' },
  { key: 'records', path: '/records', label: '流水' },
  { key: 'stats', path: '/stats', label: '统计' },
  { key: 'mine', path: '/mine', label: '我的' },
] as const;
const adminPaths = ['/settings', '/audit'];
export function canVisit(pathname: string, role?: Role): boolean {
  if (
    adminPaths.some(
      (path) => pathname === path || pathname.startsWith(path + '/')
    )
  )
    return role === 'admin';
  if (pathname === '/recurring') return !!role && role !== 'viewer';
  return true;
}
export function activeKey(pathname: string): string {
  if (['/vouchers', '/liabilities', '/accounts', '/categories', '/funds', '/fund-positions'].some((path) => pathname.startsWith(path)))
    return 'mine';
  if (pathname === '/') return 'dash';
  return (
    navItems.find((item) => item.path !== '/' && pathname.startsWith(item.path))
      ?.key ?? 'dash'
  );
}
export function returnTarget(
  value: unknown,
  role?: Role,
  basename = '/'
): string {
  let raw: string;
  if (typeof value === 'string') raw = value;
  else if (
    value &&
    typeof value === 'object' &&
    'pathname' in value &&
    typeof value.pathname === 'string'
  ) {
    const search =
      'search' in value && typeof value.search === 'string' ? value.search : '';
    const hash =
      'hash' in value && typeof value.hash === 'string' ? value.hash : '';
    raw = value.pathname + search + hash;
  } else return '/';
  if (
    !raw.startsWith('/') ||
    raw.startsWith('//') ||
    raw.includes('\\') ||
    /[\u0000-\u001f]/.test(raw)
  )
    return '/';
  const prefix = basename === '/' ? '' : basename.replace(/\/$/, '');
  if (prefix && (raw === prefix || raw.startsWith(prefix + '/')))
    raw = raw.slice(prefix.length) || '/';
  const parsed = new URL(raw, 'https://local.invalid');
  if (
    parsed.origin !== 'https://local.invalid' ||
    parsed.pathname === '/login' ||
    !canVisit(parsed.pathname, role)
  )
    return '/';
  return parsed.pathname + parsed.search + parsed.hash;
}
