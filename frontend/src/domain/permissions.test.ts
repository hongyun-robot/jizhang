import { describe, expect, it } from 'vitest';
import type { Role, SessionUser } from './identity';
import type { Account, Transaction } from '../api/types';
import {
  canManagePersonalAccount,
  canManageRule,
  canModifyTransaction,
  canRecord,
} from './permissions';

const transaction: Transaction = {
  id: 1,
  kind: 'expense',
  amount: '10',
  account_id: 1,
  entry_date: '2026-10-12',
  period_key: '2026-10',
  created_by: 1,
  source: 'manual',
  created_at: '',
  updated_at: '',
};
const account: Account = {
  id: 1,
  type: 'liability',
  owner_id: 1,
  name: 'credit',
  opening_balance: '0',
  status: 'active',
  sort: 1,
};
const actor = (role: Role): SessionUser => ({
  id: 1,
  username: role,
  role,
  status: 'active',
});
describe('resource permissions', () => {
  it.each(['admin', 'recorder', 'member', 'viewer'] as const)(
    'enforces money and ownership for %s',
    (role) => {
      const user = actor(role);
      expect(canRecord(role)).toBe(role !== 'viewer');
      expect(canModifyTransaction(user, transaction)).toBe(role !== 'viewer');
      expect(
        canModifyTransaction(user, { ...transaction, created_by: 2 })
      ).toBe(role === 'admin' || role === 'recorder');
      expect(canManageRule(user, { created_by: 2 })).toBe(role === 'admin');
      expect(canManagePersonalAccount(user, account)).toBe(true);
      expect(canManagePersonalAccount(user, { ...account, owner_id: 2 })).toBe(
        false
      );
    }
  );
  it('requires dedicated workflows for protected funds regardless of role', () => {
    for (const protectedRecord of [
      { voucher_event_id: 1 },
      { installment_plan_id: 1 },
      { liability_operation_id: 1 },
      { purpose: 'voucher_purchase' as const },
    ]) {
      expect(
        canModifyTransaction(actor('admin'), {
          ...transaction,
          ...protectedRecord,
        })
      ).toBe(false);
    }
    expect(
      canManagePersonalAccount(actor('viewer'), { ...account, type: 'asset' })
    ).toBe(false);
  });
});
