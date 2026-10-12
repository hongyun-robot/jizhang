import { useState } from 'react';
import { Tag } from 'antd';
import type { CategoryTreeNode } from '../api/types';

/** 与记账表单一致的一级分类/子分类按钮，提交值始终是叶子分类。 */
export default function CategoryPicker({
  roots,
  value,
  onChange,
  disabled,
  id,
  allowEmpty = false,
  error = false,
  heading,
}: {
  roots: CategoryTreeNode[];
  value?: number;
  onChange?: (value: number | undefined) => void;
  disabled?: boolean;
  id?: string;
  allowEmpty?: boolean;
  error?: boolean;
  heading?: string;
}) {
  const [activeId, setActiveId] = useState<number>();
  const selectedRoot = roots.find(
    (root) =>
      root.id === value || root.children?.some((leaf) => leaf.id === value)
  );
  const activeRoot =
    selectedRoot ?? roots.find((root) => root.id === activeId) ?? roots[0];
  const leaves = activeRoot?.children?.length
    ? activeRoot.children
    : activeRoot
      ? [activeRoot]
      : [];

  const button = (
    category: CategoryTreeNode,
    checked: boolean,
    choose: () => void
  ) => (
    <Tag.CheckableTag
      key={category.id}
      checked={checked}
      disabled={disabled}
      onChange={() => {
        if (!disabled) choose();
      }}
      onKeyDown={(event) => {
        if (!disabled && (event.key === 'Enter' || event.key === ' ')) {
          event.preventDefault();
          choose();
        }
      }}
      style={disabled ? { opacity: 0.55, cursor: 'not-allowed' } : undefined}
    >
      {category.name}
    </Tag.CheckableTag>
  );

  return (
    <div
      id={id}
      className={`cat-pick${error ? ' pick-error' : ''}`}
      role="group"
      aria-label="消费分类"
    >
      {heading && <div className="pick-label">{heading}</div>}
      <div className="cat-roots">
        {roots.map((root) =>
          button(root, activeRoot?.id === root.id, () => {
            setActiveId(root.id);
            onChange?.(root.children?.length ? undefined : root.id);
          })
        )}
      </div>
      {leaves.length > 0 && <div className="pick-label">子分类</div>}
      <div className="cat-leaves">
        {leaves.map((leaf) =>
          button(leaf, value === leaf.id, () => onChange?.(leaf.id))
        )}
        {allowEmpty && (
          <Tag.CheckableTag
            checked={!value}
            onChange={() => onChange?.(undefined)}
            disabled={disabled}
          >
            未分类
          </Tag.CheckableTag>
        )}
      </div>
    </div>
  );
}
