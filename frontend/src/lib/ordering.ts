export interface OrderInput {
  request_key: string;
  ordered_ids: number[];
  previous_ids: number[];
}
export interface OrderResult { ordered_ids: number[] }

export const sameOrder = (a: readonly number[], b: readonly number[]) =>
  a.length === b.length && a.every((id, index) => id === b[index]);

export function moveItem(ids: readonly number[], from: number, to: number): number[] {
  const next = [...ids];
  if (from < 0 || to < 0 || from >= ids.length || to >= ids.length) return next;
  const [id] = next.splice(from, 1);
  next.splice(to, 0, id);
  return next;
}
export function orderItems<T extends { id: number }>(rows: readonly T[], ids: readonly number[]): T[] {
  const byId = new Map(rows.map((row) => [row.id, row]));
  const ordered = ids.flatMap((id) => {
    const row = byId.get(id);
    byId.delete(id);
    return row ? [row] : [];
  });
  return [...ordered, ...byId.values()];
}
