import axios from 'axios';
import { describe, expect, it } from 'vitest';
import { createJsonRequester } from './request';
import { createLiabilityClient } from './liability-client';
import type { LiabilityCommand, SettlementInput } from './liability-inputs';
import {
  beginDialog,
  type LiabilityDialog,
} from '../features/liabilities/dialog';

const base = { request_key: 'stable-attempt', reason: '核对' };
const settlement: SettlementInput = {
  ...base,
  version: 2,
  principal: '10',
  fee_amount: '0',
  fee_account_id: 1,
  fee_category_id: 1,
  from_account_id: 1,
  entry_date: '2026-10-12',
};
describe('liability operation contract', () => {
  it('selects all endpoints from typed operations and preserves version/attempt data', async () => {
    const calls: { method?: string; url?: string; data: unknown }[] = [];
    const requester = createJsonRequester(
      axios.create({
        adapter: async (config) => {
          calls.push({
            method: config.method,
            url: config.url,
            data: JSON.parse(config.data),
          });
          return {
            config,
            status: 200,
            statusText: 'OK',
            headers: {},
            data: { code: 0, data: { operation_id: 1, transaction_ids: [] } },
          };
        },
      })
    );
    const client = createLiabilityClient((method, url, data) =>
      requester(url, { method, data })
    );
    const commands: LiabilityCommand[] = [
      {
        mode: 'repay',
        input: {
          ...base,
          account_id: 2,
          from_account_id: 1,
          amount: '10',
          entry_date: '2026-10-12',
          mode: 'auto',
          targets: [],
        },
      },
      {
        mode: 'allocation',
        txnId: 3,
        input: { ...base, mode: 'hold', targets: [] },
      },
      {
        mode: 'reconcile',
        billId: 4,
        input: { ...base, complete: true, moves: [] },
      },
      {
        mode: 'create_plan',
        input: {
          ...base,
          account_id: 2,
          source_txn_id: 3,
          term_count: 2,
          first_statement_date: '2026-10-31',
          fee_mode: 'per_term',
          fee_input: '0',
          fee_account_id: 2,
          fee_category_id: 1,
        },
      },
      {
        mode: 'edit_plan',
        planId: 5,
        input: { ...base, version: 2, terms: [] },
      },
      {
        mode: 'refund_adjustment',
        planId: 5,
        input: { ...base, version: 2, terms: [] },
      },
      { mode: 'settle', planId: 5, input: settlement },
      { mode: 'reverse', operationId: 6, input: base },
    ];
    for (const command of commands) await client.execute(command);
    await client.preview(5, settlement);
    expect(calls.map(({ method, url }) => [method, url])).toEqual([
      ['post', '/liabilities/repayments'],
      ['put', '/liabilities/repayments/3/allocations'],
      ['post', '/statements/4/reconcile'],
      ['post', '/liabilities/installments'],
      ['patch', '/liabilities/installments/5'],
      ['post', '/liabilities/installments/5/refund-adjustments'],
      ['post', '/liabilities/installments/5/settle'],
      ['post', '/liabilities/operations/6/reverse'],
      ['post', '/liabilities/installments/5/settlement-preview'],
    ]);
    expect(calls[6].data).toEqual(settlement);
    expect(calls[8].data).toEqual(settlement);
  });
  it('captures a version once and disallows incomplete states at compile time', () => {
    expect(
      beginDialog(
        { mode: 'settle', planId: 1 },
        [{ id: 1, version: 2 }],
        'attempt'
      ).version
    ).toBe(2);
    if (false) {
      // @ts-expect-error a plan operation requires a captured version
      const missing: LiabilityDialog = {
        mode: 'settle',
        planId: 1,
        requestKey: 'attempt',
      };
      // @ts-expect-error a repayment cannot contain an unrelated bill id
      const unrelated: LiabilityDialog = {
        mode: 'repay',
        billId: 1,
        requestKey: 'attempt',
      };
      void missing;
      void unrelated;
    }
  });
});
