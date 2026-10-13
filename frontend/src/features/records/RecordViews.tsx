import type { ReactNode } from 'react';
import type { Transaction, TxnKind } from '../../api/types';
import type { CategoryInfo } from '../../domain/categories';
import { Tag } from 'antd';
import dayjs from 'dayjs';
import Money from '../../components/Money';
import LiabilityFeeTag from '../../components/LiabilityFeeTag';
import { VOUCHER_PURPOSE_LABEL } from '../../labels';
import { cents, formatMoney } from '../../lib/money';
import { amountOf, tone, kindTag } from './display';

interface Presentation {
  acctName: ReadonlyMap<number, string>;
  catInfo: ReadonlyMap<number, CategoryInfo>;
  actions: (transaction: Transaction) => ReactNode;
  locateRefunds: (transaction: Transaction) => void;
}
export function recordColumns({
  acctName,
  catInfo,
  actions,
  locateRefunds,
}: Presentation) {
  return [
    {
      title: '日期',
      dataIndex: 'entry_date',
      key: 'date',
      width: 110,
      render: (v: string) => dayjs(v).format('YYYY-MM-DD'),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 116,
      render: (k: TxnKind, t: Transaction) => (
        <>
          {(t.purpose && VOUCHER_PURPOSE_LABEL[t.purpose]) || kindTag(k)}
          {t.source === 'recurring' && <Tag className="src-tag">自动</Tag>}
          {t.source === 'voucher_auto' && (
            <Tag className="src-tag">到期自动</Tag>
          )}
          <LiabilityFeeTag transaction={t} />
          {k === 'expense' && cents(t.refunded_amount ?? '0') > 0n && (
            <Tag
              className="refunded-tag"
              color="green"
              title="点击查看关联退款"
              role="button"
              tabIndex={0}
              onClick={() => locateRefunds(t)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  locateRefunds(t);
                }
              }}
            >
              已退 ¥{formatMoney(t.refunded_amount)}
            </Tag>
          )}
        </>
      ),
    },
    {
      title: '分类',
      key: 'category',
      width: 140,
      render: (_: unknown, t: Transaction) =>
        t.kind === 'transfer'
          ? '—'
          : ((t.category_id ? catInfo.get(t.category_id)?.full : undefined) ??
            '—'),
    },
    {
      title: '账户',
      key: 'account',
      width: 120,
      render: (_: unknown, t: Transaction) =>
        t.kind === 'transfer'
          ? `${t.account_name_override ?? acctName.get(t.account_id) ?? t.account_id} → ${t.transfer_name_override ?? acctName.get(t.transfer_account_id ?? 0) ?? ''}`
          : (acctName.get(t.account_id) ?? t.account_id),
    },
    {
      title: '金额',
      dataIndex: 'amount',
      key: 'amount',
      align: 'right' as const,
      width: 120,
      render: (_: string, t: Transaction) => (
        <>
          <Money value={amountOf(t)} tone={tone(t.kind)} />
          {t.voucher_event_id && (
            <div className="txn-effects">
              余额 {t.balance_effect} · 消费 {t.expense_effect}
            </div>
          )}
        </>
      ),
    },
    {
      title: '平台 / 商家',
      dataIndex: 'platform',
      key: 'platform',
      width: 130,
      ellipsis: true,
    },
    { title: '备注', dataIndex: 'note', key: 'note', ellipsis: true },
    {
      title: '',
      key: 'actions',
      width: 130,
      render: (_: unknown, t: Transaction) => actions(t),
    },
  ];
}
export function RecordRows({
  items,
  acctName,
  catInfo,
  actions,
  locateRefunds,
}: Presentation & { items: Transaction[] }) {
  return (
    <>
      {items.map((t) => (
        <div className="txn-row" key={t.id} data-txn-id={t.id}>
          <div className="txn-main">
            <div className="txn-title">
              <span className="txn-cat">
                {(t.purpose && VOUCHER_PURPOSE_LABEL[t.purpose]) ||
                  (t.kind === 'transfer'
                    ? '转账'
                    : ((t.category_id
                        ? catInfo.get(t.category_id)?.full
                        : undefined) ?? '未分类'))}
                {t.source === 'recurring' && (
                  <Tag className="src-tag">自动</Tag>
                )}
                {t.source === 'voucher_auto' && (
                  <Tag className="src-tag">到期自动</Tag>
                )}
                <LiabilityFeeTag transaction={t} />
                {t.kind === 'expense' &&
                  cents(t.refunded_amount ?? '0') > 0n && (
                    <Tag
                      className="refunded-tag"
                      color="green"
                      title="点击查看关联退款"
                      role="button"
                      tabIndex={0}
                      onClick={() => locateRefunds(t)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          locateRefunds(t);
                        }
                      }}
                    >
                      已退 ¥{formatMoney(t.refunded_amount)}
                    </Tag>
                  )}
              </span>
              <Money value={amountOf(t)} tone={tone(t.kind)} />
            </div>
            <div className="txn-sub">
              {dayjs(t.entry_date).format('MM-DD')} ·{' '}
              {t.kind === 'transfer'
                ? `${t.account_name_override ?? acctName.get(t.account_id) ?? ''} → ${t.transfer_name_override ?? acctName.get(t.transfer_account_id ?? 0) ?? ''}`
                : (acctName.get(t.account_id) ?? '')}
              {t.platform ? ` · ${t.platform}` : ''}
              {t.note ? ` · ${t.note}` : ''}
            </div>
            {t.voucher_event_id && (
              <div className="txn-effects">
                余额 {t.balance_effect} · 消费 {t.expense_effect}
              </div>
            )}
          </div>
          <div className="txn-actions">{actions(t)}</div>
        </div>
      ))}
    </>
  );
}
