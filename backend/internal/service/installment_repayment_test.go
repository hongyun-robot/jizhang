package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func repaymentPlan(t *testing.T, e *liabilityEnv) int64 {
	t.Helper()
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	return r.PlanID
}

func quotedRepayment(t *testing.T, e *liabilityEnv, id int64, count int) (InstallmentRepaymentInput, SettlementPreview) {
	t.Helper()
	p, err := e.svcL.Plan(adminUser(), id)
	mustOK(t, err)
	in := InstallmentRepaymentInput{RequestKey: uuid.NewString(), FromAccountID: e.account, EntryDate: liability.Date(e.now), Version: p.Version, AllRemaining: count == 0}
	if count > 0 {
		for _, term := range p.Terms {
			if term.Remaining.IsPositive() && len(in.TermIDs) < count {
				in.TermIDs = append(in.TermIDs, term.ID)
			}
		}
	}
	preview, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in)
	mustOK(t, err)
	in.QuoteSignature = preview.QuoteSignature
	in.TotalAmount = &preview.TotalAmount
	return in, preview
}

func settlementRequest(t *testing.T, e *liabilityEnv, id int64) SettlementInput {
	t.Helper()
	in, quote := quotedRepayment(t, e, id, 0)
	return SettlementInput{RequestKey: in.RequestKey, Version: in.Version, FromAccountID: in.FromAccountID, EntryDate: in.EntryDate, QuoteSignature: quote.QuoteSignature, TotalAmount: &quote.TotalAmount}
}

// Seed an actual pre-cutover record without routing it through the new write
// policy. New application interfaces must never recreate this partial payment.
func historicalPayment(t *testing.T, e *liabilityEnv, amount, date string) LiabilityResult {
	t.Helper()
	e.now = vdate(t, date)
	before := e.state(t)
	period := e.txn.deps.Periods.KeyForDate(e.now)
	result := LiabilityResult{}
	mustOK(t, e.db.Transaction(func(tx *gorm.DB) error {
		op := model.LiabilityOperation{ActorID: 1, RequestKey: uuid.NewString(), Kind: "repayment", AccountID: e.card, BeforeJSON: jsonText(before), ResultJSON: "{}"}
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		transaction := model.Transaction{Kind: model.TxnTransfer, Amount: dec(amount), AccountID: e.account, TransferAccountID: &e.card, EntryDate: e.now, PeriodKey: period, CreatedBy: 1, Source: model.SourceManual}
		if err := repository.NewTransactionRepo(tx).Create(&transaction); err != nil {
			return err
		}
		result = LiabilityResult{OperationID: op.ID, TransactionIDs: []int64{transaction.ID}}
		return tx.Model(&op).Update("result_json", jsonText(result)).Error
	}))
	return result
}

func TestInstallmentRepaymentContinuousAndLocked(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	in, quote := quotedRepayment(t, e, id, 2)
	if !quote.Principal.Equal(dec("2000")) || !quote.ScheduledFees.Equal(dec("40")) || !quote.TotalAmount.Equal(dec("2040")) {
		t.Fatal(quote)
	}
	before := e.state(t)
	wrong := in
	amount := dec("2000")
	wrong.TotalAmount = &amount
	_, err := e.svcL.RepayInstallment(adminUser(), id, wrong)
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("unlocked amount", err)
	}
	after := e.state(t)
	if len(before.Transactions) != len(after.Transactions) || !before.Summary.Balance.Equal(after.Summary.Balance) {
		t.Fatal("failed amount changed funds")
	}
	paid, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	again, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	if paid.OperationID != again.OperationID || len(paid.TransactionIDs) != 3 || len(paid.TermIDs) != 2 {
		t.Fatal(paid, again)
	}
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("1000")) || !d.Summary.ForecastFees.Equal(dec("20")) || d.Plans[0].Status != "active" {
		t.Fatal(d.Summary, d.Plans)
	}
	for _, term := range d.Plans[0].Terms {
		if term.Number <= 2 && (!term.Remaining.IsZero() || term.Status != "settled" || term.FeeTxnID == nil) {
			t.Fatal(term)
		}
		if term.Number == 3 && (term.Status != "scheduled" || term.FeeTxnID != nil || !term.Remaining.Equal(dec("1000"))) {
			t.Fatal("changed unselected term", term)
		}
	}
	accounts, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, asset := range accounts {
		if asset.ID == e.account && !asset.Balance.Equal(dec("-1040")) {
			t.Fatal("fee charged twice", asset.Balance)
		}
	}
}

