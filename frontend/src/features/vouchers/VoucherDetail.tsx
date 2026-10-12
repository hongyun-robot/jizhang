import { Alert, Button, Popconfirm, Tag } from 'antd';
import dayjs from 'dayjs';
import Money from '../../components/Money';
import type {
  Account,
  Voucher,
  VoucherDetail as Detail,
  VoucherAction,
} from '../../api/types';

import { ACTION, stateLabel, autoLabel } from './display';
export default function VoucherDetail({
  voucher,
  accounts,
  pending,
  onEdit,
  begin,
  undo,
  remove,
  resume,
}: {
  voucher: Detail;
  accounts: { data?: Account[] };
  pending: { update: boolean; remove: boolean; undo: boolean };
  onEdit: (voucher: Voucher) => void;
  begin: (action: Exclude<VoucherAction, 'purchase'>) => void;
  undo: (id: number) => Promise<void>;
  remove: () => Promise<void>;
  resume: () => Promise<void>;
}) {
  return (
    <div className="voucher-detail">
      <div className="voucher-detail-head">
        <Tag>{stateLabel(voucher)}</Tag>
        <span>{voucher.platform || '未填写平台'}</span>
      </div>
      <div className="voucher-metrics">
        <div>
          <span>未用金额 · {voucher.remaining_units} 次</span>
          <Money value={voucher.remaining_amount} />
        </div>
        <div>
          <span>实付 · {voucher.total_units} 次</span>
          <Money value={voucher.paid_amount} />
        </div>
        <div>
          <span>已核销 · {voucher.used_units} 次</span>
          <Money value={voucher.used_amount} />
        </div>
        <div>
          <span>已退回 / 已作废</span>
          <span>
            <Money value={voucher.refunded_amount} /> /{' '}
            <Money value={voucher.written_off_amount} />
          </span>
        </div>
      </div>
      <div className="voucher-facts">
        <div>购买日：{voucher.purchase_date.slice(0, 10)}</div>
        <div>
          支付账户：
          {accounts.data?.find((a) => a.id === voucher.account_id)?.name ??
            voucher.account_id}
        </div>
        <div>有效期：{voucher.expires_on?.slice(0, 10) ?? '未设置'}</div>
        <div>{autoLabel(voucher)}</div>
        {voucher.note && <div>备注：{voucher.note}</div>}
      </div>
      {voucher.last_error && voucher.status === 'failed' && (
        <Alert
          type="warning"
          showIcon
          title="自动退款待处理"
          description={voucher.last_error}
        />
      )}
      {voucher.auto_refund_suspended && (
        <Alert
          type="info"
          showIcon
          title="自动退款已暂停"
          description="撤销后不会立即再次退款。核实后可手动处理或重新启用。"
        />
      )}
      <div className="voucher-actions">
        {voucher.can_operate && (
          <>
            <Button type="primary" onClick={() => begin('redeem')}>
              核销
            </Button>
            <Button onClick={() => begin('refund')}>退未用部分</Button>
            <Button onClick={() => begin('writeoff')}>作废</Button>
          </>
        )}
        {voucher.can_edit && (
          <Button onClick={() => onEdit(voucher)}>编辑券</Button>
        )}
        {voucher.can_edit &&
          voucher.auto_refund_enabled &&
          voucher.auto_refund_suspended &&
          voucher.remaining_units > 0 && (
            <Popconfirm
              title="重新启用到期自动退款？"
              description="已过期的券会按有效期补记退款。"
              onConfirm={resume}
            >
              <Button loading={pending.update}>重新启用自动退款</Button>
            </Popconfirm>
          )}
        {voucher.can_delete && (
          <Popconfirm title="删除券并撤销购买扣款？" onConfirm={remove}>
            <Button danger loading={pending.remove}>
              删除券
            </Button>
          </Popconfirm>
        )}
      </div>
      <h3>操作记录</h3>
      <div className="voucher-events">
        {[...voucher.events].reverse().map((event) => (
          <div
            className={`voucher-event${event.voided_at ? ' is-voided' : ''}`}
            key={event.id}
          >
            <div>
              <strong>{ACTION[event.action]}</strong> · {event.units} 次{' '}
              {event.source === 'auto' && <Tag>到期自动登记</Tag>}
              <div className="muted">
                {event.entry_date.slice(0, 10)}
                {event.voided_at
                  ? ` · 已撤销 ${dayjs(event.voided_at).format('MM-DD HH:mm')}`
                  : ''}
              </div>
            </div>
            <div className="voucher-event-end">
              <Money value={event.amount} />
              {event.can_undo && (
                <Popconfirm
                  title={`撤销这次${ACTION[event.action]}？`}
                  description={
                    event.source === 'auto'
                      ? '余额将回退，自动退款会暂停。'
                      : '恢复对应份额，并还原消费或余额。'
                  }
                  onConfirm={() => undo(event.id)}
                >
                  <Button type="link" size="small" loading={pending.undo}>
                    撤销
                  </Button>
                </Popconfirm>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
