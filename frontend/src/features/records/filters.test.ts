import { describe, expect, it } from 'vitest';
import {
  listParams,
  parseFilters,
  parsePage,
  serializeFilters,
  txnQueryParams,
} from './filters';

const fallback = { start: '2026-10-01', end: '2026-10-31' };
describe('transaction filter contract', () => {
  it('round-trips all supported filters and page', () => {
    const search = new URLSearchParams(
      'from=2026-01-01&to=2026-02-28&kind=refund&account_id=2&category_id=3&amount_min=0&amount_max=50&keyword=午餐&related_txn_id=8&page=3'
    );
    const filters = parseFilters(search, fallback);
    expect(
      parseFilters(
        serializeFilters(filters, parsePage(search), fallback),
        fallback
      )
    ).toEqual(filters);
    expect(parsePage(serializeFilters(filters, 3, fallback))).toBe(3);
  });
  it('falls back from invalid dates, amounts and identifiers', () => {
    const filters = parseFilters(
      new URLSearchParams(
        'from=2026-02-31&to=2026-03-01&account_id=-1&amount_min=30&amount_max=10'
      ),
      fallback
    );
    expect(filters.range).toEqual(fallback);
    expect(filters.account_id).toBeUndefined();
    expect(filters.amount_min).toBeUndefined();
    expect(parsePage(new URLSearchParams('page=NaN'))).toBe(1);
  });
  it('exports the same related refunds without pagination or date restrictions', () => {
    const filters = parseFilters(
      new URLSearchParams('related_txn_id=8&keyword=餐'),
      fallback
    );
    const params = listParams(filters, 4);
    const exported = txnQueryParams(params, true);
    expect(exported.related_txn_id).toBe(8);
    expect(exported.keyword).toBe('餐');
    expect(exported.start_date).toBeUndefined();
    expect(exported.page).toBeUndefined();
    expect(exported.page_size).toBeUndefined();
  });
});
