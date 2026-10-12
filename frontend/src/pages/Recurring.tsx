import RecurringFormModal from '../features/recurring/RecurringFormModal';
import { categoryIndex } from '../domain/categories';

import { canManageRule } from '../domain/permissions';
import { QueryBoundary } from '../components/QueryState';
import { useMemo, useState } from 'react';
import dayjs from 'dayjs';
import { App, Button, Card, Empty, Popconfirm, Tag } from 'antd';
import {
  PauseOutlined,
  PlayCircleOutlined,
  PlusOutlined,
} from '@ant-design/icons';

import { errMsg } from '../api/client';

import { useAccounts } from '../api/accounts';
import { useCategoryForest } from '../api/categories';
import { useRuleMutations, useRules } from '../api/recurring';

import type { RecurringRule, RuleFreq } from '../api/types';
import { useAuth } from '../stores/auth';

import Money from '../components/Money';

const FREQ_LABEL: Record<RuleFreq, string> = {
  daily: '每日',
  weekly: '每周',
  monthly: '每月',
  yearly: '每年',
};

/**
 * 周期记账管理页（specs/recurring-entries 按创建者归属隔离）：
 * - 可记账角色自助管理自己的模板；管理员可见并可管全部（列表带创建人）
 * - 模板 CRUD + 启用/暂停；频率四种，首次生成日期起算
 * - 生成的流水在流水页独立改删（带「自动」标记），不回写模板
 */
export default function Recurring() {
  const { message } = App.useApp();
  const user = useAuth((s) => s.user);
  const isAdmin = user?.role === 'admin';

  const rules = useRules();
  const accounts = useAccounts();
  const forest = useCategoryForest();
  const { setStatus, remove } = useRuleMutations();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<RecurringRule | null>(null);

  const acctName = useMemo(
    () => new Map((accounts.data ?? []).map((a) => [a.id, a.name])),
    [accounts.data]
  );

  // 分类可读名：一级无子级取自身，否则「一级/子级」
  const catInfo = useMemo(
    () =>
      new Map(
        [...categoryIndex(forest.data).info].map(([id, entry]) => [
          id,
          entry.full,
        ])
      ),
    [forest.data]
  );

  const openCreate = () => {
    setEditing(null);
    setOpen(true);
  };

  const openEdit = (rule: RecurringRule) => {
    setEditing(rule);
    setOpen(true);
  };

  const toggle = async (r: RecurringRule) => {
    try {
      await setStatus.mutateAsync({
        id: r.id,
        status: r.status === 'enabled' ? 'paused' : 'enabled',
      });
      message.success(r.status === 'enabled' ? '已暂停' : '已启用');
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const doDelete = async (r: RecurringRule) => {
    try {
      await remove.mutateAsync(r.id);
      message.success('已删除模板，已生成的流水保留');
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">RECURRING · 到期自动补记</div>
          <h2>周期记账</h2>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建周期
        </Button>
      </div>

      <QueryBoundary query={rules}>
        <Card
          size="small"
          className="panel"
          title="模板"
          loading={rules.isPending}
        >
          {(rules.data ?? []).length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="还没有周期模板"
            />
          ) : (
            <div className="rule-list" data-rule-count={rules.data!.length}>
              {rules.data!.map((r) => (
                <div className="rule-row" key={r.id} data-rule-id={r.id}>
                  <div className="rule-top">
                    <span className="rule-amount">
                      <Money value={r.amount} />
                    </span>
                    <Tag>{FREQ_LABEL[r.freq]}</Tag>
                    <Tag
                      className="rule-status"
                      color={r.status === 'enabled' ? 'success' : undefined}
                    >
                      {r.status === 'enabled' ? '启用' : '暂停'}
                    </Tag>
                  </div>
                  <div className="rule-meta">
                    {r.created_by_name ? `${r.created_by_name} · ` : ''}
                    {catInfo.get(r.category_id) ?? '分类已删除'} ·{' '}
                    {acctName.get(r.account_id) ?? '账户已删除'}
                    {r.note ? ` · ${r.note}` : ''}
                  </div>
                  <div className="rule-meta">
                    {dayjs(r.start_date).format('YYYY-MM-DD')} 起 · 下次生成{' '}
                    {dayjs(r.next_run_date).format('YYYY-MM-DD')}
                  </div>
                  {canManageRule(user, r) && (
                    <div className="rule-actions">
                      <Button
                        size="small"
                        icon={
                          r.status === 'enabled' ? (
                            <PauseOutlined />
                          ) : (
                            <PlayCircleOutlined />
                          )
                        }
                        loading={setStatus.isPending}
                        onClick={() => toggle(r)}
                      >
                        {r.status === 'enabled' ? '暂停' : '启用'}
                      </Button>
                      <Button size="small" onClick={() => openEdit(r)}>
                        编辑
                      </Button>
                      <Popconfirm
                        title="删除这个模板？"
                        description="已生成的流水会保留"
                        onConfirm={() => doDelete(r)}
                      >
                        <Button size="small" danger>
                          删除
                        </Button>
                      </Popconfirm>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
          <div className="muted small" style={{ marginTop: 12 }}>
            {isAdmin
              ? '显示全部模板（含各成员创建的，前缀为创建人）；'
              : '仅显示你自己创建的模板，他人代建的由管理员维护；'}
            到期流水由服务端每 5
            分钟生成，打开应用也会自动补齐；生成的流水在「流水」页带「自动」
            标记，可单独修改或删除，不影响模板。暂停期间到期的期次会在重新启用后补齐。
          </div>
        </Card>
      </QueryBoundary>

      <RecurringFormModal
        open={open}
        editing={editing}
        onClose={() => setOpen(false)}
      />
    </div>
  );
}
