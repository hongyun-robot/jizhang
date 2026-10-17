package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

func advanceInput(e *liabilityEnv, amount, date string) LiabilityRepaymentInput {
	return LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec(amount), EntryDate: date, RequestKey: uuid.NewString(), Mode: "auto", Scope: unbilledOrdinary}
}
func quoteAdvance(t *testing.T, e *liabilityEnv, in LiabilityRepaymentInput) LiabilityRepaymentInput {
	t.Helper()
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in)
	mustOK(t, err)
	in.QuoteSignature = q.QuoteSignature
	return in
}
func TestOrdinaryAdvanceLifecycle(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "1000", "2026-10-15")
	in := advanceInput(e, "300", "2026-10-15")
	before := e.state(t)
	in = quoteAdvance(t, e, in)
	if !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("preview wrote ledger state")
	}
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if r.RepaymentScope != unbilledOrdinary || len(r.RepaymentTargets) != 1 || r.RepaymentTargets[0].SourceID != source.ID {
		t.Fatal(r)
	}
	e.balance(t, "700")
	d := e.state(t)
	if !d.Summary.Unbilled.Equal(dec("700")) || len(d.Transactions) != 2 {
		t.Fatal(d)
	}
	again, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if again.OperationID != r.OperationID {
		t.Fatal("duplicate payment")
	}
	bad := in
	bad.Amount = dec("301")
	if _, err := e.svcL.Repay(adminUser(), bad); !errors.Is(err, liability.ErrConflict) {
		t.Fatal(err)
	}
	// Both later and backdated purchases must retain their own unpaid principal.
	e.spend(t, "20", "2026-10-14")
	e.spend(t, "30", "2026-10-15")
	for i := 0; i < 2; i++ {
		mustOK(t, liability.Reallocate(e.db, e.card))
	}
	d = e.state(t)
	for _, a := range d.Allocations {
		if d.Items[0].ID != a.DebtItemID {
			t.Fatal("payment moved to a new purchase", a)
		}
	}
	e.now = vdate(t, "2026-11-01")
	for i := 0; i < 2; i++ {
		_, err = e.svcL.EnsureDue(e.now)
		mustOK(t, err)
	}
	if !e.state(t).Summary.CurrentDue.Equal(dec("750")) {
		t.Fatal(e.state(t).Summary)
	}
	_, err = e.txn.Update(adminUser(), r.TransactionIDs[0], TxnInput{Kind: model.TxnTransfer, AccountID: e.account, TransferAccountID: e.card, Amount: dec("100"), EntryDate: vdate(t, "2026-10-15")})
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal("advance edited", err)
	}
	if err = e.txn.Delete(adminUser(), r.TransactionIDs[0]); !errors.Is(err, liability.ErrManaged) {
		t.Fatal("advance deleted", err)
	}
	_, err = e.svcL.Allocate(adminUser(), r.TransactionIDs[0], LiabilityAllocationInput{RequestKey: uuid.NewString(), Mode: "auto"})
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal("advance reallocated", err)
	}
	reverse := ReverseInput{RequestKey: uuid.NewString(), Reason: "撤销测试"}
	_, err = e.svcL.Reverse(adminUser(), r.OperationID, reverse)
	mustOK(t, err)
	_, err = e.svcL.Reverse(adminUser(), r.OperationID, reverse)
	mustOK(t, err)
	e.balance(t, "1000")
	if !e.state(t).Summary.CurrentDue.Equal(dec("1050")) {
		t.Fatal(e.state(t).Summary)
	}
}

func TestOrdinaryAdvanceTargetsAndDates(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "100", "2026-10-10")
	e.spend(t, "200", "2026-10-15")
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), advanceInput(e, "50", "2026-10-12"))
	mustOK(t, err)
	if !q.AvailableAmount.Equal(dec("100")) {
		t.Fatal(q)
	}
	for _, in := range []LiabilityRepaymentInput{advanceInput(e, "101", "2026-10-12"), advanceInput(e, "1", "2026-10-16"), advanceInput(e, "0", "2026-10-15")} {
		if _, err = e.svcL.OrdinaryRepaymentPreview(adminUser(), in); err == nil {
			t.Fatal("invalid date/amount accepted", in)
		}
	}
	id := repaymentPlan(t, e)
	_ = id
	in := advanceInput(e, "300", "2026-10-15")
	q, err = e.svcL.OrdinaryRepaymentPreview(adminUser(), in)
	mustOK(t, err)
	if !q.AvailableAmount.Equal(dec("300")) {
		t.Fatal("installments counted", q)
	}
	in.Mode = "manual"
	in.Targets = []liability.Target{{StatementID: q.Bills[0].StatementID, Amount: dec("301")}}
	in.Amount = dec("301")
	if _, err = e.svcL.OrdinaryRepaymentPreview(adminUser(), in); err == nil {
		t.Fatal("mixed bill included installment")
	}
	in.Amount = dec("300")
	in.Targets[0].Amount = dec("300")
	in = quoteAdvance(t, e, in)
	_, err = e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	q, err = e.svcL.OrdinaryRepaymentPreview(adminUser(), advanceInput(e, "1", "2026-10-15"))
	if err == nil || !q.AvailableAmount.IsZero() {
		t.Fatal("only installments should have no capacity", q, err)
	}
}

