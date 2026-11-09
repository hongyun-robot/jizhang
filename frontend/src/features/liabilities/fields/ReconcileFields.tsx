import ResponsiveSelect from '../../../components/ResponsiveSelect';
import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Button, Checkbox, Form, Input, Space } from 'antd';

import Money from '../../../components/Money';
import { previewCents as cents, money, parseCents } from '../../../lib/money';
import { reconciledBillAmount } from '../amounts';
import type { FieldContext } from './context';
export default function ReconcileFields({
  context,
}: {
  context: FieldContext;
}) {
  const { d, bill, values, bills, reason, onAddExpense, editable } = context;
  const adjustedAmount = reconciledBillAmount(d, bill?.id ?? 0, values?.moves);
  return (
    <>
      <p>
        系统账单 <Money value={bill?.amount} /> · 原出账快照{' '}
        <Money value={bill?.snapshot_amount} />
      </p>
      {!!values?.moves?.length && (
        <p aria-live="polite">
          调整后系统账单 <Money value={adjustedAmount} />
        </p>
      )}
      <Form.Item
        name="reference_amount"
        label="平台账单参考金额（仅用于核对）"
        extra="参考金额用于核对调整后的本账单；仅调整归属可取消勾选“标记核对完成”。"
        dependencies={['moves', 'complete']}
        rules={[
          ...amountRules,
          ({ getFieldValue }) => ({
            validator: (_rule, value: unknown) => {
              const reference = parseCents(String(value ?? ''));
              const amount = reconciledBillAmount(d, bill?.id ?? 0, getFieldValue('moves'));
              return !getFieldValue('complete') || reference === null || reference === cents(amount)
                ? Promise.resolve()
                : Promise.reject(new Error(`调整后本账单金额为 ¥${amount}，请核对参考金额，或取消勾选“标记核对完成”`));
            },
          }),
        ]}
      >
        <Input inputMode="decimal" />
      </Form.Item>
      {values?.reference_amount && (
        <p>
          {values?.moves?.length ? '与调整后系统差异' : '与系统差异'}{' '}
          <Money
            value={money(cents(values.reference_amount) - cents(adjustedAmount))}
          />
        </p>
      )}
      <Space wrap>
        <Button onClick={onAddExpense}>补记实际消费 / 费用</Button>
        <span className="small muted">
          记录真实金额与日期，必要时在下面调整账单归属。
        </span>
      </Space>
      {!!values?.moves?.length && (
        <p className="small muted">
          调整预览：
          {values.moves
            .map((m) => {
              const item = d.items.find((i) => i.id === m.item_id);
              return item
                ? `¥${item.amount}：${d.statements.find((b) => b.id === item.statement_id)?.cycle_end} → ${d.statements.find((b) => b.id === m.statement_id)?.cycle_end ?? '请选择账单'}`
                : '';
            })
            .filter(Boolean)
            .join('；')}
          。消费日期、余额和收支账期保持不变。
        </p>
      )}
      <Form.List name="moves">
        {(fields, { add, remove }) => (
          <>
            {fields.map((f) => (
              <Space
                key={f.key}
                align="baseline"
                className="liability-form-row"
              >
                <Form.Item
                  name={[f.name, 'item_id']}
                  rules={[{ required: true }]}
                >
                  <ResponsiveSelect
                    sheetTitle="消费"
                    placeholder="选择消费"
                    options={d.items
                      .filter(
                        (i) =>
                          !i.plan_id &&
                          editable(
                            d.transactions.find((t) => t.id === i.source_id)
                              ?.created_by ?? -1
                          )
                      )
                      .map((i) => ({
                        value: i.id,
                        label: `${d.transactions.find((t) => t.id === i.source_id)?.note || '消费'} · ¥${i.amount}`,
                      }))}
                    style={{ minWidth: 180 }}
                    getPopupContainer={popupInParent}
                  />
                </Form.Item>
                <Form.Item
                  name={[f.name, 'statement_id']}
                  rules={[{ required: true }]}
                >
                  <ResponsiveSelect
                    sheetTitle="实际归属账单"
                    placeholder="实际归属账单"
                    options={bills.filter(
                      (b) =>
                        d.statements.find((x) => x.id === b.value)?.kind ===
                        'monthly'
                    )}
                    style={{ minWidth: 190 }}
                    getPopupContainer={popupInParent}
                  />
                </Form.Item>
                <Button onClick={() => remove(f.name)}>移除</Button>
              </Space>
            ))}
            <Button className="liability-toolbar" onClick={() => add({})}>
              调整消费归属
            </Button>
          </>
        )}
      </Form.List>
      {reason((values?.moves?.length ?? 0) > 0)}
      <Form.Item name="complete" valuePropName="checked">
        <Checkbox>差异已解决，标记核对完成</Checkbox>
      </Form.Item>
      <p className="small muted">
        核对不直接覆盖应还金额，也不改变消费日期或收支账期。
      </p>
    </>
  );
}
