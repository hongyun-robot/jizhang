import type { CategoryOption } from '../domain/categories';
import { categoryPath } from '../domain/categories';
import type { CategoryTreeNode } from '../api/types';
import CategoryPicker from './CategoryPicker';
import { useSelectionActive } from './SelectionScope';

export default function ResponsiveCategorySelect({
  options,
  value,
  onChange,
  active: requestedActive = true,
}: {
  options: CategoryOption[];
  value?: number[];
  onChange: (path: number[]) => void;
  active?: boolean;
}) {
  const active = useSelectionActive(requestedActive);
  const node = (option: CategoryOption): CategoryTreeNode => ({
    id: option.value,
    name: option.label,
    kind: 'expense',
    sort: 0,
    children: option.children?.map(node),
  });
  return (
    <div className="category-filter">
      <CategoryPicker
        heading="分类"
        aria-label="分类"
        roots={options.map(node)}
        value={value?.at(-1)}
        disabled={!active}
        mode="filter"
        onChange={(id) => onChange(categoryPath(options, id) ?? [])}
      />
    </div>
  );
}
