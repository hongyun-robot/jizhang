import AccountSelect from '../../../components/AccountSelect';
import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Alert, Button, Form, Input } from 'antd';
import dayjs from 'dayjs';
import ResponsiveSelect from '../../../components/ResponsiveSelect';
import Money from '../../../components/Money';
import DatePicker from '../../../components/DatePicker';

import type { FieldContext } from './context';
export default function RepaymentFields({
  context,
}: {
  context: FieldContext;
}) {
  const { assets, reason, allocationFields } = context;
  const advance = context.values?.scope === 'unbilled_ordinary';
  return (
    <>
      <Form.Item name="scope" label="还款范围">
        <ResponsiveSelect sheetTitle="还款范围" mobileSearch={false} options={[
          { value: 'issued', label: '还已出账' },
          { value: 'unbilled_ordinary', label: '提前还未出账' },
        ]} />
      </Form.Item>
      {advance && <>
        <p>未出账普通消费可还 <Money value={context.ordinaryAvailable} /></p>
        {(Number(context.d.summary.current_due) > 0 || Number(context.d.summary.overdue) > 0) &&
          <Alert type="info" title="本次只还未出账普通消费，不包含已出账或逾期欠款" />}
        {Number(context.ordinaryAvailable) === 0 && <p className="muted">
          当前日期没有可提前偿还的未出账普通消费。
          {context.d.plans.length > 0 && <a href={`/liabilities/${context.d.account.id}?plan=1`}>查看分期计划</a>}
        </p>}
      </>}
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
        <DatePicker getPopupContainer={popupInParent} disabledDate={advance ? (date) => date.isAfter(dayjs(), 'day') : undefined} />
      </Form.Item>
      {allocationFields}
      {advance && <>
        <Button onClick={context.loadOrdinaryPreview} loading={context.ordinaryPreviewing}>核对还款金额</Button>
        {context.ordinaryPreview && <p className="ordinary-repayment-confirmed">
          已核对本次还款 <Money value={context.ordinaryPreview.amount} />，保存后按以上消费分配。
        </p>}
      </>}
      {reason()}
    </>
  );
}
