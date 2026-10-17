import axios from 'axios';
import { createJsonRequester } from '../../api/request';
import dayjs from 'dayjs';
import { describe, expect, it } from 'vitest';
import { ordinaryCapacity } from './ordinary';
import { liabilityCommand } from './input';
import type { LiabilityDetail } from '../../api/liability';
import { createLiabilityClient } from '../../api/liability-client';

describe('ordinary advance drafts', () => {
  it('shows only dated ordinary unpaid principal, counting cents exactly in a mixed bill', () => {
    const data: Parameters<typeof ordinaryCapacity>[0] = {
      account: { id: 1 },
      statements: [
        { id: 1, kind: 'monthly', issued_at: null, due_date: '2026-11-10', cycle_end: '2026-10-31' },
        { id: 2, kind: 'monthly', issued_at: '2026-10-01', due_date: '2026-10-10', cycle_end: '2026-09-30' },
      ],
      transactions: [
        { id: 1, account_id: 1, kind: 'expense', entry_date: '2026-10-10', balance_effect: '0' },
        { id: 2, account_id: 1, kind: 'expense', entry_date: '2026-10-10', purpose: 'voucher_purchase', balance_effect: '-0.20' },
        { id: 3, account_id: 1, kind: 'expense', entry_date: '2026-10-15' },
        { id: 4, account_id: 1, kind: 'transfer', entry_date: '2026-10-10' },
      ],
      items: [1, 2, 3, 4].map((id) => ({ id, source_id: id, source_txn_id: id, kind: 'principal', statement_id: 1, plan_id: null, term_id: null, amount: '100', remaining: id === 1 ? '0.10' : id === 2 ? '0.20' : '100' })),
    };
    const before = JSON.stringify(data);
    expect(ordinaryCapacity(data, '2026-10-12').total).toBe('0.30');
    expect(ordinaryCapacity({ ...data, items: data.items.map((i) => ({ ...i, plan_id: 5 })) }, '2026-10-12').total).toBe('0.00');
    expect(JSON.stringify(data)).toBe(before);
  });

  const detail: LiabilityDetail = {
    account: { id: 1, name: '花呗', type: 'liability', opening_balance: '0', status: 'active', sort: 1 },
    summary: { account_id: 1, name: '花呗', liability_type: 'huabei', billing_enabled: true, balance: '-1000', total_debt: '1000', current_due: '0', due_date: '', overdue: '0', unbilled: '1000', future_principal: '0', forecast_fees: '0' },
    rules: [], statements: [], items: [], credits: [], allocations: [], plans: [], operations: [], transactions: [],
  };
  it('preserves old request shape and sends the explicit scope/quote only for advances', async () => {
    const draft = { amount: '300.01', from_account_id: 2, entry_date: dayjs('2026-10-15'), quote_signature: 'confirmed' };
    const dialog = { mode: 'repay', requestKey: 'stable-attempt' } as const;
    const old = liabilityCommand(detail, dialog, draft);
    expect(old.input).not.toHaveProperty('scope');
    expect(old.input).not.toHaveProperty('quote_signature');
    const command = liabilityCommand(detail, dialog, { ...draft, scope: 'unbilled_ordinary' });
    expect(command.input).toMatchObject({ amount: '300.01', scope: 'unbilled_ordinary', quote_signature: 'confirmed', targets: [] });
    const calls: { url: string; data: unknown }[] = [];
    const requester = createJsonRequester(axios.create({ adapter: async (config) => {
      calls.push({ url: config.url ?? '', data: JSON.parse(config.data) });
      return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data: { operation_id: 1, transaction_ids: [] } } };
    } }));
    const client = createLiabilityClient((method, url, data) => requester(url, { method, data }));
    if (command.mode !== 'repay') throw new Error('expected repayment');
    await client.ordinaryPreview(command.input);
    await client.execute(command);
    expect(calls.map((c) => c.url)).toEqual(['/liabilities/repayments/preview', '/liabilities/repayments']);
    expect(calls[0].data).toEqual(calls[1].data);
  });
});