func TestOrdinaryAdvanceStaleAndAtomic(t *testing.T) {
	for _, change := range []string{"refund", "issue", "fault"} {
		t.Run(change, func(t *testing.T) {
			e := newLiabilityEnv(t)
			source := e.spend(t, "500", "2026-10-15")
			in := quoteAdvance(t, e, advanceInput(e, "300", "2026-10-15"))
			if change == "refund" {
				_, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.card, RelatedTxnID: source.ID, Amount: dec("100"), EntryDate: vdate(t, "2026-10-15")})
				mustOK(t, err)
			}
			if change == "issue" {
				e.now = vdate(t, "2026-11-01")
				_, err := e.svcL.EnsureDue(e.now)
				mustOK(t, err)
			}
			if change == "fault" {
				mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("advance-fault", func(tx *gorm.DB) {
					if x, ok := tx.Statement.Dest.(*model.Transaction); ok && x.Kind == model.TxnTransfer {
						tx.AddError(errors.New("injected advance failure"))
					}
				}))
				defer e.db.Callback().Create().Remove("advance-fault")
			}
			before := e.state(t)
			if _, err := e.svcL.Repay(adminUser(), in); err == nil {
				t.Fatal("stale/failed advance accepted")
			}
			if !reflect.DeepEqual(before, e.state(t)) {
				t.Fatal("failed advance changed ledger")
			}
		})
	}
}

func TestOrdinaryAdvanceMutationAndRefundProtection(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "500", "2026-10-15")
	in := quoteAdvance(t, e, advanceInput(e, "300", "2026-10-15"))
	_, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	for _, test := range []struct{ amount, date string }{{"200", "2026-10-15"}, {"500", "2026-10-16"}} {
		_, err = e.txn.Update(adminUser(), source.ID, TxnInput{Kind: model.TxnExpense, AccountID: e.card, CategoryID: e.category, Amount: dec(test.amount), EntryDate: vdate(t, test.date)})
		if err == nil {
			t.Fatal("invalid paid source mutation accepted")
		}
	}
	if err = e.txn.Delete(adminUser(), source.ID); err == nil {
		t.Fatal("paid source deleted")
	}
	_, err = e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, SourceTxnID: source.ID, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	if err == nil {
		t.Fatal("paid source converted and lost fixed target")
	}
	_, err = e.txn.Update(adminUser(), source.ID, TxnInput{Kind: model.TxnExpense, AccountID: e.card, CategoryID: e.category, Amount: dec("600"), EntryDate: vdate(t, "2026-10-15")})
	mustOK(t, err)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.card, RelatedTxnID: source.ID, Amount: dec("300"), EntryDate: vdate(t, "2026-10-15")})
	mustOK(t, err)
	before := e.state(t)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.card, RelatedTxnID: source.ID, Amount: dec("1"), EntryDate: vdate(t, "2026-10-15")})
	if err == nil || !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("refund displaced fixed advance", err)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.account, RelatedTxnID: source.ID, Amount: dec("1"), EntryDate: vdate(t, "2026-10-15")})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal("asset refund affected liability")
	}
}

func TestOrdinaryAdvanceAuthorizationAndClosedPeriods(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	in := advanceInput(e, "100", "2026-10-15")
	if _, err := e.svcL.OrdinaryRepaymentPreview(&model.User{ID: 2, Role: model.RoleViewer}, in); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	foreign := model.Account{OwnerID: testOwner(3), Name: "foreign", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.db.Create(&foreign).Error)
	in.FromAccountID = foreign.ID
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	in.FromAccountID = e.account
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.account).Update("asset_class", "fund").Error)
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in); !errors.Is(err, ErrTxnAccountInvalid) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.account).Update("asset_class", "cash").Error)
	mustClose(t, e.closing, adminUser(), "2026-10")
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
	e.now = vdate(t, "2026-11-01")
	in.EntryDate = "2026-11-01"
	in = quoteAdvance(t, e, in)
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	mustClose(t, e.closing, adminUser(), "2026-11")
	if _, err := e.svcL.Reverse(adminUser(), r.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "closed"}); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
}

