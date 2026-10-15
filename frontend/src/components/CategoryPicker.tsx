import { useState } from 'react';
import { Tag } from 'antd';
import type { CategoryTreeNode } from '../api/types';

/** 业务表单选择末级分类；筛选模式允许选择整个大类或全部分类。 */
export default function CategoryPicker({
  roots,
  value,
  onChange,
  disabled,
  id,
  allowEmpty = false,
  error = false,
  heading,
  emptyLabel = '未分类',
  mode = 'leaf',
  'aria-label': ariaLabel = '消费分类',
}: {
  roots: CategoryTreeNode[];
  value?: number;
  onChange?: (value: number | undefined) => void;
  disabled?: boolean;
  id?: string;
  allowEmpty?: boolean;
  error?: boolean;
  heading?: string;
  emptyLabel?: string;
  mode?: 'leaf' | 'filter';
  'aria-label'?: string;
}) {
  const [activeId, setActiveId] = useState<number>();
  const filtering = mode === 'filter';
  const selectedRoot = roots.find(
    (root) =>
      root.id === value || root.children?.some((leaf) => leaf.id === value)
  );
  const activeRoot =
    selectedRoot ??
    (filtering
      ? undefined
      : roots.find((root) => root.id === activeId) ?? roots[0]);
  const leaves = activeRoot?.children?.length
    ? activeRoot.children
    : activeRoot && !filtering
      ? [activeRoot]
      : [];

  const button = (
    key: number | string,
    label: string,
    checked: boolean,
    choose: () => void
  ) => (
    <Tag.CheckableTag
      key={key}
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
      {label}
    </Tag.CheckableTag>
  );

  return (
    <div
      id={id}
      className={`cat-pick${error ? ' pick-error' : ''}`}
      role="group"
      aria-label={ariaLabel}
    >
      {heading && <div className="pick-label">{heading}</div>}
      <div className="cat-roots">
        {filtering &&
          button('all-roots', '全部', value === undefined, () =>
            onChange?.(undefined)
          )}
        {roots.map((root) =>
          button(root.id, root.name, activeRoot?.id === root.id, () => {
            setActiveId(root.id);
            onChange?.(!filtering && root.children?.length ? undefined : root.id);
          })
        )}
      </div>
      {(filtering || leaves.length > 0) && (
        <div className="pick-label">子分类</div>
      )}
      <div className="cat-leaves">
        {filtering &&
          button(
            'all-leaves',
            '全部',
            value === undefined || value === activeRoot?.id,
            () => onChange?.(activeRoot?.id)
          )}
        {leaves.map((leaf) =>
          button(leaf.id, leaf.name, value === leaf.id, () => onChange?.(leaf.id))
        )}
        {allowEmpty && !filtering && (
          <Tag.CheckableTag
            checked={!value}
            onChange={() => {
              if (!disabled) onChange?.(undefined);
            }}
            disabled={disabled}
          >
            {emptyLabel}
          </Tag.CheckableTag>
        )}
      </div>
    </div>
  );
}
