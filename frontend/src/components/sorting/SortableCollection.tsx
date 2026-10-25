import { createContext, createElement, useContext, useEffect, useRef, useState, type HTMLAttributes, type ReactNode, type RefObject } from 'react';
import { Alert, Button } from 'antd';
import { flushSync } from 'react-dom';
import { HolderOutlined } from '@ant-design/icons';
import { Accessibility, KeyboardSensor, PointerActivationConstraints, PointerSensor, Scroller, type DragEndEvent } from '@dnd-kit/dom';
import { DragDropProvider, useDragDropManager } from '@dnd-kit/react';
import { isSortable, useSortable } from '@dnd-kit/react/sortable';
import { defaultCollisionDetection } from '@dnd-kit/collision';
import { moveItem, sameOrder } from '../../lib/ordering';

const Collection = createContext({ ids: [] as number[], disabled: false, reduced: false });
const Handle = createContext<(element: Element | null) => void>(() => {});
const sensors = [PointerSensor.configure({ activationConstraints: (event) =>
  event.pointerType === 'touch'
    ? [new PointerActivationConstraints.Delay({ value: 220, tolerance: 5 })]
    : [new PointerActivationConstraints.Distance({ value: 5 })],
}), KeyboardSensor];

export function SortableCollection({ ids, disabled = false, onOrder, onDraggingChange, scrollContainer, children }: {
  ids: number[]; disabled?: boolean; onOrder: (next: number[]) => void; onDraggingChange?: (dragging: boolean) => void; scrollContainer?: RefObject<HTMLElement | null>; children: ReactNode;
}) {
  const [reduced, setReduced] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  const dragged = useRef<number[] | undefined>(undefined);
  const suppressUntil = useRef(0);
  useEffect(() => () => onDraggingChange?.(false), [onDraggingChange]);
  useEffect(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)');
    const change = () => setReduced(media.matches);
    media.addEventListener('change', change);
    return () => media.removeEventListener('change', change);
  }, []);
  useEffect(() => {
    const suppressClick = (event: MouseEvent) => {
      if (Date.now() < suppressUntil.current && event.target instanceof Element && event.target.closest('[data-sort-handle]')) {
        event.preventDefault(); event.stopPropagation();
      }
    };
    document.addEventListener('click', suppressClick, true);
    return () => { document.removeEventListener('click', suppressClick, true); };
  }, []);
  return (
    <Collection.Provider value={{ ids, disabled: disabled || ids.length < 2, reduced }}>
      <DragDropProvider sensors={sensors}
        plugins={(defaults) => defaults.map((plugin) => plugin === Accessibility ? Accessibility.configure({
          screenReaderInstructions: { draggable: '按空格或回车开始排序，方向键移动，再按空格或回车保存，Escape 取消。' },
          announcements: {
            dragstart: () => '开始排序，使用方向键调整位置。',
            dragend: (event: DragEndEvent) => event.canceled ? '已取消排序。' : '排序已放下，正在保存。',
            dragover: () => '排序位置已改变。',
          },
        }) : plugin)}
        onBeforeDragStart={(event) => { if (disabled) event.preventDefault(); }}
        onDragStart={() => { dragged.current = [...ids]; onDraggingChange?.(true); }}
        onDragEnd={(event) => {
          suppressUntil.current = Date.now() + 350;
          const source = event.operation.source;
          if (!event.canceled && !disabled && dragged.current && sameOrder(dragged.current, ids) && isSortable(source)) {
            const area = source.sortable.target?.parentElement?.getBoundingClientRect();
            const point = event.operation.position.current;
            // The moved row occupies its new slot; excluding it from collision targets
            // can leave target=null even for a valid drop inside the same collection.
            const inside = area && point.x >= area.left && point.x <= area.right && point.y >= area.top && point.y <= area.bottom;
            if (event.operation.target || inside || event.operation.activatorEvent instanceof KeyboardEvent) {
              // Commit the preview before a fast failed save can restore it. The
              // sorting plugin already moved DOM nodes outside React's tree.
              flushSync(() => onOrder(moveItem(ids, source.initialIndex, source.index)));
            }
          }
          dragged.current = undefined;
          setTimeout(() => onDraggingChange?.(false), 0);
        }}>
        <DragSafety ids={ids} scrollContainer={scrollContainer} />
        {children}
      </DragDropProvider>
    </Collection.Provider>
  );
}

function DragSafety({ ids, scrollContainer }: { ids: number[]; scrollContainer?: RefObject<HTMLElement | null> }) {
  const manager = useDragDropManager();
  const original = useRef(ids);
  useEffect(() => {
    const scroller = manager?.registry.plugins.get(Scroller);
    if (!scroller || !scrollContainer) return;
    const getScrollable = scroller.getScrollableElements;
    scroller.getScrollableElements = () => scrollContainer.current ? new Set([scrollContainer.current]) : null;
    return () => { scroller.getScrollableElements = getScrollable; };
  }, [manager, scrollContainer]);
  useEffect(() => {
    if (!sameOrder(original.current, ids) && manager?.dragOperation.status.dragging) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true, cancelable: true }));
    }
    original.current = ids;
  }, [ids, manager]);
  return null;
}

export function SortableItem({ id, tag = 'div', excludeSelfCollision = false, children, ...props }: Omit<HTMLAttributes<HTMLElement>, 'id'> & {
  id: number; tag?: 'div' | 'li' | 'tr'; excludeSelfCollision?: boolean; children: ReactNode;
}) {
  const collection = useContext(Collection);
  const { ref, handleRef, isDragSource } = useSortable({
    id, index: collection.ids.indexOf(id), disabled: collection.disabled,
    collisionDetector: excludeSelfCollision ? (input) => input.dragOperation.source?.id === input.droppable.id ? null : defaultCollisionDetection(input) : undefined,
    transition: collection.reduced ? null : { duration: 150 },
  });
  return <Handle.Provider value={handleRef}>{createElement(tag, {
    ...props, ref, 'data-order-id': id, 'data-dragging': isDragSource || undefined,
  }, children)}</Handle.Provider>;
}
export function SortableHandle({ name }: { name: string }) {
  const ref = useContext(Handle);
  const { disabled } = useContext(Collection);
  return <button ref={ref} type="button" className="sort-handle" data-sort-handle
    aria-label={`拖动排序 ${name}`} title="拖动排序，或用键盘调整位置" disabled={disabled}
    onClick={(event) => event.stopPropagation()}><HolderOutlined /></button>;
}
export function SortableTableRow({ children, ...props }: HTMLAttributes<HTMLTableRowElement> & { 'data-row-key'?: number | string }) {
  if (!Number.isSafeInteger(Number(props['data-row-key']))) return <tr {...props}>{children}</tr>;
  const { id: _htmlId, ...attributes } = props;
  return <SortableItem tag="tr" id={Number(props['data-row-key'])} {...attributes}>{children}</SortableItem>;
}
export function OrderStatus({ busy, error, retry, refresh }: { busy: boolean; error?: string; retry: () => void; refresh: () => Promise<unknown> }) {
  return error ? <Alert className="order-status" type="error" showIcon title={error}
    action={<><Button size="small" onClick={retry}>重试保存</Button><Button size="small" onClick={() => void refresh()}>刷新列表</Button></>} />
    : <div className="order-status muted" role="status">{busy ? '正在保存排序…' : '拖动左侧手柄调整顺序，松开后自动保存'}</div>;
}
