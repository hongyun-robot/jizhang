import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Button, DatePicker, Form, Input, Select, Space } from 'antd';

import Money from '../../../components/Money';

import type { FieldContext } from './context';
export default function SettlementFields({
  context,
}: {
  context: FieldContext;
}) {
  const {
    assets,
    feeAccounts,
    categories,
    reason,
    preview,
    previewing,
    loadPreview,
  } = context;
  return (
    <>
      <Form.Item
        name="principal"
        label="平台实际剩余本金"
        rules={[{ required: true }, ...amountRules]}
      >
        <Input inputMode="decimal" />
      </Form.Item>
      <Form.Item
        name="from_account_id"
        label="本金还款账户"
        rules={[{ required: true }]}
      >
        <Select options={assets} getPopupContainer={popupInParent} />
      </Form.Item>
      <Space className="liability-form-row">
        <Form.Item
          name="fee_amount"
          label="实际结清手续费（手填）"
          rules={[{ required: true }, ...amountRules]}
        >
          <Input inputMode="decimal" />
        </Form.Item>
        <Form.Item
          name="fee_account_id"
          label="结清费支付账户"
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
      <Form.Item
        name="entry_date"
        label="实际结清日期"
        rules={[{ required: true }]}
      >
        <DatePicker getPopupContainer={popupInParent} />
      </Form.Item>
      {reason()}
      <Button onClick={loadPreview} loading={previewing}>
        核对结清预览
      </Button>
      {preview && (
        <div className="liability-preview">
          <p>
            已还本金 <Money value={preview.paid_principal} /> · 未还本金{' '}
            <Money value={preview.principal} /> · 已发生未付费用{' '}
            <Money value={preview.outstanding_fees} />
          </p>
          <p>
            使用待抵扣资金 <Money value={preview.pending_credit} /> · 本次转账{' '}
            <Money value={preview.transfer_amount} />
          </p>
          <p>
            另记结清手续费 <Money value={preview.fee_amount} /> ·
            取消未来未发生费用 <Money value={preview.cancelled_fees} />
          </p>
        </div>
      )}
    </>
  );
}
