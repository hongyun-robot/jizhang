package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestLiabilityRepaymentDayUpdatesUnissuedStatement(t *testing.T) {
	e := newLiabilityEnv(t)
	checkRepaymentDayUpdatesUnissuedStatement(t, e)
}

func configureSettingsTestAccount(t *testing.T, e *liabilityEnv, day, due, offset int) {
	t.Helper()
	owner := adminUser().ID
	card := model.Account{Name: "还款日期测试", Type: model.AccountLiability, OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, e.db.Create(&card).Error)
	e.card = card.ID
	_, err := e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: day, RepaymentDay: due, RepaymentMonthOffset: offset})
	mustOK(t, err)
}

func checkRepaymentDayUpdatesUnissuedStatement(t *testing.T, e *liabilityEnv) {
	t.Helper()
	configureSettingsTestAccount(t, e, 31, 1, 1)
	e.spend(t, "2.30", "2026-10-15")
	before := e.state(t)
	if len(before.Statements) != 1 || before.Statements[0].IssuedAt != nil || before.Statements[0].DueDate != "2026-11-01" {
		t.Fatal("unexpected reproduction fixture", before.Statements)
	}
	rule, err := e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 8, RepaymentMonthOffset: 1})
	mustOK(t, err)
	after := e.state(t)
	if len(after.Statements) != 1 {
		t.Fatal("repayment day change created duplicate statements", after.Statements)
	}
	if after.Statements[0].DueDate != "2026-11-08" {
		t.Fatalf("repayment day change left existing unissued statement due on %s; want 2026-11-08", after.Statements[0].DueDate)
	}
	if after.Statements[0].ID != before.Statements[0].ID || after.Statements[0].CycleStart != before.Statements[0].CycleStart || after.Statements[0].CycleEnd != before.Statements[0].CycleEnd || !after.Summary.TotalDebt.Equal(before.Summary.TotalDebt) {
		t.Fatal("repayment day change altered statement identity or debt", before, after)
	}
	if rule.EffectiveFrom != "2026-10-01" || *after.Account.RepaymentDay != 8 {
		t.Fatal("repayment rule did not take effect for the current cycle", rule, after.Account)
	}
	var audit struct {
		Statements []model.LiabilityStatement `json:"statements"`
	}
	mustOK(t, json.Unmarshal([]byte(after.Operations[0].BeforeJSON), &audit))
	if len(audit.Statements) != 1 || audit.Statements[0].DueDate != "2026-11-01" {
		t.Fatal("old repayment date missing from audit", audit)
	}
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	after = e.state(t)
	if after.Summary.DueDate != "2026-11-08" || !after.Summary.CurrentDue.Equal(dec("2.30")) || !after.Summary.Overdue.IsZero() {
		t.Fatal("billing did not use the updated repayment date", after.Summary)
	}
}

func TestLiabilityAccountUpdateSynchronizesRepaymentDay(t *testing.T) {
	e := newLiabilityEnv(t)
	// The account form uses AccountService, whose clock is the wall clock.
	// Keep its fixture relative to today so the cycle assertion is stable.
	e.now = time.Now()
	configureSettingsTestAccount(t, e, 31, 1, 1)
	e.spend(t, "2.30", liability.Date(e.now))
	before := e.state(t)
	day, repayment, offset := 31, 8, 1
	accounts := NewAccountService(repository.NewAccountRepo(e.db))
	_, err := accounts.UpdateFor(adminUser(), e.card, CreateAccountInput{Name: "还款日期测试", Type: model.AccountLiability, LiabilityType: "huabei", StatementDay: &day, RepaymentDay: &repayment, RepaymentMonthOffset: &offset}, model.StatusActive)
	mustOK(t, err)
	after := e.state(t)
	end, err := liability.Parse(before.Statements[0].CycleEnd)
	mustOK(t, err)
	want := end.AddDate(0, 0, 1).Format("2006-01") + "-08"
	if after.Statements[0].DueDate != want || *after.Account.RepaymentDay != 8 {
		t.Fatal("account settings form left the old repayment date", after.Statements, after.Account)
	}
}

