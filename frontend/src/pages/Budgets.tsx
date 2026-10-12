import { QueryBoundary } from '../components/QueryState';
import { useEffect, useRef, useState } from 'react';
import { App, Button, Card, DatePicker, InputNumber, Tag } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';

import { errMsg } from '../api/client';
import { useBudgets, useUpdateBudgets } from '../api/budgets';
import type { BudgetSetInput } from '../api/types';
import BudgetProgress from '../components/BudgetProgress';
import { useAuth } from '../stores/auth';
import { readApi } from '../api/request';
import type { BudgetView } from '../api/types';
import {
  budgetFields,
  receiveBudget,
  savedBudget,
  type BudgetDraft,
} from '../features/budgets/draft';

/**
 * 预算（specs/budgets，任务 8.3）：
 * - 查看对四档角色开放；配置仅管理员（输入框与保存按钮仅管理员可见）。
 * - 账期用月份选择器手动选择，键值以后端返回的 period_key 为准，不在客户端重算归期。
 * - 整单提交：留空/0 即不设预算；保存后立即生效，低于既有消耗会当场生成超支提醒。
 */
export default function Budgets() {
  const { message, modal } = App.useApp();
  const user = useAuth((s) => s.user);
  const isAdmin = user?.role === 'admin';

  const [month, setMonth] = useState<Dayjs | null>(null);
  const [pickerVersion, setPickerVersion] = useState(0);
  const view = useBudgets(month ? month.format('YYYY-MM') : undefined);
  const save = useUpdateBudgets();

  // 未手动选择时，用后端返回的当前账期回填选择器（period_key 只信后端）
  useEffect(() => {
    if (view.data && !month) setMonth(dayjs(view.data.period_key));
  }, [view.data, month]);

  // 选择账期切换 / 保存返回后，用服务端值回填表单
  const [draft, setDraft] = useState<BudgetDraft>({
    key: '',
    fields: {},
    dirty: false,
  });
  const form = draft.fields;
  const revision = useRef(0);
  const selectedKey = useRef('');
  const confirmingMonth = useRef(false);
  selectedKey.current = month?.format('YYYY-MM') ?? view.data?.period_key ?? '';
  useEffect(() => {
    if (!view.data) return;
    const snapshot = view.data;
    if (selectedKey.current !== snapshot.period_key) return;
    setDraft((current) =>
      receiveBudget(current, snapshot.period_key, budgetFields(snapshot))
    );
  }, [view.data]);
  const selectMonth = (next: Dayjs | null) => {
    if (
      !next ||
      next.format('YYYY-MM') === selectedKey.current ||
      confirmingMonth.current
    )
      return;
    const change = () => {
      confirmingMonth.current = false;
      revision.current++;
      setMonth(next);
    };
    if (draft.dirty) {
      confirmingMonth.current = true;
      modal.confirm({
        title: '放弃未保存的预算修改？',
        okText: '放弃并切换',
        cancelText: '保留修改',
        onOk: change,
        onCancel: () => {
          confirmingMonth.current = false;
          setPickerVersion((value) => value + 1);
        },
      });
    } else change();
  };

  const inputFor = (scope: 'total' | number) => {
    if (!isAdmin) return undefined;
    const key = String(scope);
    return (
      <InputNumber
        className="budget-input"
        min={0}
        max={9999999999.99}
        precision={2}
        disabled={save.isPending}
        placeholder="未设置"
        value={form[key] ? Number(form[key]) : null}
        onChange={(v) => {
          revision.current++;
          setDraft((previous) => ({
            ...previous,
            fields: { ...previous.fields, [key]: v === null ? '' : String(v) },
            dirty: true,
          }));
        }}
      />
    );
  };

  const doSave = async () => {
    if (!view.data || save.isPending) return;
    const key = view.data.period_key;
    const version = revision.current;
    const input: BudgetSetInput = {
      total: form.total || null,
      items: Object.entries(form)
        .filter(([k, v]) => k !== 'total' && v)
        .map(([k, v]) => ({ category_id: Number(k), amount: v })),
    };
    try {
      await save.mutateAsync({ periodKey: key, input });
      message.success('预算已保存');
    } catch (e) {
      message.error(errMsg(e));
      return;
    }
    try {
      const confirmed = await readApi<BudgetView>(`/budgets?period_key=${key}`);
      if (selectedKey.current === key && revision.current === version)
        setDraft((current) =>
          savedBudget(current, key, budgetFields(confirmed))
        );
    } catch {
      message.warning('预算已保存，最新配置读取失败，请重试更新');
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            BUDGET · {month ? month.format('YYYY.MM') : '本期'}
          </div>
          <h2>预算</h2>
        </div>
        <Tag color={isAdmin ? 'teal' : 'default'}>
          {isAdmin ? '可配置' : '仅查看'}
        </Tag>
      </div>

      <QueryBoundary query={view}>
        <Card
          size="small"
          className="panel panel--measure"
          title="预算配置"
          extra={
            <DatePicker
              key={pickerVersion}
              picker="month"
              value={month}
              onChange={selectMonth}
              allowClear={false}
              format="YYYY-MM"
            />
          }
          loading={view.isLoading}
        >
          {view.data && (
            <>
              <BudgetProgress view={view.data} showUnset inputFor={inputFor} />
              {isAdmin && (
                <div className="setting-row" style={{ marginTop: 14 }}>
                  <Button
                    type="primary"
                    loading={save.isPending}
                    onClick={() => void doSave()}
                  >
                    保存
                  </Button>
                  <span className="muted small">
                    留空或 0 表示不设预算；保存后立即生效
                  </span>
                  {draft.dirty && (
                    <span className="small muted">有未保存修改</span>
                  )}
                </div>
              )}
            </>
          )}
        </Card>
      </QueryBoundary>
    </div>
  );
}