func TestInstallmentRepaymentSelectionAndDates(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	p, err := e.svcL.Plan(adminUser(), id)
	mustOK(t, err)
	base := InstallmentRepaymentInput{FromAccountID: e.account, EntryDate: "2026-10-15"}
	for _, terms := range [][]int64{{p.Terms[1].ID}, {p.Terms[0].ID, p.Terms[2].ID}, {p.Terms[0].ID, p.Terms[0].ID}, {999999}} {
		in := base
		in.TermIDs = terms
		if _, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in); err == nil {
			t.Fatal("invalid range accepted", terms)
		}
	}
	base.AllRemaining = true
	for _, date := range []string{"2026-10-16", "2026-10-14", "2026-02-30"} {
		in := base
		in.EntryDate = date
		if _, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in); err == nil {
			t.Fatal("invalid date accepted", date)
		}
	}
	if _, err := e.svcL.InstallmentRepaymentPreview(&model.User{ID: 2, Role: model.RoleViewer}, id, base); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
}

func TestInstallmentRepaymentUsesSelectedAccountForNewFees(t *testing.T) {
	e := newLiabilityEnv(t)
	owner := int64(1)
	other := model.Account{Name: "原手续费账户", Type: model.AccountAsset, OwnerID: &owner, Status: model.StatusActive, OpeningBalance: dec("100")}
	mustOK(t, e.db.Create(&other).Error)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: other.ID, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	in, _ := quotedRepayment(t, e, r.PlanID, 1)
	_, err = e.svcL.RepayInstallment(adminUser(), r.PlanID, in)
	mustOK(t, err)
	accounts, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, a := range accounts {
		if a.ID == other.ID && !a.Balance.Equal(dec("100")) {
			t.Fatal("charged original fee account", a.Balance)
		}
	}
	p := e.state(t).Plans[0]
	if p.FeeAccountID != other.ID || p.Terms[1].FeeTxnID != nil || p.Terms[0].FeeDate != "2026-10-31" {
		t.Fatal("lost unselected configuration or planned date", p)
	}
}

func TestInstallmentRepaymentNoRepeatedFeeOrBilling(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	in, _ := quotedRepayment(t, e, id, 0)
	_, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	e.now = vdate(t, "2027-01-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	fees := 0
	for _, transaction := range d.Transactions {
		if transaction.Kind == model.TxnExpense && transaction.LiabilityTermID != nil {
			fees++
		}
	}
	if fees != 3 || !d.Summary.TotalDebt.IsZero() || !d.Summary.ForecastFees.IsZero() || d.Plans[0].Status != "settled" {
		t.Fatal(fees, d.Summary, d.Plans)
	}
	for _, bill := range d.Statements {
		if bill.Kind == "monthly" && !bill.Amount.IsZero() {
			t.Fatal("prepaid term billed again", bill)
		}
	}
}

func TestInstallmentRepaymentReverseInOrder(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	in, _ := quotedRepayment(t, e, id, 1)
	first, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	in, _ = quotedRepayment(t, e, id, 1)
	second, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	_, err = e.svcL.Reverse(adminUser(), first.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "先撤销前一笔"})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("ignored successor", err)
	}
	_, err = e.svcL.Reverse(adminUser(), second.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "先撤销后一笔"})
	mustOK(t, err)
	_, err = e.svcL.Reverse(adminUser(), first.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "再撤销前一笔"})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("3000")) || !d.Summary.ForecastFees.Equal(dec("60")) || d.Plans[0].Status != "active" {
		t.Fatal(d.Summary, d.Plans)
	}
	for _, term := range d.Plans[0].Terms {
		if term.FeeTxnID != nil || term.Status != "scheduled" || !term.Remaining.Equal(dec("1000")) {
			t.Fatal("incomplete restoration", term)
		}
	}
}

