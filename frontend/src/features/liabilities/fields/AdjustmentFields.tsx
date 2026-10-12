import ResponsiveSelect from '../../../components/ResponsiveSelect';
import AccountSelect from '../../../components/AccountSelect';
import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Alert, DatePicker, Form, Input, InputNumber, Space } from 'antd';
import dayjs from 'dayjs';
import Money from '../../../components/Money';
import { previewSum as sumMoney } from '../../../lib/money';
import type { FieldContext } from './context';
export default function AdjustmentFields({
  context,
}: {
  context: FieldContext;
}) {
  const { d, plan, values, form, bills, reason, editable, feeCandidates } =
    context;
  if (!plan) return null;
  return (
    <>
      <Form.Item name="fee_account_id" label="后续费用支付账户" rules={[{ required: true }]}>
        <AccountSelect sheetTitle="后续费用支付账户" assetOnly options={context.feeAccounts} onCreatingChange={context.setCreatingAccount} getPopupContainer={popupInParent} />
      </Form.Item>
      <Form.Item name="fee_category_id" label="后续手续费分类" rules={[{ required: true }]}>
        <ResponsiveSelect sheetTitle="后续手续费分类" options={context.categories} getPopupContainer={popupInParent} />
      </Form.Item>
      {plan.status === 'needs_reconcile' && (
        <Form.Item
          name="refund_txn_id"
          label="已到账的本金退款记录"
          rules={[{ required: true }]}
        >
          <ResponsiveSelect
            sheetTitle="已到账的本金退款记录"
            options={d.transactions
              .filter(
                (t) =>
                  t.kind === 'refund' && t.related_txn_id === plan.source_txn_id
              )
              .map((t) => ({
                value: t.id,
                label: `${dayjs(t.entry_date).format('YYYY-MM-DD')} · ¥${t.amount}`,
              }))}
            getPopupContainer={popupInParent}
          />
        </Form.Item>
      )}
      <Alert
        type="info"
        title="以平台实际剩余计划为依据"
        description="各期未还本金合计须保持真实金额；已发生费用请通过实际退款或合规修正处理。"
      />
      <Form.List name="terms">
        {(fields) => (
          <>
            {fields.map((f) => {
              const old = plan.terms[f.name];
              const issued = !!d.statements.find(
                (b) => b.id === old.statement_id
              )?.issued_at;
              const canChangePrincipal =
                !issued || plan.status === 'needs_reconcile';
              return (
                <div key={f.key} className="liability-term-edit">
                  <h4>
                    第 {old.number} 期
                    {old.blocked_reason && (
                      <span className="liability-problem">
                        {' '}
                        · {old.blocked_reason}
                      </span>
                    )}
                  </h4>
                  <Form.Item name={[f.name, 'id']} hidden>
                    <InputNumber />
                  </Form.Item>
                  <Space className="liability-form-row">
                    <Form.Item
                      name={[f.name, 'remaining_principal']}
                      label="未还本金"
                      rules={amountRules}
                    >
                      <Input
                        inputMode="decimal"
                        disabled={!canChangePrincipal}
                      />
                    </Form.Item>
                    <Form.Item name={[f.name, 'statement_id']} label="归属账单">
                      <ResponsiveSelect
                        sheetTitle="归属账单"
                        options={bills}
                        disabled={issued}
                        style={{ minWidth: 210 }}
                        getPopupContainer={popupInParent}
                      />
                    </Form.Item>
                  </Space>
                  <Space className="liability-form-row">
                    <Form.Item
                      name={[f.name, 'fee_amount']}
                      label="手续费"
                      rules={amountRules}
                    >
                      <Input inputMode="decimal" disabled={!!old.fee_txn_id} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'fee_date']} label="实际收取日期">
                      <DatePicker
                        disabled={!!old.fee_txn_id}
                        getPopupContainer={popupInParent}
                      />
                    </Form.Item>
                  </Space>
                  {!old.fee_txn_id && (
                    <Form.Item
                      name={[f.name, 'fee_txn_id']}
                      label="如已手工记账，关联已有费用流水"
                    >
                      <ResponsiveSelect
                        sheetTitle="已有费用流水"
                        allowClear
                        onChange={(id: number | undefined) => {
                          const t = (
                            feeCandidates.data?.items ?? d.transactions
                          ).find((x) => x.id === id);
                          if (t) {
                            form.setFieldValue(
                              ['terms', f.name, 'fee_amount'],
                              t.amount
                            );
                            form.setFieldValue(
                              ['terms', f.name, 'fee_date'],
                              dayjs(t.entry_date)
                            );
                          }
                        }}
                        options={(feeCandidates.data?.items ?? d.transactions)
                          .filter(
                            (t) =>
                              t.kind === 'expense' &&
                              !t.installment_plan_id &&
                              !t.liability_term_id &&
                              !t.liability_operation_id &&
                              t.account_id === plan.fee_account_id &&
                              editable(t.created_by)
                          )
                          .map((t) => ({
                            value: t.id,
                            label: `${dayjs(t.entry_date).format('YYYY-MM-DD')} · ¥${t.amount} · ${t.note || '支出'}`,
                          }))}
                        getPopupContainer={popupInParent}
                      />
                    </Form.Item>
                  )}
                </div>
              );
            })}
          </>
        )}
      </Form.List>
      <p>
        当前待还本金合计{' '}
        <Money value={sumMoney(plan.terms.map((t) => t.remaining))} /> · 调整后{' '}
        <Money
          value={sumMoney(
            (values?.terms ?? []).map((t) => t.remaining_principal ?? '0')
          )}
        />
      </p>
      {reason(true)}
    </>
  );
}
