import AccountSelect from '../../../components/AccountSelect';
import ResponsiveSelect from '../../../components/ResponsiveSelect';
import { popupInParent } from '../../../lib/popup';
import { Button, Form } from 'antd';
import DatePicker from '../../../components/DatePicker';
import dayjs from 'dayjs';
import Money from '../../../components/Money';
import LiabilityAmount from '../LiabilityAmount';
import { cents, sumMoney } from '../../../lib/money';
import type { FieldContext } from './context';

export default function SettlementFields({ context }: { context: FieldContext }) {
  const { assets, reason, preview, previewing, loadPreview, plan, dialog } = context;
  const pending = plan?.terms.filter((term) => cents(term.remaining) > 0n) ?? [];
  const source = context.d.transactions.find((transaction) => transaction.id === plan?.source_txn_id);
  return (
    <>
      {dialog.mode === 'repay_plan' && (
        <Form.Item name="repayment_end" label="还款期数" rules={[{ required: true }]}>
          <ResponsiveSelect
            sheetTitle="还款期数"
            mobileSearch={false}
            options={[
              ...pending.map((term, index) => ({ value: term.number, label: `还至第 ${term.number} 期 · 共 ${index + 1} 期` })),
              { value: 0, label: '全部剩余期数' },
            ]}
          />
        </Form.Item>
      )}
      {dialog.mode === 'settle' && <p>一次还清全部剩余期数的本金与未付手续费。</p>}
      <Form.Item name="from_account_id" label="还款账户" rules={[{ required: true }]}>
        <AccountSelect assetOnly onCreatingChange={context.setCreatingAccount} sheetTitle="还款账户" options={assets} getPopupContainer={popupInParent} />
      </Form.Item>
      <Form.Item name="entry_date" label="实际还款日期" rules={[{ required: true }]}>
        <DatePicker
          getPopupContainer={popupInParent}
          disabledDate={(date) => date.isAfter(dayjs(), 'day') || (!!source && date.isBefore(dayjs(source.entry_date), 'day'))}
        />
      </Form.Item>
      <p className="small muted">从最早未还清的一期开始，所选期次连同未付手续费一次还清，包含未出账费用。</p>
      {reason()}
      <Button onClick={loadPreview} loading={previewing}>核对还款金额</Button>
      {preview && (
        <div className="liability-preview">
          <div className="liability-term-preview">
            <span className="small muted">本次应付合计</span>
            <LiabilityAmount total={preview.total_amount} fee={sumMoney([preview.outstanding_fees, preview.scheduled_fees])} />
          </div>
          {preview.terms.map((term) => (
            <div className="liability-term-preview" key={term.id}>
              <span>第 {term.number} 期</span>
              <LiabilityAmount total={term.total_amount} fee={sumMoney([term.outstanding_fees, term.scheduled_fees])} />
            </div>
          ))}
          <p>剩余本金 <Money value={preview.principal} /> · 已发生未付手续费 <Money value={preview.outstanding_fees} /> · 本次提前收取手续费 <Money value={preview.scheduled_fees} /></p>
          <p className="small muted">金额由所选期次计算，全部从还款账户支付；已支付手续费不会重复收取。</p>
        </div>
      )}
    </>
  );
}
