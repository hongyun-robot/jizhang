import { useState } from 'react';
import { Alert, Button, Card, Empty, Space, Tag } from 'antd';
import type { WealthPosition, WealthEvent, WealthOrder, WealthOrderKind, WealthOrderData } from '../../api/wealth';
import WealthOrderForm from './WealthOrderForm';
import { useAuth, canRecord } from '../../stores/auth';
import Money from '../../components/Money';

export default function WealthOrders({ position, events, active }: {position: WealthPosition; events: WealthEvent[]; active: boolean}) {
  const user = useAuth((s) => s.user);
  const [action, setAction] = useState<{kind: WealthOrderKind; order?: WealthOrder; source?: WealthEvent}>();
  const orders = position.orders || [];
  const allowed = canRecord(user?.role);
  const weeklyAllocation = position.order_policy === 'zhoulibao_20250402';
  const open = (kind: WealthOrderKind, order: WealthOrder) => {
    const source = kind === 'refund_buy' ? events.find((e) => e.kind === 'cancel_buy' && e.source_event_id === order.event_id && !e.voided_at) : events.find((e) => e.id === order.event_id);
    setAction({ kind, order, source });
  };
  return <section className="wealth-orders">
    <Space wrap className="wealth-toolbar">{allowed && <>{active && position.product.admission_status === 'enabled' && <Button onClick={() => setAction({kind:'subscribe'})}>登记待确认申购</Button>}{weeklyAllocation && <Button onClick={() => setAction({kind:'request_redeem'})}>登记转出申请</Button>}</>}</Space>
    <Alert type="info" title="待确认本金和待退款继续计入资产" description={weeklyAllocation ? '甲类为已确认份额；乙类为所选上一交易日待确认申请；丙类为所选当日待确认申请，仅可撤销。转出先乙后甲，乙按银行申请时间后进先出。交易日按银行记录选择。' : '按银行记录登记申请和确认份额；实际已确认的赎回通过“赎回”登记，到账后再核对资金。'} />
    {!orders.length && <Empty description="暂无待确认申购或转出订单" />}
    {[...orders].reverse().map((order) => <Card key={order.event_id} className="panel wealth-event-row" data-testid={`wealth-order-${order.event_id}`}>
      <Space wrap><strong>{order.kind === 'subscribe' ? '申购申请' : '转出申请'} #{order.event_id}</strong><Tag>{({pending:'待银行确认',confirmed:'已确认',cancelled:'已取消或确认失败',refunded:'退款已登记完成',offset:'已分配转出抵扣，无剩余退款'} as Record<string,string>)[order.status] || order.status}</Tag><Money value={order.amount} /></Space>
      <p className="muted small">申请所属交易日 {order.data.trade_date || '—'} {order.data.application_time || ''}{order.data.expected_date ? ` · 预计确认 ${order.data.expected_date}（仅提醒）` : ''}</p>
      {order.kind==='request_redeem' && <p className="small">实际分配金额 {order.data.allocated_amount || '0'} 元{order.data.cancelled_for_insufficient ? ' · 余额不足，按所选策略取消' : order.data.insufficient_policy==='all' ? ' · 不足时按全部可转出余额' : ''}</p>}
      {Number(order.reserved_amount) > 0 && <p className="small">已分配至关联转出：<Money value={order.reserved_amount} />{order.status !== 'confirmed' && '，取消申购后仅剩余部分可退。'}</p>}
      {Number(order.refund_due) > 0 && <p>剩余待退：<Money value={order.refund_due} /></p>}
      {order.kind === 'request_redeem' && <><p className="small">{order.status === 'confirmed' ? `银行已确认份额 ${order.suggested_shares}` : order.ready ? `可核对建议份额 ${order.suggested_shares}` : '等待关联乙类申购确认或失败后，再确认转出结果'}</p>{order.data.allocations?.map((a, i) => <p className="small muted" key={i}>{a.source_id ? `乙类申购 #${a.source_id}` : '甲类已确认'}：{a.amount} 元 · 当前可核对 {a.shares} 份</p>)}</>}
      {allowed && <Space wrap>
        {order.kind === 'subscribe' && order.status === 'pending' && <><Button onClick={() => open('confirm_buy',order)}>确认申购份额</Button><Button onClick={() => open('cancel_buy',order)}>取消或登记确认失败</Button></>}
        {order.kind === 'subscribe' && Number(order.refund_due) > 0 && <Button onClick={() => open('refund_buy',order)}>登记实际退款</Button>}
        {order.kind === 'request_redeem' && order.status === 'pending' && <><Button disabled={!order.ready} onClick={() => open('confirm_redeem',order)}>确认转出结果</Button><Button onClick={() => open('cancel_redeem',order)}>取消转出申请</Button></>}
      </Space>}
    </Card>)}
    {action && <WealthOrderForm key={`${position.id}:${action.kind}:${action.order?.event_id || ''}`} accountId={position.account_id} position={position} {...action} onClose={() => setAction(undefined)} />}
  </section>;
}

export function WealthOrderEventInfo({ event }: {event: WealthEvent}) {
  if (!event.order_json) return null;
  try {
    const data = JSON.parse(event.order_json) as WealthOrderData;
    if (event.kind === 'opening' && data.opening_nav) return <p className="small muted">建账参考净值 {data.opening_nav} · 建账日期 {data.nav_date} · {data.nav_source === 'automatic' ? '同日自动同步值' : data.nav_source === 'manual' ? '同日本人补录值' : '手工填写'}，不改变实际总成本或行情序列。</p>;
    return <p className="small muted">{data.trade_date && `申请交易日 ${data.trade_date} · `}{data.nav_date && `净值日期 ${data.nav_date} · 成交净值 ${event.trade_nav} · `}{data.nav_source && `来源 ${data.nav_source === 'automatic' ? '自动同步' : data.nav_source === 'manual' ? '本人补录' : '银行确认'} · `}{data.reason && `说明：${data.reason}`}</p>;
  } catch { return null; }
}