func TestInstallmentSettlementNewQuoteRetry(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	in, quote := quotedRepayment(t, e, id, 0)
	request := SettlementInput{RequestKey: in.RequestKey, Version: in.Version, FromAccountID: in.FromAccountID, EntryDate: in.EntryDate, QuoteSignature: quote.QuoteSignature, TotalAmount: &quote.TotalAmount}
	first, err := e.svcL.Settle(adminUser(), id, request)
	mustOK(t, err)
	again, err := e.svcL.Settle(adminUser(), id, request)
	mustOK(t, err)
	if first.OperationID != again.OperationID || !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal(first, again)
	}
	request.Principal = dec("3000")
	if _, err := e.svcL.Settle(adminUser(), id, request); !errors.Is(err, liability.ErrConflict) {
		t.Fatal("old amount contract accepted", err)
	}
}

func TestLiabilityAuditReadOnlyAndCutover(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	// A persisted old transfer can have unused funds while the balance is negative.
	transfer := model.Transaction{Kind: model.TxnTransfer, Amount: dec("200"), AccountID: e.account, TransferAccountID: &e.card, EntryDate: e.now, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual}
	mustOK(t, repository.NewTransactionRepo(e.db).Create(&transfer))
	before, err := json.Marshal(e.state(t))
	mustOK(t, err)
	audit, err := liability.AuditRepayments(e.db)
	mustOK(t, err)
	if len(audit.Funds) != 1 || audit.Funds[0].SourceID != transfer.ID || !audit.Funds[0].Amount.Equal(dec("200")) {
		t.Fatal(audit)
	}
	if !errors.Is(liability.CheckRepaymentCutover(e.db), liability.ErrConflict) {
		t.Fatal("cutover ignored negative-balance unused funds")
	}
	after, err := json.Marshal(e.state(t))
	mustOK(t, err)
	if string(before) != string(after) {
		t.Fatal("read-only audit changed persisted state")
	}
	var term model.InstallmentTerm
	mustOK(t, e.db.Where("plan_id = ? AND number = ?", id, 1).First(&term).Error)
	mustOK(t, e.db.Model(&model.LiabilityDebtItem{}).Where("term_id = ? AND kind = ?", term.ID, "principal").Update("amount", dec("0")).Error)
	audit, err = liability.AuditRepayments(e.db)
	mustOK(t, err)
	if len(audit.Problems) != 1 || audit.Problems[0].TermID != term.ID {
		t.Fatal("fee-only legacy problem omitted", audit)
	}
}

func TestInstallmentRepaymentFeeModesAndExactCents(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     string
		fee      string
		asset    bool
		expected string
		fees     int
	}{
		{"zero", "per_term", "0", false, "100", 0},
		{"small-cents", "per_term", "0.08", false, "100.24", 3},
		{"total-spread-remainder", "total_spread", "10", false, "110", 3},
		{"already-paid-upfront", "upfront", "60", true, "100", 1},
		{"already-paid-first-fee", "per_term", "20", true, "140", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newLiabilityEnv(t)
			feeAccount := e.card
			var dates []string
			if test.asset {
				feeAccount = e.account
				if test.mode == "per_term" {
					dates = []string{"2026-10-15", "2026-11-30", "2026-12-31"}
				}
			}
			created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("100"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: test.mode, FeeInput: dec(test.fee), FeeAccountID: feeAccount, FeeCategoryID: e.category, FeeDate: "2026-10-15", FeeDates: dates, RequestKey: uuid.NewString()})
			mustOK(t, err)
			in, quote := quotedRepayment(t, e, created.PlanID, 0)
			if !quote.TotalAmount.Equal(dec(test.expected)) {
				t.Fatal("wrong fee sources", quote)
			}
			_, err = e.svcL.RepayInstallment(adminUser(), created.PlanID, in)
			mustOK(t, err)
			d := e.state(t)
			fees := 0
			for _, transaction := range d.Transactions {
				if !transaction.Amount.IsPositive() {
					t.Fatal("zero posting", transaction)
				}
				if transaction.Kind == model.TxnExpense && transaction.LiabilityTermID != nil {
					fees++
				}
			}
			if fees != test.fees || !d.Summary.TotalDebt.IsZero() {
				t.Fatal(fees, d.Summary)
			}
		})
	}
}

