import type {
  CategoryForest,
  CategoryKind,
  CategoryTreeNode,
} from '../api/types';

export interface CategoryInfo {
  name: string;
  full: string;
  kind: CategoryKind;
  parent?: number;
}
export interface CategoryOption {
  value: number;
  label: string;
  children?: CategoryOption[];
}
export function categoryIndex(forest?: CategoryForest | null) {
  const info = new Map<number, CategoryInfo>();
  const options: CategoryOption[] = [];
  const roots = (forest ?? []).flatMap((wrapper) => wrapper.children ?? []);
  for (const root of roots) {
    if (!root.id) continue;
    info.set(root.id, { name: root.name, full: root.name, kind: root.kind });
    const leaves = (root.children ?? []).filter((leaf) => leaf.id > 0);
    if (!leaves.length) {
      options.push({ value: root.id, label: root.name });
      continue;
    }
    options.push({
      value: root.id,
      label: root.kind === 'income' ? `收 · ${root.name}` : root.name,
      children: leaves.map((leaf) => {
        info.set(leaf.id, {
          name: leaf.name,
          full: `${root.name}/${leaf.name}`,
          kind: root.kind,
          parent: root.id,
        });
        return { value: leaf.id, label: leaf.name };
      }),
    });
  }
  return {
    info,
    options,
    roots,
    rootsFor: (kind: CategoryKind): CategoryTreeNode[] =>
      roots.filter((root) => root.kind === kind),
  };
}
export function categoryPath(
  options: CategoryOption[],
  id?: number
): number[] | undefined {
  if (!id) return undefined;
  for (const option of options) {
    if (option.value === id) return [id];
    if (option.children?.some((child) => child.value === id))
      return [option.value, id];
  }
  return undefined;
}
