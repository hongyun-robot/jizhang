import { historySnapshot } from '../features/liabilities/history';
import type { LiabilityBill, LiabilityOperation } from '../api/liability';
import Money from './Money';

export default function LiabilityHistory({
  operation: o,
  bills,
}: {
  operation: LiabilityOperation;
  bills: Map<number, LiabilityBill>;
}) {
  try {
    const before = historySnapshot(o.before_json);
    const after = historySnapshot(o.result_json);
    if (o.kind === 'posting')
      return (
        <span>
          {before?.amount && (
            <>
              原金额 <Money value={before.amount} /> ·{' '}
            </>
          )}
          {after?.amount ? (
            <>
              现金额 <Money value={after.amount} /> ·{' '}
              {after.entry_date?.slice(0, 10)}
            </>
          ) : (
            '流水已删除，原记录保留'
          )}
        </span>
      );
    if (o.kind === 'settings' && after.rule) {
      const rule = after.rule;
      return (
        <div>
          <p>
            自 {rule.effective_from} 生效 · 每月 {rule.statement_day} 日出账，
            {rule.repayment_month_offset ? '次月' : '当月'} {rule.repayment_day}{' '}
            日还款
          </p>
          {after.statements.map((b) => {
            const old = before.statements.find((x) => x.id === b.id);
            return (
              <p key={b.id}>
                未出账期次：{old?.cycle_end ?? '新增'} → {b.cycle_end}，到期{' '}
                {old?.due_date ?? '—'} → {b.due_date}
              </p>
            );
          })}
        </div>
      );
    }
    if (o.kind === 'reconcile' && after.assignments?.length)
      return (
        <div>
          {after.assignments.map((a) => (
            <p key={a.item_id}>
              流水 #{a.source_id} · <Money value={a.amount} /> ·{' '}
              {bills.get(a.from_statement_id)?.cycle_end ?? a.from_statement_id}{' '}
              → {bills.get(a.to_statement_id)?.cycle_end ?? a.to_statement_id}
            </p>
          ))}
          <span className="small muted">消费日期和收支账期保持原记录。</span>
        </div>
      );
    if (after.plan_snapshot) {
      const plan = after.plan_snapshot;
      const old = before.plans?.find((p) => p.id === plan.id);
      return (
        <div>
          <p>
            分期 {plan.id} · 版本 {old?.version ?? '新建'} → {plan.version}
          </p>
          {plan.terms.map((term) => {
            const prior = old?.terms.find((t) => t.id === term.id);
            return (
              <p key={term.id}>
                第 {term.number} 期 · 本金{' '}
                {prior && (
                  <>
                    <Money value={prior.principal} /> →{' '}
                  </>
                )}
                <Money value={term.principal} /> · 费用{' '}
                {prior && (
                  <>
                    <Money value={prior.fee_amount} /> →{' '}
                  </>
                )}
                <Money value={term.fee_amount} /> · {term.fee_date}
                {term.fee_status === 'cancelled' && ' · 已取消'}
              </p>
            );
          })}
        </div>
      );
    }
    return (
      <span className="muted">
        {o.reason || '操作已保存'}
        {o.plan_id ? ` · 分期 ${o.plan_id}` : ''}
      </span>
    );
  } catch {
    return <span className="muted">{o.reason || '历史记录'}</span>;
  }
}
