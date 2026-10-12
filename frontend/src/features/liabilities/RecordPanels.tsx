import type { ReactNode } from 'react';
import { Button, Space, Table, Tag } from 'antd';
import dayjs from 'dayjs';
import Money from '../../components/Money';
import LiabilityHistory from '../../components/LiabilityHistory';
import type {
  LiabilityDetail,
  InstallmentPlan,
  LiabilityOperation,
} from '../../api/liability';
import type { SessionUser } from '../../domain/identity';
import type { LiabilityIntent } from './dialog';
import { liabilityIndexes } from './indexes';
import { cents, sumMoney } from '../../lib/money';

export interface RecordContext {
  d: LiabilityDetail;
  index: ReturnType<typeof liabilityIndexes>;
  user: SessionUser | null;
  editable: (owner: number) => boolean;
  open: (intent: LiabilityIntent) => void;
  sourceButton: (id: number) => ReactNode;
}

const opNames: Record<string, string> = {
  settings: '账单规则',
  posting: '资金流水',
  repayment: '还款',
  allocation: '还款分配',
  create_plan: '创建分期',
  adjust_plan: '调整期次',
  refund_adjustment: '退款核对',
  fee: '分期费用',
  settle: '提前结清',
  reverse: '撤销操作',
  reconcile: '账单核对',
};
export function PlanDetails({
  plan: p,
  context,
}: {
  plan: InstallmentPlan;
  context: RecordContext;
}) {
  const { index, editable, open, sourceButton } = context;
  const { bills } = index;
  return (
    <>
      <div className="liability-note">
        分期本金 <Money value={p.principal} /> · 未还本金{' '}
        <Money value={sumMoney(p.terms.map((t) => t.remaining))} /> ·{' '}
        {sourceButton(p.source_txn_id)}
      </div>
      <Table
        size="small"
        pagination={false}
        rowKey="id"
        dataSource={p.terms}
        scroll={{ x: 620 }}
        columns={[
          {
            title: '期次',
            dataIndex: 'number',
            render: (n: number) => `第 ${n} 期`,
          },
          {
            title: '归属账单',
            render: (_, t) => bills.get(t.statement_id)?.cycle_end ?? '—',
          },
          {
            title: '本金 / 未还',
            render: (_, t) => (
              <>
                <Money value={t.principal} />
                <div className="muted">
                  <Money value={t.remaining} />
                </div>
              </>
            ),
          },
          {
            title: '手续费',
            render: (_, t) => (
              <>
                <Money value={t.fee_amount} />
                <div className="small muted">
                  {t.fee_txn_id
                    ? '已发生'
                    : t.fee_status === 'cancelled'
                      ? '已取消'
                      : t.fee_status === 'blocked'
                        ? '待处理'
                        : cents(t.fee_amount) === 0n
                          ? '无费用'
                          : '预估'}{' '}
                  · {t.fee_date}
                </div>
                {t.blocked_reason && (
                  <span className="liability-problem">{t.blocked_reason}</span>
                )}
                {t.fee_txn_id && sourceButton(t.fee_txn_id)}
              </>
            ),
          },
        ]}
      />
      {editable(p.created_by) && p.status !== 'settled' && (
        <Space wrap className="liability-toolbar">
          <Button onClick={() => open({ mode: 'edit_plan', planId: p.id })}>
            {p.status === 'needs_reconcile'
              ? '核对退款后的期次'
              : '调整未来期次 / 费用'}
          </Button>
          <Button onClick={() => open({ mode: 'settle', planId: p.id })}>
            提前结清
          </Button>
        </Space>
      )}
    </>
  );
}

export function PaymentHistory({ context }: { context: RecordContext }) {
  const { d, index, sourceButton, editable, open } = context;
  const { bills, txns } = index;
  const incoming = d.credits.filter(
    (credit) => txns.get(credit.source_id)?.kind === 'transfer'
  );
  return (
    <Table
      rowKey="id"
      size="small"
      pagination={{ pageSize: 10 }}
      dataSource={incoming}
      scroll={{ x: 550 }}
      columns={[
        { title: '还款日期', dataIndex: 'entry_date' },
        { title: '还款金额', render: (_, c) => <Money value={c.amount} /> },
        { title: '待抵扣', render: (_, c) => <Money value={c.remaining} /> },
        {
          title: '分配',
          render: (_, c) => (
            <>
              {c.mode === 'auto'
                ? '按到期日'
                : c.mode === 'settlement'
                  ? '结清分配'
                  : c.mode === 'hold'
                    ? '待抵扣'
                    : '手动指定'}
              {(index.allocationsByCredit.get(c.id) ?? []).map((a) => (
                <div className="small muted" key={a.id}>
                  {
                    bills.get(
                      index.items.get(a.debt_item_id)?.statement_id ?? 0
                    )?.cycle_end
                  }{' '}
                  · <Money value={a.amount} />
                </div>
              ))}
            </>
          ),
        },
        {
          title: '操作',
          render: (_, c) => {
            const t = txns.get(c.source_id);
            return (
              <Space>
                {sourceButton(c.source_id)}
                {t && !t.liability_operation_id && editable(t.created_by) && (
                  <Button
                    size="small"
                    onClick={() => open({ mode: 'allocation', txnId: t.id })}
                  >
                    调整分配
                  </Button>
                )}
              </Space>
            );
          },
        },
      ]}
    />
  );
}

export function OperationHistory({ context }: { context: RecordContext }) {
  const { d, user, editable, open } = context;
  const { bills } = context.index;
  return (
    <Table
      rowKey="id"
      size="small"
      pagination={{ pageSize: 10 }}
      dataSource={d.operations}
      scroll={{ x: 600 }}
      columns={[
        {
          title: '时间',
          render: (_, o) => dayjs(o.created_at).format('MM-DD HH:mm'),
        },
        {
          title: '操作',
          render: (_, o) => (
            <>
              {opNames[o.kind] || o.kind}
              {o.reversed_at && <Tag>已撤销</Tag>}
            </>
          ),
        },
        {
          title: '操作人',
          render: (_, o) =>
            o.actor_id === 0
              ? '系统'
              : o.actor_id === user?.id
                ? '我'
                : `成员 ${o.actor_id}`,
        },
        { title: '说明', dataIndex: 'reason' },
        {
          title: '修正',
          render: (_, o: LiabilityOperation) =>
            ['settle', 'repayment'].includes(o.kind) &&
            !o.reversed_at &&
            editable(o.actor_id) ? (
              <Button
                size="small"
                onClick={() => open({ mode: 'reverse', operationId: o.id })}
              >
                撤销并恢复
              </Button>
            ) : null,
        },
      ]}
      expandable={{
        expandedRowRender: (o) => (
          <LiabilityHistory operation={o} bills={bills} />
        ),
      }}
    />
  );
}
