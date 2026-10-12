import SelectionScope from '../../components/SelectionScope';
import AccountSelect from '../../components/AccountSelect';
import { submissionGate } from '../../lib/submission';
import { useEditorSession } from '../../hooks/useEditorSession';
import { useRef as submissionRef } from 'react';
import { useState } from 'react';

import CategoryPicker from '../../components/CategoryPicker';
import { accountOptions as buildAccountOptions } from '../../domain/accounts';
import { amountError } from '../../lib/money';

import { useEffect, useMemo, useRef } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import { App, Button, DatePicker, Grid, Input, Modal, Segmented } from 'antd';

import { errMsg } from '../../api/client';
import { rootsOfKind } from '../../api/category';
import { useAccounts } from '../../api/accounts';
import { useCategoryForest } from '../../api/categories';
import { useRuleMutations } from '../../api/recurring';
import { useVisualViewportKeyboard } from '../../hooks/useVisualViewportKeyboard';
import type {
  CategoryForest,
  CategoryKind,
  RecurringRule,
  RuleFreq,
} from '../../api/types';
import { useAuth } from '../../stores/auth';
import { popupInParent } from '../../lib/popup';

const FREQ_OPTIONS = [
  { value: 'daily', label: '每日' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'yearly', label: '每年' },
];
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
function locate(
  forest: CategoryForest | undefined,
  catId: number
): {
  kind?: CategoryKind;
  rootId?: number;
} {
  for (const wrapper of forest ?? []) {
    for (const root of wrapper.children ?? []) {
      if (root.id === catId) return { kind: root.kind, rootId: root.id };
      if (root.children?.some((c) => c.id === catId))
        return { kind: root.kind, rootId: root.id };
    }
  }
  return {};
}
const defaultForm = (): FormState => ({
  kind: 'expense',
  amount: '',
  freq: 'monthly',
  note: '',
  start: dayjs(),
});
export default function RecurringFormModal({
  open,
  editing,
  onClose,
}: {
  open: boolean;
  editing: RecurringRule | null;
  onClose: () => void;
}) {
  const submitGate = submissionRef(submissionGate());
  const [creatingAccount, setCreatingAccount] = useState(false);
  const editor = useEditorSession(open, editing?.id ?? 'new');
  const close = () => {
    editor.close();
    onClose();
  };

  const { create, update } = useRuleMutations();
  const [form, setForm] = useState<FormState>(defaultForm);
  useVisualViewportKeyboard(open);
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const user = useAuth((s) => s.user);
  const accounts = useAccounts();
  const forest = useCategoryForest();
  const kindTouched = useRef(false);
  const patch = (p: Partial<FormState>) => {
    if (p.kind !== undefined || p.categoryId !== undefined)
      kindTouched.current = true;
    setForm((f) => ({ ...f, ...p }));
  };
  const accountOptions = buildAccountOptions(accounts.data, {
    ownerId: user?.id,
    history: !!editing,
    selectedId: form.accountId,
  });
  const roots = useMemo(
    () => rootsOfKind(forest.data, form.kind),
    [forest.data, form.kind]
  );
  const validate = (): string | null => {
    if (amountError(form.amount)) {
      return '请输入大于 0 的金额，最多两位小数';
    }
    if (!form.accountId) return '请选择账户';
    if (!form.categoryId) return '请选择分类';
    if (form.start.year() < 2000 || form.start.year() > 2100) {
      return '首次生成日期需在 2000-2100 年之间';
    }
    return null;
  };
  const submit = () =>
    submitGate.current(async () => {
      if (creatingAccount) return;
      const isCurrent = editor.capture();
      if (!isCurrent()) return;
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
          if (!isCurrent()) return;
          message.success('已保存模板');
        } else {
          await create.mutateAsync(input);
          if (!isCurrent()) return;
          message.success('已创建周期模板');
        }
        close();
      } catch (e) {
        if (isCurrent()) message.error(errMsg(e));
      }
    });
  const initialized = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!open) {
      initialized.current = undefined;
      return;
    }
    const key = editing?.id ?? 0;
    if (initialized.current === key) {
      if (editing && !kindTouched.current) {
        const selected = locate(forest.data, editing.category_id);
        if (selected.kind)
          setForm((current) => ({ ...current, kind: selected.kind! }));
      }
      return;
    }
    initialized.current = key;
    kindTouched.current = false;
    const selected = editing
      ? locate(forest.data, editing.category_id)
      : undefined;
    setForm(
      editing
        ? {
            kind: selected?.kind ?? 'expense',
            amount: editing.amount,
            freq: editing.freq,
            accountId: editing.account_id || undefined,
            categoryId: editing.category_id,
            note: editing.note,
            start: dayjs(editing.start_date),
          }
        : defaultForm()
    );
  }, [open, editing, forest.data]);

  return (
    <SelectionScope active={open}>
      <Modal
        open={open}
        onCancel={close}
        title={editing ? '编辑周期模板' : '新建周期模板'}
        width={isMobile ? '100%' : 520}
        style={
          isMobile ? { top: 0, maxWidth: '100%', paddingBottom: 0 } : undefined
        }
        footer={[
          <Button key="cancel" onClick={close}>
            取消
          </Button>,
          <Button
            key="ok"
            type="primary"
            disabled={creatingAccount}
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
              onChange={(e) =>
                patch({ amount: e.target.value.replace(/[^\d.]/g, '') })
              }
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
              patch({
                kind: v as CategoryKind,
                categoryId: undefined,
                activeRootId: undefined,
              })
            }
          />

          {/* 分类 */}
          <CategoryPicker
            key={String(open) + String(editing?.id) + form.kind}
            roots={roots}
            value={form.categoryId}
            onChange={(categoryId) => patch({ categoryId })}
            heading="分类"
          />

          {/* 账户 */}
          <AccountSelect
            onCreatingChange={setCreatingAccount}
            sheetTitle="账户"
            active={open}
            placeholder="账户"
            options={accountOptions}
            value={form.accountId}
            onChange={(v) => patch({ accountId: v })}
            style={{ width: '100%' }}
            getPopupContainer={popupInParent}
          />

          {/* 首次生成日期 */}
          <DatePicker
            value={form.start}
            onChange={(v) => v && patch({ start: v })}
            style={{ width: '100%' }}
            allowClear={false}
            placeholder="首次生成日期"
            disabledDate={(d) => d.year() < 2000 || d.year() > 2100}
            getPopupContainer={popupInParent}
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
    </SelectionScope>
  );
}