func TestInstallmentRepaymentNetPostedFeeAndStaleQuote(t *testing.T) {
	for _, refunded := range []bool{false, true} {
		t.Run(map[bool]string{false: "historically-paid-fee", true: "actual-fee-refund"}[refunded], func(t *testing.T) {
			e := newLiabilityEnv(t)
			id := repaymentPlan(t, e)
			e.now = vdate(t, "2026-11-01")
			_, err := e.svcL.EnsureDue(e.now)
			mustOK(t, err)
			d := e.state(t)
			term := d.Plans[0].Terms[0]
			if refunded {
				_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("5"), AccountID: e.card, RelatedTxnID: *term.FeeTxnID, EntryDate: e.now})
				mustOK(t, err)
			} else {
				paid := historicalPayment(t, e, "5", "2026-11-01")
				var fee model.LiabilityDebtItem
				mustOK(t, e.db.Where("source_id = ? AND kind = ?", *term.FeeTxnID, "fee").First(&fee).Error)
				mustOK(t, e.db.Model(&model.LiabilityCreditEvent{}).Where("source_id = ?", paid.TransactionIDs[0]).Updates(map[string]any{"mode": "settlement", "targets": jsonText([]liability.Target{{DebtItemID: fee.ID, Amount: dec("5")}})}).Error)
				mustOK(t, liability.Reallocate(e.db, e.card))
			}
			in, quote := quotedRepayment(t, e, id, 1)
			if !quote.OutstandingFees.Equal(dec("15")) || !quote.TotalAmount.Equal(dec("1015")) || !quote.ScheduledFees.IsZero() {
				t.Fatal(quote)
			}
			_, err = e.svcL.RepayInstallment(adminUser(), id, in)
			mustOK(t, err)
		})
	}
	t.Run("balance-changed-without-plan-version", func(t *testing.T) {
		e := newLiabilityEnv(t)
		id := repaymentPlan(t, e)
		e.now = vdate(t, "2026-11-01")
		_, err := e.svcL.EnsureDue(e.now)
		mustOK(t, err)
		in, _ := quotedRepayment(t, e, id, 0)
		historicalPayment(t, e, "500", "2026-11-01")
		plan, err := e.svcL.Plan(adminUser(), id)
		mustOK(t, err)
		if plan.Version != in.Version {
			t.Fatal("fixture changed the version")
		}
		_, err = e.svcL.RepayInstallment(adminUser(), id, in)
		if !errors.Is(err, liability.ErrConflict) {
			t.Fatal("stale quote accepted", err)
		}
		if len(e.state(t).Transactions) != 3 {
			t.Fatal("conflict added a posting")
		}
	})
}

func TestInstallmentRepaymentPreservesCancelledHistory(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	mustOK(t, e.db.Model(&model.InstallmentTerm{}).Where("plan_id = ? AND number = ?", id, 1).Update("fee_status", "cancelled").Error)
	mustOK(t, liability.CheckRepaymentCutover(e.db))
	in, quote := quotedRepayment(t, e, id, 0)
	if !quote.TotalAmount.Equal(dec("3040")) {
		t.Fatal(quote)
	}
	_, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	term := e.state(t).Plans[0].Terms[0]
	if term.FeeStatus != "cancelled" || term.FeeTxnID != nil || !term.FeeAmount.Equal(dec("20")) {
		t.Fatal("cancelled history changed", term)
	}
}

