import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import {
  Alert,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
} from 'antd';
import dayjs from 'dayjs';
import Money from '../../../components/Money';
import {
  previewCents as cents,
  previewSum as sumMoney,
} from '../../../lib/money';
import type { FieldContext } from './context';
export default function InstallmentFields({
  context,
}: {
  context: FieldContext;
}) {
  const {
    form,
    feeAccounts,
    categories,
    availableExpenses,
    sourceExpense,
    originalRemaining,
    feeMode,
    feeParts,
    schedule,
    setFeeDates,
    reason,
  } = context;
  return (
    <>
      <Form.Item name="source_txn_id" label="消费来源">
        <Select
          allowClear
          placeholder="新消费（记录一次本金支出）"
          options={availableExpenses.map((t) => ({
            value: t.id,
            label: `${dayjs(t.entry_date).format('MM-DD')} ${t.note || '消费'} · ¥${t.amount}`,
          }))}
          onChange={(id: number | undefined) => {
            const t = availableExpenses.find((x) => x.id === id);
            if (t)
              form.setFieldsValue({
                entry_date: dayjs(t.entry_date),
                first_statement_date: dayjs(t.entry_date),
                category_id: t.category_id ?? undefined,
              });
          }}
          getPopupContainer={popupInParent}
        />
      </Form.Item>
      {sourceExpense ? (
        <Alert
          type="info"
          title={
            <>
              原消费 <Money value={sourceExpense.amount} />
              ，本次分期未还本金 <Money value={originalRemaining} />
            </>
          }
          description="已还款或已退款部分保留在历史记录中；请填写本金核对说明。"
        />
      ) : (
        <>
          <Form.Item
            name="amount"
            label="消费本金"
            rules={[{ required: true }, ...amountRules]}
          >
            <Input inputMode="decimal" />
          </Form.Item>
          <Space className="liability-form-row">
            <Form.Item
              name="entry_date"
              label="实际消费日期"
              rules={[{ required: true }]}
            >
              <DatePicker getPopupContainer={popupInParent} />
            </Form.Item>
            <Form.Item
              name="category_id"
              label="消费分类"
              rules={[{ required: true }]}
            >
              <Select
                options={categories}
                style={{ minWidth: 190 }}
                getPopupContainer={popupInParent}
              />
            </Form.Item>
          </Space>
          <Form.Item name="note" label="消费备注">
            <Input maxLength={255} />
          </Form.Item>
          <Form.Item name="platform" label="平台 / 商家">
            <Input maxLength={128} />
          </Form.Item>
        </>
      )}
      <Space className="liability-form-row">
        <Form.Item
          name="term_count"
          label="分期期数"
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={120} />
        </Form.Item>
        <Form.Item
          name="first_statement_date"
          label="首期归属日期"
          rules={[{ required: true }]}
        >
          <DatePicker getPopupContainer={popupInParent} />
        </Form.Item>
      </Space>
      <Form.Item name="fee_mode" label="手填手续费方式">
        <Select
          options={[
            { value: 'per_term', label: '每期固定手续费' },
            { value: 'total_spread', label: '总手续费按期分摊' },
            { value: 'upfront', label: '一次性收取' },
          ]}
          getPopupContainer={popupInParent}
        />
      </Form.Item>
      <Space className="liability-form-row">
        <Form.Item
          name="fee_input"
          label={feeMode === 'per_term' ? '每期手续费' : '总手续费'}
          rules={[{ required: true }, ...amountRules]}
        >
          <Input inputMode="decimal" />
        </Form.Item>
        <Form.Item
          name="fee_account_id"
          label="费用支付账户"
          rules={[{ required: true }]}
        >
          <Select
            options={feeAccounts}
            style={{ minWidth: 190 }}
            getPopupContainer={popupInParent}
          />
        </Form.Item>
      </Space>
      <Form.Item
        name="fee_category_id"
        label="手续费支出分类"
        rules={[{ required: true }]}
      >
        <Select options={categories} getPopupContainer={popupInParent} />
      </Form.Item>
      {feeMode === 'upfront' && (
        <Form.Item
          name="fee_date"
          label="一次性实际收取日期"
          rules={[{ required: true }]}
        >
          <DatePicker getPopupContainer={popupInParent} />
        </Form.Item>
      )}
      <p>
        本金合计 <Money value={originalRemaining} /> · 手续费预估合计{' '}
        <Money value={sumMoney(feeParts)} />
      </p>
      <Table
        size="small"
        pagination={false}
        scroll={{ x: 580 }}
        dataSource={schedule}
        columns={[
          { title: '期次', dataIndex: 'number' },
          {
            title: '出账 / 到期',
            render: (_, r) => (
              <>
                {r.close}
                <div className="small muted">{r.due}</div>
              </>
            ),
          },
          { title: '本金', render: (_, r) => <Money value={r.amount} /> },
          { title: '手续费', render: (_, r) => <Money value={r.fee} /> },
          {
            title: '收取日期',
            render: (_, r) =>
              feeMode === 'upfront' ? (
                r.feeDate
              ) : (
                <DatePicker
                  value={r.feeDate ? dayjs(r.feeDate) : null}
                  allowClear={false}
                  onChange={(date) => {
                    if (date)
                      setFeeDates((dates) => ({
                        ...dates,
                        [r.key]: date.format('YYYY-MM-DD'),
                      }));
                  }}
                  getPopupContainer={popupInParent}
                />
              ),
          },
        ]}
      />
      {reason(
        !!sourceExpense &&
          cents(sourceExpense.amount) !== cents(originalRemaining)
      )}
      <p className="small muted">
        本金支出只记录一次。手续费按所填实际日期生成独立支出；未来费用先作预估。
      </p>
    </>
  );
}
