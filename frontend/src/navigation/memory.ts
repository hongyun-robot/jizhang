const searches = new Map<string, string>();
export function rememberSearch(pathname: string, search: string): void {
  searches.set(pathname, search);
}
export function clearNavigationMemory(): void {
  searches.clear();
}
export function lastSearchOf(pathname: string): string {
  return searches.get(pathname) ?? '';
}
export function navTarget(path: string, current: string): string | null {
  const target = path + lastSearchOf(path);
  return target === current ? null : target;
}