func TestLiabilityHistoricalUnallocatedRealReturn(t *testing.T) {
	e := newLiabilityEnv(t)
	repaymentPlan(t, e)
	old := model.Transaction{Kind: model.TxnTransfer, Amount: dec("200"), AccountID: e.account, TransferAccountID: &e.card, EntryDate: e.now, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual}
	mustOK(t, repository.NewTransactionRepo(e.db).Create(&old))
	mustOK(t, e.db.Model(&model.LiabilityCreditEvent{}).Where("source_id = ?", old.ID).Update("mode", "hold").Error)
	if !errors.Is(liability.CheckRepaymentCutover(e.db), liability.ErrConflict) {
		t.Fatal("legacy funds ignored")
	}
	// This represents an actually completed bank return, not a migration making
	// up cash movement. The exact returned debt item receives the old funds.
	mustOK(t, repository.LedgerWrite(e.db, func(tx *gorm.DB) error {
		returned := model.Transaction{Kind: model.TxnTransfer, Amount: dec("200"), AccountID: e.card, TransferAccountID: &e.account, EntryDate: e.now, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual, Note: "已实际返还的历史余款"}
		if err := repository.NewTransactionRepo(tx).Create(&returned); err != nil {
			return err
		}
		var item model.LiabilityDebtItem
		if err := tx.Where("source_id = ? AND account_id = ?", returned.ID, e.card).First(&item).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.LiabilityCreditEvent{}).Where("source_id = ?", old.ID).Updates(map[string]any{"mode": "settlement", "targets": jsonText([]liability.Target{{DebtItemID: item.ID, Amount: dec("200")}})}).Error; err != nil {
			return err
		}
		return liability.Reallocate(tx, e.card)
	}))
	mustOK(t, liability.CheckRepaymentCutover(e.db))
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("3000")) || !d.Summary.PendingCredit.IsZero() || !d.Plans[0].Terms[0].Remaining.Equal(dec("1000")) {
		t.Fatal(d.Summary, d.Plans)
	}
	var source model.Transaction
	mustOK(t, e.db.First(&source, old.ID).Error)
	if !source.Amount.Equal(dec("200")) {
		t.Fatal("legacy cash was rewritten")
	}
}

func TestOrdinaryTransferPaysCompleteTermsAcrossPlans(t *testing.T) {
	e := newLiabilityEnv(t)
	first, second := repaymentPlan(t, e), repaymentPlan(t, e)
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, AccountID: e.account, TransferAccountID: e.card, Amount: dec("500"), EntryDate: e.now})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("partial transfer accepted", err)
	}
	transfer, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, AccountID: e.account, TransferAccountID: e.card, Amount: dec("2040"), EntryDate: e.now})
	mustOK(t, err)
	if transfer.LiabilityOperationID == nil {
		t.Fatal("unprotected ordinary term payment")
	}
	d := e.state(t)
	for _, id := range []int64{first, second} {
		p, err := e.svcL.Plan(adminUser(), id)
		mustOK(t, err)
		if p.Terms[0].Status != "settled" || !p.Terms[0].Remaining.IsZero() || p.Terms[1].Status != "scheduled" {
			t.Fatal(p)
		}
	}
	if !d.Summary.TotalDebt.Equal(dec("4000")) || !d.Summary.PendingCredit.IsZero() {
		t.Fatal(d.Summary)
	}
	_, err = e.svcL.Reverse(adminUser(), *transfer.LiabilityOperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "核对两笔计划后撤销"})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("6040")) {
		t.Fatal("multi-plan reversal incomplete")
	}
}

