import type { Role, SessionUser } from './identity';
import type { Account, RecurringRule, Transaction } from '../api/types';

export const RECORDABLE_ROLES: readonly Role[] = [
  'admin',
  'recorder',
  'member',
];
export function canRecord(role?: Role | null): boolean {
  return !!role && RECORDABLE_ROLES.includes(role);
}
export function canManageSettings(user?: SessionUser | null): boolean {
  return user?.role === 'admin';
}
export function canManagePersonalAccount(
  user: SessionUser | null,
  account?: Account
): boolean {
  return (
    !!user &&
    (!account || (account.type === 'liability' && account.owner_id === user.id))
  );
}
export function canModifyTransaction(
  user: SessionUser | null,
  transaction: Transaction
): boolean {
  if (!user || !canRecord(user.role)) return false;
  if (
    transaction.installment_plan_id ||
    transaction.liability_term_id ||
    transaction.liability_operation_id
  )
    return false;
  if (
    transaction.voucher_event_id ||
    (transaction.purpose && transaction.purpose !== 'regular')
  )
    return false;
  return canModifyOwnedRecord(user, transaction.created_by);
}
export function canModifyOwnedRecord(
  user: SessionUser | null,
  owner: number
): boolean {
  return (
    !!user &&
    canRecord(user.role) &&
    (user.role !== 'member' || owner === user.id)
  );
}
export function canManageRule(
  user: SessionUser | null,
  rule: Pick<RecurringRule, 'created_by'>
): boolean {
  return (
    !!user &&
    canRecord(user.role) &&
    (user.role === 'admin' || rule.created_by === user.id)
  );
}
