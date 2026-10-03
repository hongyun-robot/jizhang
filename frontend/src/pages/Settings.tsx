import { useState } from 'react';
import { Alert, App, Button, Card, DatePicker, Descriptions, InputNumber, Popconfirm, Spin, Tag } from 'antd';
import { LockOutlined, UnlockOutlined } from '@ant-design/icons';
import dayjs, { type Dayjs } from 'dayjs';

import { errMsg } from '../api/client';
import { useClosingMutations, useClosingPreview, useSettings, useUpdateSettings } from '../api/hooks';
import Money from '../components/Money';

/**
 * 账期设置（仅管理员，specs/billing-periods）：
 * - 结算日 1–28（缺省 1 = 自然月）；修改仅影响今日之后新记流水的归期，不回溯历史。
 * - 封账：手动选择已结束账期，确认前展示该期收入/支出/结余；封后硬锁，可解封。
 * - 审计日志独立页（/audit），封账与解封两步均留痕。
 */
export default function Settings() {
  const { message, modal } = App.useApp();
  const settings = useSettings();
  const update = useUpdateSettings();
  const { close, reopen } = useClosingMutations();

  const [day, setDay] = useState<number | null>(null);
  const currentDay = settings.data?.settlement_day ?? 1;
  const editDay = day ?? currentDay;

  // 手动选择账期：默认上个自然月（具体区间/口径一律以后端预览为准）
  const [month, setMonth] = useState<Dayjs>(() => dayjs().subtract(1, 'month'));
  const periodKey = month.format('YYYY-MM');
  const preview = useClosingPreview(periodKey);
  const p = preview.data;

  const saveDay = async () => {
    try {
      await update.mutateAsync(editDay);
      message.success('结算日已保存，仅影响今日之后的流水');
      setDay(null);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const askClose = () => {
    if (!p) return;
    modal.confirm({
      title: `封账 ${p.period_key}？`,
      width: 420,
      content: (
        <div>
          <div className="muted small">
            {p.start_date} ~ {p.end_date}
          </div>
          <Descriptions
            size="small"
            column={3}
            style={{ marginTop: 8 }}
            items={[
              { key: 'i', label: '收入', children: <Money value={p.income} tone="income" /> },
              { key: 'e', label: '支出', children: <Money value={p.expense} tone="expense" /> },
              {
                key: 'b',
                label: '结余',
                children: (
                  <Money value={p.balance} tone={Number(p.balance) < 0 ? 'expense' : 'income'} />
                ),
              },
            ]}
          />
          <div className="muted small" style={{ marginTop: 8 }}>
            封账后该期内流水不可增改删（任何角色），解封后恢复。
          </div>
        </div>
      ),
      okText: '确认封账',
      cancelText: '取消',
      onOk: async () => {
        try {
          await close.mutateAsync(p.period_key);
          message.success(`${p.period_key} 已封账`);
        } catch (e) {
          message.error(errMsg(e));
        }
      },
    });
  };

  const doReopen = async (key: string) => {
    try {
      await reopen.mutateAsync(key);
      message.success(`${key} 已解封`);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h2>账期设置</h2>
      </div>

      {/* 结算日：改日提示“仅影响今日之后的流水” */}
      <Card size="small" className="panel" title="结算日" loading={settings.isLoading}>
        <div className="setting-row">
          <InputNumber
            min={1}
            max={28}
            value={editDay}
            onChange={(v) => setDay(v)}
            addonAfter="日"
            style={{ width: 132 }}
          />
          <Button
            type="primary"
            loading={update.isPending}
            disabled={editDay === currentDay}
            onClick={() => void saveDay()}
          >
            保存
          </Button>
        </div>
        <Alert
          type="info"
          showIcon
          style={{ marginTop: 10 }}
          message="账期按结算日（1–28 日）划分，1 日为自然月。修改仅影响今日之后的流水归期，历史流水与已封账账期保持不变。"
        />
      </Card>

      {/* 封账：手动选择已结束账期，确认前展示收支结余 */}
      <Card size="small" className="panel" title="封账管理">
        <div className="setting-row">
          <DatePicker
            picker="month"
            value={month}
            onChange={(v) => v && setMonth(v)}
            allowClear={false}
            format="YYYY-MM"
          />
          {p?.closed && <Tag color="red">已封账</Tag>}
          {p && !p.closed && (p.ended ? <Tag color="green">已结束</Tag> : <Tag>进行中</Tag>)}
        </div>

        {preview.isLoading ? (
          <div style={{ padding: '12px 0' }}>
            <Spin size="small" />
          </div>
        ) : p ? (
          <>
            <div className="muted small" style={{ margin: '8px 0' }}>
              账期区间 {p.start_date} ~ {p.end_date}
            </div>
            <div className="stat-grid three">
              <Card size="small" className="panel stat-card">
                <div className="stat-label">收入</div>
                <Money value={p.income} tone="income" />
              </Card>
              <Card size="small" className="panel stat-card">
                <div className="stat-label">支出</div>
                <Money value={p.expense} tone="expense" />
              </Card>
              <Card size="small" className="panel stat-card">
                <div className="stat-label">结余</div>
                <Money value={p.balance} tone={Number(p.balance) < 0 ? 'expense' : 'income'} />
              </Card>
            </div>
            <div className="setting-row" style={{ marginTop: 12 }}>
              {p.closed ? (
                <Popconfirm
                  title={`解封 ${p.period_key}？`}
                  description="解封后该期流水恢复可增改删"
                  okText="解封"
                  cancelText="取消"
                  onConfirm={() => void doReopen(p.period_key)}
                >
                  <Button icon={<UnlockOutlined />}>解封</Button>
                </Popconfirm>
              ) : p.ended ? (
                <Button type="primary" icon={<LockOutlined />} onClick={askClose}>
                  封账
                </Button>
              ) : (
                <Alert type="warning" showIcon message="该账期尚未结束，结束后才能封账" />
              )}
            </div>
          </>
        ) : null}
      </Card>
    </div>
  );
}