func TestInstallmentRepaymentBlockedClosedAndForeignAccounts(t *testing.T) {
	t.Run("blocked-fee-cannot-change-date", func(t *testing.T) {
		e := newLiabilityEnv(t)
		id := repaymentPlan(t, e)
		mustClose(t, e.closing, adminUser(), "2026-10")
		e.now = vdate(t, "2026-11-01")
		_, err := e.svcL.EnsureDue(e.now)
		mustOK(t, err)
		_, err = e.svcL.InstallmentRepaymentPreview(adminUser(), id, InstallmentRepaymentInput{AllRemaining: true, FromAccountID: e.account, EntryDate: "2026-11-01"})
		if !errors.Is(err, liability.ErrConflict) {
			t.Fatal("blocked fee bypassed", err)
		}
	})
	t.Run("closed-repayment-date", func(t *testing.T) {
		e := newLiabilityEnv(t)
		id := repaymentPlan(t, e)
		mustClose(t, e.closing, adminUser(), "2026-10")
		_, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, InstallmentRepaymentInput{AllRemaining: true, FromAccountID: e.account, EntryDate: "2026-10-15"})
		if !errors.Is(err, ErrTxnClosedPeriod) {
			t.Fatal(err)
		}
	})
	t.Run("foreign-and-fund-source", func(t *testing.T) {
		e := newLiabilityEnv(t)
		id := repaymentPlan(t, e)
		owner := int64(3)
		foreign := model.Account{Name: "他人资产", Type: model.AccountAsset, OwnerID: &owner, Status: model.StatusActive}
		mustOK(t, e.db.Create(&foreign).Error)
		in := InstallmentRepaymentInput{AllRemaining: true, FromAccountID: foreign.ID, EntryDate: "2026-10-15"}
		if _, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in); !errors.Is(err, ErrTxnForbidden) {
			t.Fatal(err)
		}
		mustOK(t, e.db.Table("accounts").Where("id = ?", e.account).Update("asset_class", "fund").Error)
		in.FromAccountID = e.account
		if _, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in); !errors.Is(err, ErrTxnAccountInvalid) {
			t.Fatal("fund used as cash", err)
		}
	})
}

func TestAutomaticLiabilityFundsFailAtomicallyAndKeepReason(t *testing.T) {
	t.Run("recurring-income", func(t *testing.T) {
		e := newLiabilityEnv(t)
		mustOK(t, e.db.AutoMigrate(&model.RecurringRule{}))
		category := model.Category{OwnerID: testOwner(1), Name: "周期收入", Kind: model.KindIncome}
		mustOK(t, e.db.Create(&category).Error)
		rule := model.RecurringRule{AccountID: e.card, CategoryID: category.ID, Amount: dec("50"), CreatedBy: 1, Freq: model.FreqMonthly, StartDate: e.now, NextRunDate: e.now, Status: model.RuleEnabled}
		mustOK(t, e.db.Create(&rule).Error)
		svc := NewRecurringService(RuleDeps{Rules: repository.NewRecurringRepo(e.db), Dates: repository.NewTransactionRepo(e.db), Txns: e.txn, Accounts: repository.NewAccountRepo(e.db), Cats: repository.NewCategoryRepo(e.db)})
		generated, err := svc.EnsureDue(e.now)
		if generated != 0 || !errors.Is(err, liability.ErrConflict) {
			t.Fatal(generated, err)
		}
		var stored model.RecurringRule
		mustOK(t, e.db.First(&stored, rule.ID).Error)
		if stored.BlockedReason == "" || !stored.NextRunDate.Equal(rule.NextRunDate) || len(e.state(t).Transactions) != 0 {
			t.Fatal("failed income lost reason or moved money", stored)
		}
	})
	t.Run("voucher-refund", func(t *testing.T) {
		e := newLiabilityEnv(t)
		input := e.input(t, "100", 1, "2026-10-20")
		input.AccountID = e.card
		v, err := e.svc.Create(adminUser(), input)
		mustOK(t, err)
		e.now = vdate(t, "2026-11-01")
		_, err = e.svcL.EnsureDue(e.now)
		mustOK(t, err)
		e.pay(t, "100", "2026-11-01")
		result, err := e.svc.EnsureDue(e.now)
		mustOK(t, err)
		var stored model.Voucher
		mustOK(t, e.db.First(&stored, v.ID).Error)
		if result.Generated != 0 || result.Failed != 1 || stored.LastError == "" || len(e.state(t).Transactions) != 2 || !e.state(t).Summary.TotalDebt.IsZero() {
			t.Fatal(result, stored.LastError)
		}
	})
}

