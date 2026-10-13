import { Tag } from 'antd';
import type { Transaction } from '../api/types';

export default function LiabilityFeeTag({ transaction: t }: { transaction: Transaction }) {
  if (t.kind !== 'expense' || (!t.liability_term_id && t.source !== 'liability_auto')) return null;
  return (
    <Tag className="src-tag" title="按收取日期记为费用支出；还款记为转账，不重复计入支出。">
      {t.liability_term_id ? '分期手续费' : '负债手续费'}
    </Tag>
  );
}
