package model

import "github.com/shopspring/decimal"

// TxnPurpose 由服务端操作决定，不能作为普通记账的可配置选项。
type TxnPurpose string

const (
	PurposeRegular           TxnPurpose = "regular"
	PurposeVoucherPurchase   TxnPurpose = "voucher_purchase"
	PurposeVoucherRedemption TxnPurpose = "voucher_redemption"
	PurposeVoucherRefund     TxnPurpose = "voucher_refund"
	PurposeVoucherWriteoff   TxnPurpose = "voucher_writeoff"
	PurposeFundPurchase      TxnPurpose = "fund_purchase"
	PurposeFundRedemption    TxnPurpose = "fund_redemption"
	PurposeFundDividend      TxnPurpose = "fund_dividend"
	PurposeGoldPurchase      TxnPurpose = "gold_purchase"
	PurposeGoldRedemption    TxnPurpose = "gold_redemption"
)

func (t Transaction) IsVoucher() bool {
	return t.VoucherEventID != nil || (t.Purpose != "" && t.Purpose != PurposeRegular && !t.IsFundManaged() && !t.IsGoldManaged())
}
func (t Transaction) IsGoldManaged() bool {
	return t.GoldEventID != nil || t.Purpose == PurposeGoldPurchase || t.Purpose == PurposeGoldRedemption
}

func (t Transaction) IsFundManaged() bool {
	return t.FundEventID != nil || t.Purpose == PurposeFundPurchase || t.Purpose == PurposeFundRedemption || t.Purpose == PurposeFundDividend
}

// Effects 返回行所属账户的资金影响和消费支出影响。转账的转入侧由账户聚合另计。
func (t Transaction) Effects() (decimal.Decimal, decimal.Decimal) {
	switch t.Purpose {
	case PurposeVoucherPurchase:
		return t.Amount.Neg(), decimal.Zero
	case PurposeVoucherRedemption, PurposeVoucherWriteoff:
		return decimal.Zero, t.Amount
	case PurposeVoucherRefund:
		return t.Amount, decimal.Zero
	}
	switch t.Kind {
	case TxnExpense:
		return t.Amount.Neg(), t.Amount
	case TxnRefund:
		return t.Amount, t.Amount.Neg()
	case TxnIncome:
		return t.Amount, decimal.Zero
	case TxnTransfer:
		return t.Amount.Neg(), decimal.Zero
	}
	return decimal.Zero, decimal.Zero
}

func (t *Transaction) FillEffects() {
	if t.Purpose == "" {
		t.Purpose = PurposeRegular
	}
	t.BalanceEffect, t.ExpenseEffect = t.Effects()
}
