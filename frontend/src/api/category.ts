import type { CategoryForest, CategoryKind, CategoryTreeNode } from './types';

/**
 * 取某 kind 的一级分类列表。
 * 后端 Tree() 返回 [包装节点(支出), 包装节点(收入)]，roots 挂在 children 上；
 * 这里按 root.kind 归属（不依赖位置）。
 */
export function rootsOfKind(forest: CategoryForest | undefined, kind: CategoryKind): CategoryTreeNode[] {
  if (!forest) return [];
  const out: CategoryTreeNode[] = [];
  for (const wrapper of forest) {
    for (const root of wrapper.children ?? []) {
      if (root.kind === kind) out.push(root);
    }
  }
  return out;
}
