import { DatePicker, InputNumber, Modal } from 'antd';
import type { Dayjs } from 'dayjs';
import Money from '../../components/Money';
import type { Voucher, VoucherAction } from '../../api/types';
import { ACTION, operationCost } from './display';
export default function VoucherOperation({
  voucher,
  operation,
  date,
  units,
  setDate,
  setUnits,
  setOperation,
  pending,
  submit,
}: {
  voucher: Voucher | undefined;
  operation: Exclude<VoucherAction, 'purchase'> | null;
  date: Dayjs;
  units: number;
  setDate: (date: Dayjs) => void;
  setUnits: (units: number) => void;
  setOperation: (operation: Exclude<VoucherAction, 'purchase'> | null) => void;
  pending: boolean;
  submit: () => Promise<void>;
}) {
  const mutations = { operate: { isPending: pending } };
  return (
    <Modal
      open={operation !== null}
      onCancel={() => setOperation(null)}
      title={operation ? ACTION[operation] : ''}
      okText="确认记录"
      cancelText="取消"
      onOk={submit}
      confirmLoading={mutations.operate.isPending}
    >
      {voucher && operation && (
        <div className="voucher-operation">
          <p>
            {voucher.name} · 剩余 {voucher.remaining_units} 次
          </p>
          <label>
            归属日期
            <DatePicker
              value={date}
              allowClear={false}
              onChange={(d) => d && setDate(d)}
              style={{ width: '100%' }}
              disabledDate={(d) =>
                d.format('YYYY-MM-DD') < voucher.purchase_date.slice(0, 10) ||
                (operation === 'redeem' &&
                  !!voucher.expires_on &&
                  d.format('YYYY-MM-DD') > voucher.expires_on.slice(0, 10))
              }
            />
          </label>
          {operation === 'redeem' && (
            <label>
              核销次数
              <InputNumber
                min={1}
                max={voucher.remaining_units}
                precision={0}
                value={units}
                onChange={(n) => n !== null && setUnits(Number(n))}
                style={{ width: '100%' }}
              />
            </label>
          )}
          <div>
            本次{operation === 'refund' ? '退款' : '消费'}金额：
            <Money
              value={
                operation === 'redeem'
                  ? operationCost(voucher, units)
                  : voucher.remaining_amount
              }
            />
          </div>
          <p className="muted">
            {operation === 'refund'
              ? '退回全部未用部分，恢复原支付账户余额；已核销的消费保留。'
              : operation === 'writeoff'
                ? '将全部剩余部分作废，剩余实付成本计入作废日支出和预算。'
                : '按实付成本分摊计入核销日消费，不再次扣余额。'}
          </p>
        </div>
      )}
    </Modal>
  );
}