func TestLiabilityPaidSourceMutationCannotLeaveFunds(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	paid := e.pay(t, "500", "2026-11-01")
	if _, err := e.txn.Update(adminUser(), source.ID, TxnInput{Kind: model.TxnExpense, AccountID: e.card, CategoryID: e.category, Amount: dec("400"), EntryDate: source.EntryDate}); !errors.Is(err, liability.ErrConflict) {
		t.Fatal("reduced paid debt accepted", err)
	}
	if err := e.txn.Delete(adminUser(), source.ID); !errors.Is(err, liability.ErrConflict) {
		t.Fatal("removed paid debt accepted", err)
	}
	if _, err := e.txn.Update(adminUser(), paid.TransactionIDs[0], TxnInput{Kind: model.TxnTransfer, AccountID: e.account, TransferAccountID: e.card, Amount: dec("600"), EntryDate: e.now}); !errors.Is(err, liability.ErrConflict) {
		t.Fatal("excess changed payment accepted", err)
	}
	mustOK(t, e.txn.Delete(adminUser(), paid.TransactionIDs[0]))
	if !e.state(t).Summary.TotalDebt.Equal(dec("500")) {
		t.Fatal("normal repayment deletion did not restore debt")
	}
}

func TestHistoricalRepaymentReverseRespectsSuccessor(t *testing.T) {
	e := newLiabilityEnv(t)
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("200"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 2, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	old := historicalPayment(t, e, "20", "2026-11-01")
	settled, err := e.svcL.Settle(adminUser(), created.PlanID, settlementRequest(t, e, created.PlanID))
	mustOK(t, err)
	_, err = e.svcL.Reverse(adminUser(), old.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "尝试绕过后继保护"})
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal(err)
	}
	_, err = e.svcL.Reverse(adminUser(), settled.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "先撤销后继"})
	mustOK(t, err)
	_, err = e.svcL.Reverse(adminUser(), old.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "再撤销旧还款"})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("200")) {
		t.Fatal("old payment not restored")
	}
}

func TestInstallmentReverseRestoresDueFeesWithoutMovingClosedDate(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	in, _ := quotedRepayment(t, e, id, 2)
	paid, err := e.svcL.RepayInstallment(adminUser(), id, in)
	mustOK(t, err)
	if len(paid.PostingSnapshots) != 3 {
		t.Fatal("financial history lacks immutable postings", paid)
	}
	mustClose(t, e.closing, adminUser(), "2026-11")
	e.now = vdate(t, "2026-12-01")
	request := ReverseInput{RequestKey: uuid.NewString(), Reason: "撤销提前还款，保留原收费日"}
	reversed, err := e.svcL.Reverse(adminUser(), paid.OperationID, request)
	mustOK(t, err)
	again, err := e.svcL.Reverse(adminUser(), paid.OperationID, request)
	mustOK(t, err)
	if again.OperationID != reversed.OperationID {
		t.Fatal("repeated reversal duplicated effects")
	}
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	p := e.state(t).Plans[0]
	if p.Terms[0].FeeStatus != "posted" || p.Terms[1].FeeStatus != "blocked" || p.Terms[1].FeeTxnID != nil || p.Terms[1].FeeDate != "2026-11-30" {
		t.Fatal(p)
	}
}
