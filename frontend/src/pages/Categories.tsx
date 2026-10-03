import { useState } from 'react';
import { App, Button, Card, Empty, Input, Modal, Popconfirm, Spin, Tag } from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import { useCategoryForest, useCategoryMutations } from '../api/hooks';
import { rootsOfKind } from '../api/category';
import type { CategoryKind, CategoryTreeNode } from '../api/types';

/**
 * 分类管理（仅管理员，specs/categories）：
 * 支出/收入两套两级树；一级可增子分类，叶子可重命名/删除（受引用约束拦截）。
 */
export default function Categories() {
  const { message } = App.useApp();
  const forest = useCategoryForest();
  const { create, update, remove } = useCategoryMutations();

  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<CategoryKind>('expense');
  const [parent, setParent] = useState<CategoryTreeNode | null>(null);
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<CategoryTreeNode | null>(null);

  const openCreate = (k: CategoryKind, p: CategoryTreeNode | null) => {
    setKind(k);
    setParent(p);
    setRenaming(null);
    setName('');
    setOpen(true);
  };

  const openRename = (c: CategoryTreeNode) => {
    setRenaming(c);
    setParent(null);
    setName(c.name);
    setOpen(true);
  };

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      message.warning('请输入名称');
      return;
    }
    try {
      if (renaming) {
        await update.mutateAsync({ id: renaming.id, name: trimmed });
        message.success('已重命名');
      } else {
        await create.mutateAsync({
          kind,
          name: trimmed,
          parent_id: parent?.id,
        });
        message.success('已创建');
      }
      setOpen(false);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const doDelete = async (c: CategoryTreeNode) => {
    try {
      await remove.mutateAsync(c.id);
      message.success(`已删除「${c.name}」`);
    } catch (e) {
      message.error(errMsg(e)); // 409：有子分类 / 被流水引用
    }
  };

  const renderSection = (k: CategoryKind, title: string, roots?: CategoryTreeNode[] | null) => (
    <Card
      size="small"
      className="panel"
      title={title}
      extra={
        <Button size="small" icon={<PlusOutlined />} onClick={() => openCreate(k, null)}>
          新建一级
        </Button>
      }
    >
      {!roots?.length ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分类" />
      ) : (
        <div className="cat-section">
          {roots.map((root) => (
            <div className="cat-root-row" key={root.id}>
              <div className="cat-root-line">
                <span className="cat-root-name">{root.name}</span>
                <Button type="link" size="small" onClick={() => openCreate(k, root)}>
                  +子分类
                </Button>
                <Button type="link" size="small" onClick={() => openRename(root)}>
                  重命名
                </Button>
                <Popconfirm title={`删除「${root.name}」？`} onConfirm={() => doDelete(root)}>
                  <Button type="link" size="small" danger>
                    删除
                  </Button>
                </Popconfirm>
              </div>
              {root.children?.length ? (
                <div className="cat-children">
                  {root.children.map((c) => (
                    <Tag
                      key={c.id}
                      closable
                      onClose={(e) => {
                        e.preventDefault();
                        void doDelete(c);
                      }}
                      onClick={(e) => {
                        if ((e.target as HTMLElement).closest('.ant-tag-close-icon')) return;
                        openRename(c);
                      }}
                      style={{ cursor: 'pointer' }}
                    >
                      {c.name}
                    </Tag>
                  ))}
                </div>
              ) : (
                <div className="cat-children muted">无子分类（可直接记账）</div>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">CATEGORIES · 支出与收入两棵树</div>
          <h2>分类管理</h2>
        </div>
      </div>

      {forest.isLoading ? (
        <Spin />
      ) : (
        <>
          {renderSection('expense', '支出分类', rootsOfKind(forest.data, 'expense'))}
          {renderSection('income', '收入分类', rootsOfKind(forest.data, 'income'))}
        </>
      )}

      <Modal
        open={open}
        onCancel={() => setOpen(false)}
        onOk={submit}
        title={renaming ? '重命名分类' : `新建分类${parent ? `（挂在「${parent.name}」下）` : '（一级）'}`}
        confirmLoading={create.isPending || update.isPending}
        destroyOnHidden
      >
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="分类名称"
          maxLength={64}
          onPressEnter={submit}
          autoFocus
        />
      </Modal>
    </div>
  );
}
