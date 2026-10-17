package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

// Called only from the isolated random database created by TestLiabilityMySQL.
func checkOrdinaryAdvanceMySQL(t *testing.T, db *gorm.DB) {
	e := &liabilityEnv{voucherEnv: seedVoucherEnv(t, db), now: vdate(t, "2026-10-15")}
	e.svcL = NewLiabilityService(db, e.txn)
	e.svcL.Now = func() time.Time { return e.now }
	configureSettingsTestAccount(t, e, 31, 10, 1)
	e.spend(t, "500", "2026-10-15")
	in := quoteAdvance(t, e, advanceInput(e, "300", "2026-10-15"))
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, err := e.svcL.Repay(adminUser(), in); results <- err }()
	}
	close(start)
	for i := 0; i < 2; i++ {
		mustOK(t, <-results)
	}
	d := e.state(t)
	if !d.Summary.Unbilled.Equal(dec("200")) || len(d.Credits) != 1 {
		t.Fatal("same attempt duplicated", d.Summary)
	}
	// Two distinct requests confirmed against the same capacity: exactly one commits.
	in = quoteAdvance(t, e, advanceInput(e, "150", "2026-10-15"))
	start = make(chan struct{})
	results = make(chan error, 2)
	for i := 0; i < 2; i++ {
		request := in
		request.RequestKey = uuid.NewString()
		go func() { <-start; _, err := e.svcL.Repay(adminUser(), request); results <- err }()
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if !errors.Is(err, liability.ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 || !e.state(t).Summary.Unbilled.Equal(dec("50")) {
		t.Fatal("concurrent advances overpaid", success)
	}
	// The persisted fixed mode survives a new service instance and repeated replay.
	e.svcL = NewLiabilityService(db, e.txn)
	e.svcL.Now = func() time.Time { return e.now }
	mustOK(t, liability.Reallocate(db, e.card))
	if !e.state(t).Summary.Unbilled.Equal(dec("50")) {
		t.Fatal("fixed targets lost on reload")
	}
	in = quoteAdvance(t, e, advanceInput(e, "50", "2026-10-15"))
	e.now = vdate(t, "2026-11-01")
	start = make(chan struct{})
	results = make(chan error, 2)
	go func() { <-start; _, err := e.svcL.EnsureDue(e.now); results <- err }()
	go func() { <-start; _, err := e.svcL.Repay(adminUser(), in); results <- err }()
	close(start)
	for i := 0; i < 2; i++ {
		err := <-results
		if err != nil && !errors.Is(err, liability.ErrConflict) {
			t.Fatal(err)
		}
	}
	d = e.state(t)
	if !d.Summary.TotalDebt.IsZero() && !d.Summary.TotalDebt.Equal(dec("50")) {
		t.Fatal("issuance race altered debt", d.Summary)
	}
	for _, item := range d.Items {
		if item.Remaining.IsNegative() {
			t.Fatal(item)
		}
	}
	// A failing SQL posting cannot leave its repayment operation or allocations.
	configureSettingsTestAccount(t, e, 31, 10, 1)
	e.spend(t, "100", "2026-11-01")
	in = quoteAdvance(t, e, advanceInput(e, "50", "2026-11-01"))
	fault := errors.New("mysql advance posting fault")
	mustOK(t, db.Callback().Create().Before("gorm:create").Register("mysql-advance-fault", func(tx *gorm.DB) {
		if source, ok := tx.Statement.Dest.(*model.Transaction); ok && source.Kind == model.TxnTransfer {
			tx.AddError(fault)
		}
	}))
	_, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, db.Callback().Create().Remove("mysql-advance-fault"))
	if !errors.Is(err, fault) {
		t.Fatal(err)
	}
	d = e.state(t)
	if len(d.Credits) != 0 || !d.Summary.TotalDebt.Equal(dec("100")) {
		t.Fatal("mysql failure committed partial funds")
	}
}
