package model

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestVoucherCost(t *testing.T) {
	paid := decimal.RequireFromString("10.00")
	want := []string{"3.33", "3.33", "3.34"}
	sum := decimal.Zero
	for i, v := range want {
		got := VoucherCost(paid, 3, int64(i), 1)
		if !got.Equal(decimal.RequireFromString(v)) {
			t.Fatalf("unit %d: %s", i, got)
		}
		sum = sum.Add(got)
	}
	if !sum.Equal(paid) {
		t.Fatal("cost does not conserve payment")
	}
	if !VoucherCost(paid, 3, 0, 2).Equal(decimal.RequireFromString("6.66")) {
		t.Fatal("batch allocation")
	}
	if !VoucherCost(paid, 3, 2, 1).Equal(decimal.RequireFromString("3.34")) {
		t.Fatal("remaining refund")
	}
	if !VoucherCost(decimal.RequireFromString("0.01"), 2, 0, 1).IsZero() {
		t.Fatal("zero-cost unit")
	}
	if !VoucherCost(decimal.RequireFromString("0.01"), 2, 1, 1).Equal(decimal.RequireFromString("0.01")) {
		t.Fatal("last cent")
	}
}

func TestTransactionEffects(t *testing.T) {
	for _, tc := range []struct {
		kind             TxnKind
		purpose          TxnPurpose
		balance, expense string
	}{
		{TxnExpense, PurposeRegular, "-89", "89"}, {TxnRefund, PurposeRegular, "89", "-89"},
		{TxnIncome, PurposeRegular, "89", "0"}, {TxnTransfer, PurposeRegular, "-89", "0"},
		{TxnExpense, PurposeVoucherPurchase, "-89", "0"},
		{TxnExpense, PurposeVoucherRedemption, "0", "89"},
		{TxnRefund, PurposeVoucherRefund, "89", "0"},
		{TxnExpense, PurposeVoucherWriteoff, "0", "89"},
	} {
		txn := Transaction{Kind: tc.kind, Purpose: tc.purpose, Amount: decimal.NewFromInt(89)}
		b, e := txn.Effects()
		if !b.Equal(decimal.RequireFromString(tc.balance)) || !e.Equal(decimal.RequireFromString(tc.expense)) {
			t.Fatalf("%s/%s: %s,%s", tc.kind, tc.purpose, b, e)
		}
	}
}
