import { Empty, Skeleton, Tag } from 'antd';
import Money from '../../components/Money';
import type { VoucherListResp } from '../../api/types';
import { stateLabel, autoLabel } from './display';
export default function VoucherList({
  list,
  onSelect,
}: {
  list: { data?: VoucherListResp; isLoading: boolean };
  onSelect: (id: number) => void;
}) {
  return (
    <div className="voucher-list panel">
      {list.isLoading ? (
        <Skeleton active />
      ) : (list.data?.items ?? []).length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无券" />
      ) : (
        list.data!.items.map((v) => (
          <button
            className="voucher-row"
            key={v.id}
            onClick={() => onSelect(v.id)}
          >
            <div className="voucher-row-main">
              <div className="voucher-row-title">
                <strong>{v.name}</strong>
                <Tag
                  color={
                    v.status === 'failed'
                      ? 'error'
                      : v.status === 'expired'
                        ? 'warning'
                        : undefined
                  }
                >
                  {stateLabel(v)}
                </Tag>
              </div>
              <div className="muted">
                {v.platform || '未填写平台'} · 已用 {v.used_units}/
                {v.total_units} 次 · 购买 {v.purchase_date.slice(0, 10)}
              </div>
              <div className="voucher-expiry">
                {v.expires_on
                  ? `有效期至 ${v.expires_on.slice(0, 10)} · ${autoLabel(v)}`
                  : '未设置有效期'}
              </div>
              {v.last_error && v.status === 'failed' && (
                <div className="voucher-error">{v.last_error}</div>
              )}
            </div>
            <div className="voucher-row-amount">
              <span className="muted">剩余 {v.remaining_units} 次</span>
              <Money value={v.remaining_amount} />
              <span className="muted">实付 ¥{v.paid_amount}</span>
            </div>
          </button>
        ))
      )}
    </div>
  );
}