func TestOrdinaryAdvanceLegacyDigestAndScope(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	in := advanceInput(e, "100", "2026-10-15")
	in.Scope = "issued"
	if _, err := e.svcL.Repay(adminUser(), in); err == nil {
		t.Fatal("default used unbilled principal")
	}
	in.Scope = "invalid"
	if _, err := e.svcL.Repay(adminUser(), in); err == nil {
		t.Fatal("invalid scope")
	}
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	in.Scope = ""
	in.EntryDate = "2026-11-01"
	in.RequestKey = uuid.NewString()
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	// Exact pre-upgrade field order and JSON tags, without optional new fields.
	legacy := struct {
		AccountID     int64              `json:"account_id"`
		FromAccountID int64              `json:"from_account_id"`
		Amount        decimal.Decimal    `json:"amount"`
		EntryDate     string             `json:"entry_date"`
		RequestKey    string             `json:"request_key"`
		Mode          string             `json:"mode"`
		Targets       []liability.Target `json:"targets"`
		Reason        string             `json:"reason"`
	}{in.AccountID, in.FromAccountID, in.Amount, in.EntryDate, in.RequestKey, in.Mode, in.Targets, in.Reason}
	raw, err := json.Marshal(legacy)
	mustOK(t, err)
	sum := sha256.Sum256(append([]byte("repayment:"), raw...))
	var op model.LiabilityOperation
	mustOK(t, e.db.First(&op, r.OperationID).Error)
	if op.Digest != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy digest changed")
	}
	in.Scope = "issued"
	again, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if again.OperationID != r.OperationID {
		t.Fatal("legacy retry duplicated")
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, AccountID: e.account, TransferAccountID: e.card, Amount: dec("401"), EntryDate: vdate(t, "2026-11-01")})
	if err == nil {
		t.Fatal("legacy transfer retained surplus")
	}
}

func TestOrdinaryAdvanceManualBillsAndRules(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "200", "2026-10-10")
	second := e.spend(t, "500", "2026-10-15")
	// A real purchase can be assigned to a later statement without changing its date.
	later, err := liability.StatementFor(e.db, e.card, "2026-11-15")
	mustOK(t, err)
	mustOK(t, e.db.Model(&model.LiabilityDebtItem{}).Where("source_id = ?", second.ID).Updates(map[string]any{"statement_id": later.ID, "manual_assignment": true}).Error)
	in := advanceInput(e, "100", "2026-10-15")
	in.Mode = "manual"
	in.Targets = []liability.Target{{StatementID: later.ID, Amount: dec("100")}}
	in = quoteAdvance(t, e, in)
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if r.RepaymentTargets[0].SourceID != second.ID {
		t.Fatal("manual future bill was ignored", r)
	}
	_, err = e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 8, RepaymentMonthOffset: 1})
	mustOK(t, err)
	mustOK(t, liability.Reallocate(e.db, e.card))
	d := e.state(t)
	if !d.Summary.Unbilled.Equal(dec("600")) {
		t.Fatal(d.Summary)
	}
	for _, b := range d.Statements {
		if b.ID == later.ID && !b.Remaining.Equal(dec("400")) {
			t.Fatal(b)
		}
	}
	bad := advanceInput(e, "200", "2026-10-15")
	bad.Mode = "manual"
	bad.Targets = []liability.Target{{StatementID: later.ID, Amount: dec("100")}, {StatementID: later.ID, Amount: dec("100")}}
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), bad); err == nil {
		t.Fatal("duplicate target accepted")
	}
	bad.Targets = []liability.Target{{DebtItemID: d.Items[0].ID, Amount: dec("200")}}
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), bad); err == nil {
		t.Fatal("client debt item target accepted")
	}
	// Moving a paid purchase between ordinary bills keeps its fixed consumption target.
	mustOK(t, e.db.Model(&model.LiabilityDebtItem{}).Where("source_id = ?", second.ID).Update("statement_id", d.Items[0].StatementID).Error)
	mustOK(t, liability.Reallocate(e.db, e.card))
	if !e.state(t).Summary.Unbilled.Equal(dec("600")) {
		t.Fatal("bill move lost repayment")
	}
}

func TestOrdinaryAdvanceVoucherAndTransferCandidates(t *testing.T) {
	e := newLiabilityEnv(t)
	v := e.input(t, "100", 2, "")
	v.AccountID = e.card
	created, err := e.svc.Create(adminUser(), v)
	mustOK(t, err)
	e.op(t, created.ID, model.VoucherRedeem, "2026-10-15", 1)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, AccountID: e.card, TransferAccountID: e.account, Amount: dec("30"), EntryDate: vdate(t, "2026-10-15")})
	mustOK(t, err)
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), advanceInput(e, "100", "2026-10-15"))
	mustOK(t, err)
	if !q.AvailableAmount.Equal(dec("100")) || len(q.Allocations) != 1 {
		t.Fatal("non-consumption or redemption counted", q)
	}
}
