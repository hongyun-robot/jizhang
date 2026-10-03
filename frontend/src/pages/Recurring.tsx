import { useMemo, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import {
  App,
  Button,
  Card,
  DatePicker,
  Empty,
  Grid,
  Input,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Tag,
} from 'antd';
import { PauseOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import { rootsOfKind } from '../api/category';
import { useAccounts, useCategoryForest, useRuleMutations, useRules } from '../api/hooks';
import type { CategoryForest, CategoryKind, RecurringRule, RuleFreq } from '../api/types';
import { useAuth } from '../stores/auth';
import Money from '../components/Money';

const FREQ_OPTIONS = [
  { value: 'daily', label: '每日' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'yearly', label: '每年' },
];

const FREQ_LABEL: Record<RuleFreq, string> = {
  daily: '每日',
  weekly: '每周',
  monthly: '每月',
  yearly: '每年',
};

const KIND_OPTIONS = [
  { value: 'expense', label: '支出' },
  { value: 'income', label: '收入' },
];

interface FormState {
  kind: CategoryKind;
  amount: string;
  freq: RuleFreq;
  accountId?: number;
  categoryId?: number;
  activeRootId?: number;
  note: string;
  start: Dayjs;
}

const defaultForm = (): FormState => ({
  kind: 'expense',
  amount: '',
  freq: 'monthly',
  accountId: undefined,
  categoryId: undefined,
  activeRootId: undefined,
  note: '',
  start: dayjs(),
});

/** 编辑回填：由分类反推收支方向与所属一级分类。 */
function locate(forest: CategoryForest | undefined, catId: number): {
  kind: CategoryKind;
  rootId?: number;
} {
  for (const wrapper of forest ?? []) {
    for (const root of wrapper.children ?? []) {
      if (root.id === catId) return { kind: root.kind, rootId: root.id };
      if (root.children?.some((c) => c.id === catId)) return { kind: root.kind, rootId: root.id };
    }
  }
  return { kind: 'expense' };
}

/**
 * 周期记账管理页（specs/recurring-entries 按创建者归属隔离）：
 * - 可记账角色自助管理自己的模板；管理员可见并可管全部（列表带创建人）
 * - 模板 CRUD + 启用/暂停；频率四种，首次生成日期起算
 * - 生成的流水在流水页独立改删（带「自动」标记），不回写模板
 */
export default function Recurring() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const isAdmin = useAuth((s) => s.user?.role === 'admin');

  const rules = useRules();
  const accounts = useAccounts();
  const forest = useCategoryForest();
  const { create, update, setStatus, remove } = useRuleMutations();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<RecurringRule | null>(null);
  const [form, setForm] = useState<FormState>(defaultForm);
  const patch = (p: Partial<FormState>) => setForm((f) => ({ ...f, ...p }));

  const activeAccounts = useMemo(
    () => (accounts.data ?? []).filter((a) => a.status === 'active'),
    [accounts.data],
  );
  const accountOptions = activeAccounts.map((a) => ({
    value: a.id,
    label: `${a.name}${a.type === 'liability' ? '（信用）' : ''}`,
  }));

  const roots = useMemo(() => rootsOfKind(forest.data, form.kind), [forest.data, form.kind]);

  // 当前一级分类：由已选分类推导，否则取第一个（与记一笔表单一致）
  const activeRoot = useMemo(() => {
    if (form.activeRootId) return roots.find((r) => r.id === form.activeRootId) ?? roots[0];
    if (form.categoryId) {
      const parent = roots.find((r) => r.children?.some((c) => c.id === form.categoryId));
      if (parent) return parent;
      const self = roots.find((r) => r.id === form.categoryId);
      if (self) return self;
    }
    return roots[0];
  }, [roots, form.activeRootId, form.categoryId]);

  const leaves = activeRoot?.children?.length ? activeRoot.children : activeRoot ? [activeRoot] : [];

  const pickRoot = (id: number) => {
    const root = roots.find((r) => r.id === id);
    patch({ activeRootId: id, categoryId: root && !root.children?.length ? root.id : undefined });
  };

  const acctName = useMemo(
    () => new Map((accounts.data ?? []).map((a) => [a.id, a.name])),
    [accounts.data],
  );

  // 分类可读名：一级无子级取自身，否则「一级/子级」
  const catInfo = useMemo(() => {
    const m = new Map<number, string>();
    for (const w of forest.data ?? []) {
      for (const r of w.children ?? []) {
        if (r.children?.length) {
          for (const c of r.children) m.set(c.id, `${r.name}/${c.name}`);
        } else {
          m.set(r.id, r.name);
        }
      }
    }
    return m;
  }, [forest.data]);

  const openCreate = () => {
    setEditing(null);
    setForm(defaultForm());
    setOpen(true);
  };

  const openEdit = (r: RecurringRule) => {
    const loc = locate(forest.data, r.category_id);
    setEditing(r);
    setForm({
      kind: loc.kind,
      amount: r.amount,
      freq: r.freq,
      accountId: r.account_id,
      categoryId: r.category_id,
      activeRootId: loc.rootId,
      note: r.note,
      start: dayjs(r.start_date),
    });
    setOpen(true);
  };

  const validate = (): string | null => {
    if (!/^\d+(\.\d{1,2})?$/.test(form.amount) || Number(form.amount) <= 0) {
      return '请输入大于 0 的金额，最多两位小数';
    }
    if (!form.accountId) return '请选择账户';
    if (!form.categoryId) return '请选择分类';
    if (form.start.year() < 2000 || form.start.year() > 2100) {
      return '首次生成日期需在 2000-2100 年之间';
    }
    return null;
  };

  const submit = async () => {
    const err = validate();
    if (err) {
      message.warning(err);
      return;
    }
    const input = {
      amount: form.amount,
      freq: form.freq,
      account_id: form.accountId!,
      category_id: form.categoryId!,
      note: form.note || undefined,
      start_date: form.start.format('YYYY-MM-DD'),
    };
    try {
      if (editing) {
        await update.mutateAsync({ id: editing.id, input });
        message.success('已保存模板');
      } else {
        await create.mutateAsync(input);
        message.success('已创建周期模板');
      }
      setOpen(false);
    } catch (e) {
      message.error(errMsg(e));
    }
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

      <Card size="small" className="panel" title="模板" loading={rules.isPending}>
        {(rules.data ?? []).length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有周期模板" />
        ) : (
          <div className="rule-list" data-rule-count={rules.data!.length}>
            {rules.data!.map((r) => (
              <div className="rule-row" key={r.id} data-rule-id={r.id}>
                <div className="rule-top">
                  <span className="rule-amount">
                    <Money value={r.amount} />
                  </span>
                  <Tag>{FREQ_LABEL[r.freq]}</Tag>
                  <Tag className="rule-status" color={r.status === 'enabled' ? 'success' : undefined}>
                    {r.status === 'enabled' ? '启用' : '暂停'}
                  </Tag>
                </div>
                <div className="rule-meta">
                  {r.created_by_name ? `${r.created_by_name} · ` : ''}
                  {catInfo.get(r.category_id) ?? '分类已删除'} · {acctName.get(r.account_id) ?? '账户已删除'}
                  {r.note ? ` · ${r.note}` : ''}
                </div>
                <div className="rule-meta">
                  {dayjs(r.start_date).format('YYYY-MM-DD')} 起 · 下次生成{' '}
                  {dayjs(r.next_run_date).format('YYYY-MM-DD')}
                </div>
                <div className="rule-actions">
                  <Button
                    size="small"
                    icon={r.status === 'enabled' ? <PauseOutlined /> : <PlayCircleOutlined />}
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
              </div>
            ))}
          </div>
        )}
        <div className="muted small" style={{ marginTop: 12 }}>
          {isAdmin
            ? '显示全部模板（含各成员创建的，前缀为创建人）；'
            : '仅显示你自己创建的模板，他人代建的由管理员维护；'}
          到期流水由服务端每 5 分钟生成，打开应用也会自动补齐；生成的流水在「流水」页带「自动」
          标记，可单独修改或删除，不影响模板。暂停期间到期的期次会在重新启用后补齐。
        </div>
      </Card>

      <Modal
        open={open}
        onCancel={() => setOpen(false)}
        title={editing ? '编辑周期模板' : '新建周期模板'}
        width={isMobile ? '100%' : 520}
        style={isMobile ? { top: 0, maxWidth: '100%', paddingBottom: 0 } : undefined}
        footer={[
          <Button key="cancel" onClick={() => setOpen(false)}>
            取消
          </Button>,
          <Button
            key="ok"
            type="primary"
            loading={create.isPending || update.isPending}
            onClick={submit}
          >
            保存
          </Button>,
        ]}
      >
        <div className="txn-form">
          {/* 金额 */}
          <div className="amount-row">
            <span className="amount-symbol">¥</span>
            <Input
              value={form.amount}
              onChange={(e) => patch({ amount: e.target.value.replace(/[^\d.]/g, '') })}
              inputMode="decimal"
              placeholder="0.00"
              size="large"
              className="amount-input"
              autoFocus={!editing}
            />
          </div>

          {/* 频率 */}
          <Segmented
            block
            value={form.freq}
            options={FREQ_OPTIONS}
            onChange={(v) => patch({ freq: v as RuleFreq })}
          />

          {/* 收支方向：只用于筛选分类，生成方向由分类决定 */}
          <Segmented
            block
            value={form.kind}
            options={KIND_OPTIONS}
            onChange={(v) =>
              patch({ kind: v as CategoryKind, categoryId: undefined, activeRootId: undefined })
            }
          />

          {/* 分类 */}
          <div className="cat-pick">
            <div className="cat-roots">
              {roots.map((r) => (
                <Tag.CheckableTag
                  key={r.id}
                  checked={activeRoot?.id === r.id}
                  onChange={() => pickRoot(r.id)}
                >
                  {r.name}
                </Tag.CheckableTag>
              ))}
            </div>
            <div className="cat-leaves">
              {leaves.map((c) => (
                <Tag.CheckableTag
                  key={c.id}
                  checked={form.categoryId === c.id}
                  onChange={() => patch({ categoryId: c.id })}
                >
                  {c.name}
                </Tag.CheckableTag>
              ))}
            </div>
          </div>

          {/* 账户 */}
          <Select
            placeholder="账户"
            options={accountOptions}
            value={form.accountId}
            onChange={(v) => patch({ accountId: v })}
            style={{ width: '100%' }}
          />

          {/* 首次生成日期 */}
          <DatePicker
            value={form.start}
            onChange={(v) => v && patch({ start: v })}
            style={{ width: '100%' }}
            allowClear={false}
            placeholder="首次生成日期"
            disabledDate={(d) => d.year() < 2000 || d.year() > 2100}
          />

          <Input
            value={form.note}
            onChange={(e) => patch({ note: e.target.value })}
            placeholder="备注（可选），将写入生成的流水"
            maxLength={255}
          />

          <div className="rule-form-hint">
            频率与首次生成日期决定生成节奏（日期超前时到期即生成）；修改金额/账户/分类/备注只影响后续生成。
          </div>
        </div>
      </Modal>
    </div>
  );
}
