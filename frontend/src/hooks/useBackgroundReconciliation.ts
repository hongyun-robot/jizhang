import { useEffect, useRef } from 'react';
import { useEnsureRecurring } from '../api/recurring';
import { useEnsureVouchers } from '../api/vouchers';
import { useEnsureLiabilities } from '../api/liability';
import { useAuth } from '../stores/auth';
import { canRecord } from '../domain/permissions';
import { startReconciliation } from '../lib/reconciliation';

export function useBackgroundReconciliation(): void {
  const recurring = useEnsureRecurring();
  const vouchers = useEnsureVouchers();
  const liabilities = useEnsureLiabilities();
  const epoch = useAuth((state) => state.epoch);
  const writer = canRecord(useAuth((state) => state.user?.role));
  const latest = useRef({ recurring, vouchers, liabilities });
  latest.current = { recurring, vouchers, liabilities };
  useEffect(
    () =>
      startReconciliation(
        [
          {
            key: 'recurring',
            repeat: false,
            run: () => latest.current.recurring.mutateAsync(),
          },
          {
            key: 'liabilities',
            repeat: true,
            run: () => latest.current.liabilities.mutateAsync(),
          },
          ...(writer
            ? [
                {
                  key: 'vouchers',
                  repeat: true,
                  run: () => latest.current.vouchers.mutateAsync(),
                },
              ]
            : []),
        ],
        epoch,
        () => useAuth.getState().epoch
      ),
    [epoch, writer]
  );
}
