import AccountSelect from '../../../components/AccountSelect';
import { popupInParent } from '../../../lib/popup';
import { amountRules } from './amountRules';
import { Alert, Button, Form, Input } from 'antd';
import dayjs from 'dayjs';
import ResponsiveSelect from '../../../components/ResponsiveSelect';
import Money from '../../../components/Money';
import DatePicker from '../../../components/DatePicker';
import { cents, sumMoney } from '../../../lib/money';
import { combinedCapacity } from '../ordinary';
import LiabilityAmount from '../LiabilityAmount';

import type { FieldContext } from './context';
export default function RepaymentFields({
  context,
}: {
  context: FieldContext;
}) {
  const { assets, reason, allocationFields } = context;
  const advance = context.values?.scope === 'unbilled_ordinary';
  const combined = context.values?.scope === 'all';
  const needsQuote = advance || combined;
  const available = combinedCapacity(context.d, context.values?.entry_date?.format('YYYY-MM-DD') ?? dayjs().format('YYYY-MM-DD'));
  return (
    <>
      <Form.Item name="scope" label="还款范围">
        <ResponsiveSelect sheetTitle="还款范围" mobileSearch={false} options={[
          { value: 'all', label: '一起还（含未出账和分期）' },
          { value: 'issued', label: '还已出账' },
          { value: 'unbilled_ordinary', label: '提前还未出账（仅普通消费）' },
        ]} />
      </Form.Item>
      {combined && <p className="small muted">普通消费与分期一起还，只扣一笔款。可还合计 <Money value={available} />，包含对应未付手续费。</p>}
      {advance && <>
        <p>未出账普通消费可还 <Money value={context.ordinaryAvailable} /></p>
        {(Number(context.d.summary.current_due) > 0 || Number(context.d.summary.overdue) > 0) &&
          <Alert type="info" title="本次只还未出账普通消费，不包含已出账或逾期欠款" />}
        {Number(context.ordinaryAvailable) === 0 && <p className="muted">
          当前日期没有可提前偿还的未出账普通消费。
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
      {combined && <Button style={{ marginBottom: 16 }} disabled={cents(available) <= 0n} onClick={() => context.fillRepaymentAmount(available)}>全部还清</Button>}
      <Form.Item
        name="entry_date"
        label="实际还款日期"
        rules={[{ required: true }]}
      >
        <DatePicker getPopupContainer={popupInParent} disabledDate={needsQuote ? (date) => date.isAfter(dayjs(), 'day') : undefined} />
      </Form.Item>
      {!combined && allocationFields}
      {needsQuote && <>
        <Button onClick={context.loadOrdinaryPreview} loading={context.ordinaryPreviewing}>核对还款金额</Button>
        {combined && context.ordinaryPreview && <div className="liability-preview combined-repayment-preview">
          <div className="liability-term-preview"><span>本次合计</span><LiabilityAmount total={context.ordinaryPreview.amount} fee={context.ordinaryPreview.fee_amount ?? '0'} /></div>
          {cents(context.ordinaryPreview.ordinary_amount) > 0n && <p>普通消费及其他欠款 <Money value={context.ordinaryPreview.ordinary_amount ?? '0'} /></p>}
          {context.ordinaryPreview.installments?.map((term) => <div className="liability-term-preview" key={term.id}>
            <span>分期 #{term.plan_id} · 第 {term.number} 期</span>
            <LiabilityAmount total={term.total_amount} fee={sumMoney([term.outstanding_fees, term.scheduled_fees])} />
          </div>)}
          <p className="small muted">按到期日分配；分期从最早未还期开始连同手续费整期偿还。本金还款不重复计入支出。</p>
        </div>}
        {context.ordinaryPreview && <p className="ordinary-repayment-confirmed">
          已核对本次还款 <Money value={context.ordinaryPreview.amount} />，保存后一次扣款并完成以上分配。
        </p>}
      </>}
      {reason()}
    </>
  );
}
