import { useEffect, useRef, useState } from 'react';
import { Button, Cascader, Grid } from 'antd';
import type { CategoryOption } from '../domain/categories';
import SelectionSheet, { SelectionTrigger } from './SelectionSheet';
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
  const desktop = !!Grid.useBreakpoint().md;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [group, setGroup] = useState<number>();
  const trigger = useRef<HTMLButtonElement>(null);
  const close = () => {
    setOpen(false);
    setQuery('');
    setGroup(undefined);
  };
  useEffect(() => {
    if (!active || desktop) close();
  }, [active, desktop]);
  if (desktop)
    return (
      <Cascader
        size="small"
        allowClear
        placeholder="分类"
        style={{ minWidth: 130 }}
        value={value}
        options={options}
        onChange={onChange}
      />
    );
  const paths = options.flatMap((root) =>
    root.children?.length
      ? root.children.map((leaf) => ({
          ...leaf,
          label: `${root.label} / ${leaf.label}`,
          path: [root.value, leaf.value],
        }))
      : [{ ...root, path: [root.value] }]
  );
  const label = paths.find((row) => row.value === value?.at(-1))?.label;
  const parent = options.find((row) => row.value === group);
  const search = query.trim().toLowerCase();
  const rows = search
    ? paths.filter((row) => row.label.toLowerCase().includes(search))
    : (parent?.children ?? options);
  return (
    <>
      <SelectionTrigger
        label={label}
        placeholder="分类"
        ariaLabel="分类"
        open={open && active}
        triggerRef={trigger}
        style={{ minWidth: 130 }}
        onClick={() => {
          if (active) setOpen(true);
        }}
      />
      <SelectionSheet
        open={open && active}
        title="选择分类"
        value={value?.at(-1)}
        currentLabel={label}
        options={rows}
        query={query}
        onSearch={setQuery}
        onClose={close}
        trigger={trigger}
        allowClear
        onClear={() => {
          onChange([]);
          close();
        }}
        navigation={
          !search && parent ? (
            <Button type="text" onClick={() => setGroup(undefined)}>
              ← {parent.label}
            </Button>
          ) : undefined
        }
        onSelect={(id) => {
          const root =
            !search && !parent
              ? options.find((row) => row.value === id)
              : undefined;
          if (root?.children?.length) {
            setGroup(id);
            return;
          }
          const path = paths.find((row) => row.value === id)?.path;
          if (path) {
            onChange(path);
            close();
          }
        }}
      />
    </>
  );
}
