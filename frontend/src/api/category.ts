import { categoryIndex } from '../domain/categories';
import type { CategoryForest, CategoryKind, CategoryTreeNode } from './types';
export function rootsOfKind(
  forest: CategoryForest | undefined,
  kind: CategoryKind
): CategoryTreeNode[] {
  return categoryIndex(forest).rootsFor(kind);
}
