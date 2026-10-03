import { useEffect, useState } from 'react';
import { App, Button, Card, DatePicker, InputNumber, Tag } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';

import { errMsg } from '../api/client';
import { useBudgets, useUpdateBudgets } from '../api/hooks';
import type { BudgetSetInput } from '../api/types';
import BudgetProgress from '../components/BudgetProgress';
import { useAuth } from '../stores/auth';

/**
 * 预算（specs/budgets，任务 8.3）：
 * - 查看对四档角色开放；配置仅管理员（输入框与保存按钮仅管理员可见）。
 * - 账期用月份选择器手动选择，键值以后端返回的 period_key 为准，不在客户端重算归期。
 * - 整单提交：留空/0 即不设预算；保存后立即生效，低于既有消耗会当场生成超支提醒。
 */
export default function Budgets() {
  const { message } = App.useApp();
  const user = useAuth((s) => s.user);
  const isAdmin = user?.role === 'admin';

  const [month, setMonth] = useState<Dayjs | null>(null);
  const view = useBudgets(month ? month.format('YYYY-MM') : undefined);
  const save = useUpdateBudgets();

  // 未手动选择时，用后端返回的当前账期回填选择器（period_key 只信后端）
  useEffect(() => {
    if (view.data && !month) setMonth(dayjs(view.data.period_key));
  }, [view.data, month]);

  // 选择账期切换 / 保存返回后，用服务端值回填表单
  const [form, setForm] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!view.data) return;
    const f: Record<string, string> = {};
    if (view.data.total.budget) f.total = view.data.total.budget;
    for (const c of view.data.categories) if (c.budget) f[String(c.category_id)] = c.budget;
    setForm(f);
  }, [view.data]);

  const inputFor = (scope: 'total' | number) => {
    if (!isAdmin) return undefined;
    const key = String(scope);
    return (
      <InputNumber
        className="budget-input"
        min={0}
        max={9999999999.99}
        precision={2}
        placeholder="未设置"
        value={form[key] ? Number(form[key]) : null}
        onChange={(v) => setForm((prev) => ({ ...prev, [key]: v === null ? '' : String(v) }))}
      />
    );
  };

  const doSave = async () => {
    if (!view.data) return;
    const input: BudgetSetInput = {
      total: form.total || null,
      items: Object.entries(form)
        .filter(([k, v]) => k !== 'total' && v)
        .map(([k, v]) => ({ category_id: Number(k), amount: v })),
    };
    try {
      await save.mutateAsync({ periodKey: view.data.period_key, input });
      message.success('预算已保存');
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">BUDGET · {month ? month.format('YYYY.MM') : '本期'}</div>
          <h2>预算</h2>
        </div>
        <Tag color={isAdmin ? 'teal' : 'default'}>{isAdmin ? '可配置' : '仅查看'}</Tag>
      </div>

      <Card
        size="small"
        className="panel panel--measure"
        title="预算配置"
        extra={
          <DatePicker
            picker="month"
            value={month}
            onChange={(v) => v && setMonth(v)}
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
                <Button type="primary" loading={save.isPending} onClick={() => void doSave()}>
                  保存
                </Button>
                <span className="muted small">留空或 0 表示不设预算；保存后立即生效</span>
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  );
}
