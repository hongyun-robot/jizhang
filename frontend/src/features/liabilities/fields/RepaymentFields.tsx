import AccountSelect from '../../../components/AccountSelect';
import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Form, Input } from 'antd';
import DatePicker from '../../../components/DatePicker';

import type { FieldContext } from './context';
export default function RepaymentFields({
  context,
}: {
  context: FieldContext;
}) {
  const { assets, reason, allocationFields } = context;
  return (
    <>
      <Form.Item
        name="from_account_id"
        label="还款资金账户"
        rules={[{ required: true }]}
      >
        <AccountSelect
          assetOnly
            onCreatingChange={context.setCreatingAccount}
            sheetTitle="还款资金账户"
          options={assets}
          getPopupContainer={popupInParent}
        />
      </Form.Item>
      <Form.Item
        name="amount"
        label="还款金额"
        rules={[{ required: true }, ...amountRules]}
      >
        <Input inputMode="decimal" />
      </Form.Item>
      <Form.Item
        name="entry_date"
        label="实际还款日期"
        rules={[{ required: true }]}
      >
        <DatePicker getPopupContainer={popupInParent} />
      </Form.Item>
      {allocationFields}
      {reason()}
    </>
  );
}
