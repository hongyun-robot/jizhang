import type { ReactNode } from 'react';
import { Button, Space, Table, Tag } from 'antd';
import dayjs from 'dayjs';
import Money from '../../components/Money';
import LiabilityAmount from './LiabilityAmount';
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
        className="liability-plan-table"
        scroll={{ x: 600 }}
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
            title: '计划合计',
            render: (_, t) => (
              <LiabilityAmount total={sumMoney([t.principal, t.fee_amount])} fee={t.fee_amount} />
            ),
          },
          {
            title: '当前未还',
            render: (_, t) => {
              const fee = index.amounts.termRemainingFee(t.id, t.fee_txn_id);
              return <LiabilityAmount total={sumMoney([t.remaining, fee])} fee={fee} feeLabel="未还手续费" />;
            },
          },
          {
            title: '状态 / 收取日期',
            render: (_, t) => (
              <>
                {t.status === 'settled' && <Tag>已结清</Tag>}
                {t.status === 'cancelled' && <Tag>期次已取消</Tag>}
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
      <p className="small muted">
        计划合计含预估费用；已取消期次保留原计划记录。当前未还仅包含本金和已计入此负债账户的费用。
      </p>
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
        { title: '还款金额', render: (_, c) => <LiabilityAmount total={c.amount} fee={index.amounts.paymentFee(c.id)} feeLabel="已抵扣手续费" /> },
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
                {t && c.mode !== 'settlement' && !t.liability_operation_id && editable(t.created_by) && (
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
