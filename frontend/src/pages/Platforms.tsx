import { useEffect, useRef, useState } from 'react';
import { App, Button, Card, Empty, Form, Input, Modal, Popconfirm } from 'antd';
import { PlusOutlined, SearchOutlined } from '@ant-design/icons';
import {
  useCreateVoucherPlatform,
  useDeleteVoucherPlatform,
  useRenameVoucherPlatform,
  useVoucherPlatforms,
  useReorderVoucherPlatforms,
} from '../api/vouchers';
import type { VoucherPlatform } from '../api/types';
import { errMsg } from '../api/client';
import { QueryBoundary } from '../components/QueryState';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import { useAuth } from '../stores/auth';
import { useCatalogOrder } from '../hooks/useCatalogOrder';
import { OrderStatus, SortableCollection, SortableHandle, SortableItem } from '../components/sorting/SortableCollection';

export default function Platforms() {
  const { message } = App.useApp();
  const platforms = useVoucherPlatforms(true);
  const create = useCreateVoucherPlatform();
  const rename = useRenameVoucherPlatform();
  const remove = useDeleteVoucherPlatform();
  const reorder = useReorderVoucherPlatforms();
  const order = useCatalogOrder(platforms.data ?? [], 'voucher-platforms', reorder.mutateAsync, platforms.refetch);
  const sorting = order.busy || reorder.directoryBusy;
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<VoucherPlatform | null>(null);
  const [name, setName] = useState('');
  const [validation, setValidation] = useState('');
  const [deleting, setDeleting] = useState<number | null>(null);
  const generation = useRef(0);
  const saving = useRef(false);
  const deletingRef = useRef(false);
  const mounted = useRef(false);
  useVisualViewportKeyboard(open);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; generation.current++; };
  }, []);

  const openEditor = (row: VoucherPlatform | null) => {
    if (sorting) return;
    generation.current++;
    setEditing(row);
    setName(row?.name ?? search.trim());
    setValidation('');
    setOpen(true);
  };
  const closeEditor = () => {
    generation.current++;
    setOpen(false);
  };
  const submit = async () => {
    if (saving.current || sorting) return;
    const trimmed = name.trim();
    if (!trimmed || Array.from(trimmed).length > 128) {
      setValidation('名称须为 1 至 128 字');
      return;
    }
    const requestGeneration = generation.current;
    const requestEpoch = useAuth.getState().epoch;
    saving.current = true;
    try {
      if (editing) await rename.mutateAsync({ id: editing.id, name: trimmed });
      else await create.mutateAsync(trimmed);
      if (mounted.current && requestEpoch === useAuth.getState().epoch && requestGeneration === generation.current) {
        message.success(editing ? '已重命名' : '已保存');
        setSearch('');
        closeEditor();
      }
    } catch (error) {
      if (mounted.current && requestEpoch === useAuth.getState().epoch && requestGeneration === generation.current) {
        setValidation(errMsg(error));
      }
    } finally {
      saving.current = false;
    }
  };
  const deleteRow = async (row: VoucherPlatform) => {
    if (deletingRef.current || sorting) return;
    const requestEpoch = useAuth.getState().epoch;
    deletingRef.current = true;
    setDeleting(row.id);
    try {
      await remove.mutateAsync(row.id);
      if (mounted.current && requestEpoch === useAuth.getState().epoch)
        message.success(`已删除「${row.name}」`);
    } catch (error) {
      if (mounted.current && requestEpoch === useAuth.getState().epoch)
        message.error(errMsg(error));
    } finally {
      deletingRef.current = false;
      if (mounted.current) setDeleting(null);
    }
  };
  const all = order.rows;
  const keyword = search.trim().toLowerCase();
  const rows = all.filter((row) => row.name.toLowerCase().includes(keyword));
  const pending = create.isPending || rename.isPending;

  return (
    <div className="page platform-page">
      <div className="page-head">
        <div>
          <div className="eyebrow">PLATFORMS · 我的记账选项</div>
          <h2>平台 / 商家管理</h2>
          <p className="muted">仅供本人使用，其他账号有各自的目录。</p>
        </div>
        <Button type="primary" icon={<PlusOutlined />} disabled={sorting} aria-label="新建平台 / 商家" onClick={() => openEditor(null)}>
          新建平台 / 商家
        </Button>
      </div>
      <Card size="small" className="panel panel--measure" title="我的平台 / 商家"
        extra={platforms.data !== undefined ? <span className="muted">{keyword ? `${rows.length} / ${all.length}` : all.length} 个名称</span> : null}>
        <Input className="platform-search" prefix={<SearchOutlined />} allowClear
          aria-label="搜索平台 / 商家" placeholder="搜索平台 / 商家"
          value={search} onChange={(event) => setSearch(event.target.value)} />
        <QueryBoundary query={platforms}>
          <OrderStatus {...order} />
          {keyword && <p className="muted">清空搜索后可拖动排序</p>}
          <SortableCollection ids={order.ids} disabled={!!keyword || sorting || pending || remove.isPending || open} onOrder={order.commit}>
          {!rows.length ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={keyword ? '无匹配平台 / 商家' : '暂无平台 / 商家'} />
          ) : (
            <ul className="platform-list" aria-label="本人平台 / 商家列表">
              {rows.map((row) => (
                <SortableItem tag="li" id={row.id} className="platform-row" key={row.id}>
                  <SortableHandle name={row.name} />
                  <span className="platform-name">{row.name}</span>
                  <div className="platform-actions">
                    <Button disabled={sorting} onClick={() => openEditor(row)} aria-label={`重命名 ${row.name}`}>重命名</Button>
                    <Popconfirm title={`删除「${row.name}」？`}
                      description="仍被本人记录使用的名称无法删除。"
                      onConfirm={() => deleteRow(row)}
                      okButtonProps={{ loading: deleting === row.id, disabled: deletingRef.current }}>
                      <Button danger disabled={remove.isPending || sorting} aria-label={`删除 ${row.name}`}>删除</Button>
                    </Popconfirm>
                  </div>
                </SortableItem>
              ))}
            </ul>
          )}
          </SortableCollection>
        </QueryBoundary>
      </Card>
      <p className="muted platform-note">重命名仅修改候选名称，历史券和流水保留原文字。</p>
      <Modal open={open} title={editing ? '重命名平台 / 商家' : '新建平台 / 商家'}
        onCancel={closeEditor} onOk={() => void submit()} okButtonProps={{ loading: pending, disabled: pending }}
        destroyOnHidden>
        <Form layout="vertical" component={false}>
          <Form.Item label="名称" htmlFor="platform-name" validateStatus={validation ? 'error' : undefined}
            help={validation || (editing
              ? '去除首尾空白后最多 128 字，不能与本人其他名称重名。'
              : '去除首尾空白后最多 128 字，同名名称会复用。')}>
            <Input id="platform-name" value={name} placeholder="平台 / 商家名称"
              onChange={(event) => { generation.current++; setName(event.target.value); setValidation(''); }}
              onPressEnter={(event) => {
                if (!event.nativeEvent.isComposing && event.keyCode !== 229) void submit();
              }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
