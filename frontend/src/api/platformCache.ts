import type { QueryClient } from '@tanstack/react-query';
import type { VoucherPlatform } from './types';
import { queryKeys } from './keys';
import { useAuth } from '../stores/auth';

/** A cancelled old request must not repopulate a cleared session cache. */
export async function updatePlatformCache(
  client: QueryClient,
  scope: number,
  update: (rows: VoucherPlatform[]) => VoucherPlatform[]
): Promise<void> {
  if (scope !== useAuth.getState().epoch) return;
  const root = queryKeys.root('voucher-platforms', scope);
  await client.cancelQueries({ queryKey: root });
  if (scope !== useAuth.getState().epoch) return;
  client.setQueryData<VoucherPlatform[]>(
    queryKeys.key('voucher-platforms', scope),
    (rows = []) => update(rows).sort((a, b) => a.sort - b.sort || a.id - b.id)
  );
  void client.invalidateQueries({ queryKey: root });
}
