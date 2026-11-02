import type { Account } from '../api/types';
import { money, parseCents } from '../lib/money';

/** Same debt convention as liability summaries: debt = max(-balance, 0). */
export function accountBalanceSummary(account: Pick<Account, 'type' | 'balance'>) {
  const balance = parseCents(account.balance);
  return {
    label: account.type === 'liability' ? '总欠款' : '余额',
    amount: balance === null ? null : money(account.type === 'liability' ? (balance < 0n ? -balance : 0n) : balance),
  };
}

export function isCashAccount(account: Pick<Account, 'type' | 'asset_class'>): boolean {
  return account.type === 'asset' && (!account.asset_class || account.asset_class === 'cash' || account.asset_class === 'wealth_cash');
}

export function accountOptions(
  accounts: Account[] | undefined,
  options: {
    ownerId?: number;
    selectedId?: number;
    selectedIds?: number[];
    assetOnly?: boolean;
    history?: boolean;
  } = {}
) {
  return (accounts ?? [])
    .filter((account) => {
      if (account.type === 'asset' && !isCashAccount(account)) return false;
      if (options.assetOnly && account.type !== 'asset') return false;
      if (
        account.type === 'liability' &&
        options.ownerId !== undefined &&
        account.owner_id !== options.ownerId
      )
        return false;
      return (
        account.status === 'active' ||
        (options.history &&
          (account.id === options.selectedId ||
            options.selectedIds?.includes(account.id)))
      );
    })
    .map((account) => ({
      value: account.id,
      label: `${account.name}${account.status === 'disabled' ? '（已停用）' : account.type === 'liability' ? '（信用）' : ''}`,
    }));
}
