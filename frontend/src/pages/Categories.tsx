import { useRef, useState } from 'react';
import { App, Button, Card, Empty, Input, Modal, Popconfirm, Tag } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { QueryBoundary } from '../components/QueryState';
import { errMsg } from '../api/client';
import { useCategoryForest, useCategoryMutations, useReorderCategories } from '../api/categories';
import { rootsOfKind } from '../api/category';
import { useCatalogOrder } from '../hooks/useCatalogOrder';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import { OrderStatus, SortableCollection, SortableHandle, SortableItem } from '../components/sorting/SortableCollection';
import type { CategoryKind, CategoryTreeNode } from '../api/types';
import { useAuth } from '../stores/auth';

interface ListProps {
  rows: CategoryTreeNode[];
  kind: CategoryKind;
  parentId: number | null;
  title: string;
  busy: boolean;
  reload: () => Promise<unknown>;
  onCreate: () => void;
  onRename: (row: CategoryTreeNode) => void;
  onDelete: (row: CategoryTreeNode) => Promise<void>;
  onManage?: (row: CategoryTreeNode) => void;
  onDraggingChange?: (dragging: boolean) => void;
}
function CategoryList({ rows, kind, parentId, title, busy, reload, onCreate, onRename, onDelete, onManage, onDraggingChange }: ListProps) {
  const scrollContainer = useRef<HTMLDivElement>(null);
  const reorder = useReorderCategories({ kind, parent_id: parentId });
  const order = useCatalogOrder(rows, `categories:${kind}:${parentId}`, reorder.mutateAsync, reload);
  const blocked = busy || order.busy || reorder.directoryBusy;
  const content = <>
    <OrderStatus {...order} />
    {!rows.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分类" /> :
      <SortableCollection ids={order.ids} disabled={blocked} onOrder={order.commit} onDraggingChange={onDraggingChange} scrollContainer={parentId === null ? undefined : scrollContainer}>
        <div ref={scrollContainer} className={parentId === null ? 'cat-section' : 'cat-child-list'} aria-label={title}>
          {order.rows.map((row) => parentId === null ? (
            <SortableItem id={row.id} excludeSelfCollision className="cat-root-row" key={row.id}>
              <div className="cat-root-line">
                <SortableHandle name={row.name} />
                <Button type="text" className="cat-root-name" onClick={() => onManage?.(row)} aria-label={`管理子分类 ${row.name}`}>{row.name}</Button>
                <Button type="link" size="small" disabled={blocked} onClick={() => onManage?.(row)}>子分类</Button>
                <Button type="link" size="small" disabled={blocked} onClick={() => onRename(row)}>重命名</Button>
                <Popconfirm title={`删除「${row.name}」？`} onConfirm={() => onDelete(row)} disabled={blocked}>
                  <Button type="link" size="small" danger disabled={blocked}>删除</Button>
                </Popconfirm>
              </div>
              <button type="button" className="cat-root-trigger" onClick={() => onManage?.(row)} aria-label={`查看 ${row.name} 子分类`}>
                <span className="cat-children">
                  {row.children?.length ? row.children.map((child) => <Tag key={child.id}>{child.name}</Tag>) : <span className="muted">无子分类（可直接记账）</span>}
                </span>
              </button>
            </SortableItem>
          ) : (
            <SortableItem id={row.id} className="cat-child-row" key={row.id}>
              <SortableHandle name={row.name} /><span className="cat-child-name">{row.name}</span>
              <div className="cat-child-actions">
                <Button type="link" size="small" disabled={blocked} onClick={() => onRename(row)}>重命名</Button>
                <Popconfirm title={`删除「${row.name}」？`} onConfirm={() => onDelete(row)} disabled={blocked}>
                  <Button type="link" size="small" danger disabled={blocked}>删除</Button>
                </Popconfirm>
              </div>
            </SortableItem>
          ))}
        </div>
      </SortableCollection>}
  </>;
  const add = <Button size="small" icon={<PlusOutlined />} disabled={blocked} onClick={onCreate}>{parentId === null ? '新建一级' : '新增子分类'}</Button>;
  return parentId === null ? <Card size="small" className="panel" title={title} extra={add}>{content}</Card>
    : <>{add}<div style={{ marginTop: 12 }}>{content}</div></>;
}
export default function Categories() {
  const { message } = App.useApp();
  const forest = useCategoryForest();
  const { create, update, remove } = useCategoryMutations();
  const directory = useReorderCategories();
  const [childrenId, setChildrenId] = useState<number | null>(null);
  const [childrenDragging, setChildrenDragging] = useState(false);
  const roots = (forest.data ?? []).flatMap((tree) => tree.children ?? []);
  const childrenParent = roots.find((row) => row.id === childrenId);
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<CategoryKind>('expense');
  const [parentId, setParentId] = useState<number | undefined>();
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<CategoryTreeNode | null>(null);
  const saving = useRef(false);
  const generation = useRef(0);
  useVisualViewportKeyboard(open);
  const busy = create.isPending || update.isPending || remove.isPending || directory.directoryBusy;
  const openCreate = (k: CategoryKind, p?: CategoryTreeNode) => {
    generation.current++; setKind(k); setParentId(p?.id); setRenaming(null); setName(''); setOpen(true);
  };
  const openRename = (row: CategoryTreeNode) => {
    generation.current++; setRenaming(row); setParentId(undefined); setName(row.name); setOpen(true);
  };
  const closeEditor = () => { generation.current++; setOpen(false); };
  const submit = async () => {
    if (saving.current || directory.directoryBusy) return;
    const trimmed = name.trim();
    if (!trimmed) { message.warning('请输入名称'); return; }
    const started = generation.current;
    const epoch = useAuth.getState().epoch;
    saving.current = true;
    try {
      if (renaming) await update.mutateAsync({ id: renaming.id, name: trimmed });
      else await create.mutateAsync({ kind, name: trimmed, parent_id: parentId });
      if (epoch === useAuth.getState().epoch && started === generation.current) { message.success(renaming ? '已重命名' : '已创建'); closeEditor(); }
    } catch (error) {
      if (epoch === useAuth.getState().epoch && started === generation.current) message.error(errMsg(error));
    } finally { saving.current = false; }
  };
  const doDelete = async (row: CategoryTreeNode) => {
    if (busy) return;
    const epoch = useAuth.getState().epoch;
    try { await remove.mutateAsync(row.id); if (epoch === useAuth.getState().epoch) message.success(`已删除「${row.name}」`); }
    catch (error) { if (epoch === useAuth.getState().epoch) message.error(errMsg(error)); }
  };
  const common = { busy, reload: forest.refetch, onRename: openRename, onDelete: doDelete };
  return <div className="page">
    <div className="page-head"><div><div className="eyebrow">CATEGORIES · 支出与收入两棵树</div><h2>分类管理</h2></div></div>
    <QueryBoundary query={forest}>
      <CategoryList {...common} rows={rootsOfKind(forest.data, 'expense')} kind="expense" parentId={null} title="支出分类" onCreate={() => openCreate('expense')} onManage={(row) => setChildrenId(row.id)} />
      <CategoryList {...common} rows={rootsOfKind(forest.data, 'income')} kind="income" parentId={null} title="收入分类" onCreate={() => openCreate('income')} onManage={(row) => setChildrenId(row.id)} />
    </QueryBoundary>
    <Modal open={!!childrenParent} keyboard={!childrenDragging} title={childrenParent ? `子分类 · ${childrenParent.name}` : '子分类'} onCancel={() => { if (!childrenDragging) setChildrenId(null); }}
      footer={<Button onClick={() => setChildrenId(null)}>完成</Button>} destroyOnHidden>
      {childrenParent && <CategoryList key={childrenParent.id} {...common} onDraggingChange={setChildrenDragging} rows={childrenParent.children ?? []} kind={childrenParent.kind} parentId={childrenParent.id} title={`${childrenParent.name}子分类列表`} onCreate={() => openCreate(childrenParent.kind, childrenParent)} />}
    </Modal>
    <Modal open={open} onCancel={closeEditor} onOk={() => void submit()} title={renaming ? '重命名分类' : parentId ? '新建分类（子分类）' : '新建分类（一级）'}
      confirmLoading={create.isPending || update.isPending} destroyOnHidden>
      <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="分类名称" maxLength={64} onPressEnter={(event) => { if (!event.nativeEvent.isComposing) void submit(); }} />
    </Modal>
  </div>;
}
