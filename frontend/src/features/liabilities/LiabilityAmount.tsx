import Money from '../../components/Money';

export default function LiabilityAmount({
  total,
  fee,
  feeLabel = '手续费',
  tone = 'plain',
}: {
  total: string;
  fee: string;
  feeLabel?: string;
  tone?: 'plain' | 'expense';
}) {
  return (
    <div className="liability-term-amount">
      <Money value={total} tone={tone} />
      <div className="small muted">
        {feeLabel} <Money value={fee} />
      </div>
    </div>
  );
}
