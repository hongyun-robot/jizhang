import { describe, expect, it } from 'vitest';
import { suggestedWealthShares, suggestedWealthProceeds, wealthOrderBalances, wealthOpeningUnitCost } from './wealth-orders';
import type { WealthOrder } from '../api/wealth';

describe('周利宝两位四舍五入建议值', () => {
  it('derives opening unit cost from exact total and confirmed shares', () => {
    expect(wealthOpeningUnitCost('1.16','1.14')).toBe('1.01754386');
    expect(wealthOpeningUnitCost('120','100')).toBe('1.20000000');
    expect(wealthOpeningUnitCost('0','1.14')).toBe('0.00000000');
    expect(wealthOpeningUnitCost('','1.14')).toBeUndefined();
    expect(wealthOpeningUnitCost('1.16','0')).toBeUndefined();
    expect(wealthOpeningUnitCost('1.160','1.14')).toBeUndefined();
  });
  it('matches the bank example without turning rounding into a fee', () => {
    expect(suggestedWealthShares('1.16', '1.021420')).toBe('1.14');
    expect(suggestedWealthProceeds('1.14', '1.021420')).toBe('1.16');
  });
  it('uses decimal half-up rounding and handles large input without floats', () => {
    expect(suggestedWealthShares('0.01', '2')).toBe('0.01');
    expect(suggestedWealthShares('9999999999.99', '1')).toBe('9999999999.99');
    expect(suggestedWealthShares('1000', '1', '10')).toBe('990.00');
    expect(suggestedWealthProceeds('150', '1.03333333', '200')).toBe('355.00');
  });
  it('does not invent a suggestion without a valid exact-date price', () => {
    expect(suggestedWealthShares('8000', '')).toBeUndefined();
    expect(suggestedWealthShares('8000', '0')).toBeUndefined();
    expect(suggestedWealthShares('8000.001', '1')).toBeUndefined();
  });
  it('separates same-day pending, previous-day available principal and reserved confirmed shares', () => {
    const orders: WealthOrder[] = [
      {event_id:1,kind:'subscribe',status:'pending',amount:'100',refund_due:'0',reserved_amount:'0',suggested_shares:'0',ready:false,data:{trade_date:'2026-10-16'}},
      {event_id:2,kind:'subscribe',status:'pending',amount:'200',refund_due:'0',reserved_amount:'50',suggested_shares:'0',ready:false,data:{trade_date:'2026-10-15'}},
      {event_id:3,kind:'request_redeem',status:'pending',amount:'50',refund_due:'0',reserved_amount:'0',suggested_shares:'40',ready:true,data:{trade_date:'2026-10-16'}},
    ];
    expect(wealthOrderBalances(orders,'100','2026-10-16','2026-10-15','1.2')).toEqual({current:'100.00',previous:'150.00',confirmedShares:'60.00000000',confirmedAmount:'72.00'});
  });
});
