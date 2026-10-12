export function qs(
  params: Record<string, string | number | undefined | null>
): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params).sort(([a], [b]) =>
    a.localeCompare(b)
  )) {
    if (value !== undefined && value !== null && value !== '')
      search.set(key, String(value));
  }
  const text = search.toString();
  return text ? '?' + text : '';
}