func TestLiabilityRepaymentChangesPreserveIssuedBillsAndMoney(t *testing.T) {
	e := newLiabilityEnv(t)
	_, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	historicalPayment(t, e, "100", "2026-11-02")
	before := e.state(t)
	_, err = e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 8, RepaymentMonthOffset: 1})
	mustOK(t, err)
	after := e.state(t)
	for _, old := range before.Statements {
		found := false
		for _, bill := range after.Statements {
			if bill.ID != old.ID {
				continue
			}
			found = true
			if bill.CycleStart != old.CycleStart || bill.CycleEnd != old.CycleEnd || !bill.Amount.Equal(old.Amount) || !bill.PaidAmount.Equal(old.PaidAmount) {
				t.Fatal("repayment date change rewrote cycle or money", old, bill)
			}
			if old.IssuedAt != nil {
				if bill.DueDate != old.DueDate || !bill.SnapshotAmount.Equal(old.SnapshotAmount) || bill.RuleID != old.RuleID {
					t.Fatal("repayment date change rewrote issued history", old, bill)
				}
			} else if bill.DueDate[len(bill.DueDate)-2:] != "08" {
				t.Fatal("future installment retained old repayment date", bill)
			}
		}
		if !found {
			t.Fatal("statement was removed", old)
		}
	}
	oldMoney, err := json.Marshal([]any{before.Summary, before.Transactions, before.Plans, before.Allocations, before.Items})
	mustOK(t, err)
	newMoney, err := json.Marshal([]any{after.Summary, after.Transactions, after.Plans, after.Allocations, after.Items})
	mustOK(t, err)
	if string(oldMoney) != string(newMoney) {
		t.Fatal("repayment date change altered financial records or agreed fees")
	}
}

func TestLiabilityRepaymentChangeSupersedesLegacyPendingRule(t *testing.T) {
	e := newLiabilityEnv(t)
	checkRepaymentChangeSupersedesLegacyPendingRule(t, e)
}

func checkRepaymentChangeSupersedesLegacyPendingRule(t *testing.T, e *liabilityEnv) {
	t.Helper()
	configureSettingsTestAccount(t, e, 31, 10, 1)
	// Older versions scheduled repayment-only edits for the next cycle.
	legacy := model.LiabilityBillRule{AccountID: e.card, Version: 2, StatementDay: 31, RepaymentDay: 1, RepaymentMonthOffset: 1, EffectiveFrom: "2026-11-01"}
	mustOK(t, e.db.Create(&legacy).Error)
	_, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("300"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	_, err = e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 8, RepaymentMonthOffset: 1})
	mustOK(t, err)
	d := e.state(t)
	for _, bill := range d.Statements {
		if bill.DueDate[len(bill.DueDate)-2:] != "08" {
			t.Fatal("legacy pending rule retained a stale statement", bill)
		}
	}
	e.now = vdate(t, "2026-11-15")
	e.spend(t, "2.30", "2026-11-15")
	active, err := liability.RuleForDate(e.db, e.card, "2026-11-15")
	mustOK(t, err)
	if active.RepaymentDay != 8 || active.ID == legacy.ID {
		t.Fatal("legacy pending setting undid the repayment correction", active)
	}
}

func TestLiabilityRepaymentChangeCalendarRules(t *testing.T) {
	for _, tt := range []struct {
		name, date, want string
		day, due, offset int
	}{
		{"same-month", "2026-10-15", "2026-10-25", 20, 25, 0},
		{"short-month", "2027-01-15", "2027-02-28", 31, 31, 1},
		{"leap-year", "2028-01-15", "2028-02-29", 31, 31, 1},
		{"year-boundary", "2026-12-15", "2027-01-08", 31, 8, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newLiabilityEnv(t)
			e.now = vdate(t, tt.date)
			configureSettingsTestAccount(t, e, tt.day, 10, 1)
			e.spend(t, "2.30", tt.date)
			_, err := e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: tt.day, RepaymentDay: tt.due, RepaymentMonthOffset: tt.offset})
			mustOK(t, err)
			if bills := e.state(t).Statements; len(bills) != 1 || bills[0].DueDate != tt.want {
				t.Fatal("repayment change violated calendar rules", bills)
			}
		})
	}
}

func TestLiabilityRepaymentChangeFailureRollsBack(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "2.30", "2026-10-15")
	legacy := model.LiabilityBillRule{AccountID: e.card, Version: 2, StatementDay: 31, RepaymentDay: 1, RepaymentMonthOffset: 1, EffectiveFrom: "2026-11-01"}
	mustOK(t, e.db.Create(&legacy).Error)
	_, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("30"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	before, err := json.Marshal(e.state(t))
	mustOK(t, err)
	injected := errors.New("settings audit write failed")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("liability_settings_fault", func(db *gorm.DB) {
		if op, ok := db.Statement.Dest.(*model.LiabilityOperation); ok && op.Kind == "settings" {
			db.AddError(injected)
		}
	}))
	_, err = e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 8, RepaymentMonthOffset: 1})
	mustOK(t, e.db.Callback().Create().Remove("liability_settings_fault"))
	if !errors.Is(err, injected) {
		t.Fatal("expected settings failure", err)
	}
	after, err := json.Marshal(e.state(t))
	mustOK(t, err)
	if string(before) != string(after) {
		t.Fatal("failed settings change left partial account, rule, or statement changes")
	}
}
