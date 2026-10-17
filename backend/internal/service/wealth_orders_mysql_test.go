package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
)

func TestWealthMySQLPendingOrderMigrationGuardAndOldEvents(t *testing.T) {
	e := newWealthMySQLEnv(t)
	opening := e.opening(t, 0, "10", "1", "2026-10-09")
	sqlFile := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		mustOK(t, err)
		return string(b)
	}
	mustOK(t, e.db.Exec(sqlFile("000019_wealth_pending_orders.down.sql")).Error)
	if e.db.Migrator().HasColumn(&model.WealthEvent{}, "order_json") {
		t.Fatal("empty order down failed")
	}
	mustOK(t, e.db.Exec(sqlFile("000019_wealth_pending_orders.up.sql")).Error)
	var preserved model.WealthEvent
	mustOK(t, e.db.First(&preserved, opening.EventID).Error)
	if preserved.Shares.String() != "10" || preserved.OrderJSON != "" {
		t.Fatal(preserved)
	}
	root := e.subscription(t, "100", "2026-10-16", "09:00:00")
	if err := e.db.Exec(sqlFile("000019_wealth_pending_orders.down.sql")).Error; err == nil {
		t.Fatal("financial order metadata destroyed")
	}
	preserved = model.WealthEvent{}
	mustOK(t, e.db.First(&preserved, root.EventID).Error)
	if !e.db.Migrator().HasColumn(&model.WealthEvent{}, "order_json") {
		t.Fatal("guard ran after destructive DDL")
	}
}
func TestWealthMySQLPendingConfirmationCancellationAndRefundConcurrency(t *testing.T) {
	e := newWealthMySQLEnv(t)
	root := e.subscription(t, "100", "2026-10-15", "09:00:00")
	e.quote(t, "2026-10-15", "1")
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, kind := range []string{"confirm_buy", "cancel_buy"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			<-start
			in := orderInput(kind, "2026-10-16")
			in.SourceEventID = root.EventID
			if kind == "confirm_buy" {
				in.Shares = "100"
			}
			_, err := e.svc.Operate(context.Background(), e.users[0], root.PositionID, in)
			results <- err
		}(kind)
	}
	close(start)
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, ErrWealthConflict) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal(won)
	}
	second := e.subscription(t, "200", "2026-10-16", "10:00:00")
	cancel := orderInput("cancel_buy", "2026-10-16")
	cancel.SourceEventID = second.EventID
	cancelled := e.order(t, root.PositionID, cancel)
	refund := orderInput("refund_buy", "2026-10-16")
	refund.SourceEventID = cancelled.EventID
	refund.CashAccountID = e.banks[0]
	refund.Amount = "200"
	start = make(chan struct{})
	results = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.svc.Operate(context.Background(), e.users[0], root.PositionID, refund)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		mustOK(t, err)
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthEvent{}).Where("kind=? AND source_event_id=?", "refund_buy", cancelled.EventID).Count(&n).Error)
	if n != 1 || e.bank(t, 0) != "4900.00" {
		t.Fatal(n, e.bank(t, 0))
	}
	refund.RequestKey = uuid.NewString()
	if _, err := e.svc.Operate(context.Background(), e.users[0], root.PositionID, refund); !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
}
func TestWealthMySQLPendingAtomicFailureAndOwnerRoleBoundaries(t *testing.T) {
	e := newWealthMySQLEnv(t)
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("pending_order_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "wealth_audits" {
			tx.AddError(errors.New("injected audit failure"))
		}
	}))
	in := orderInput("subscribe", "2026-10-16")
	in.TradeDate = in.EffectiveDate
	in.ApplicationTime = "09:00:00"
	in.Amount = "500"
	in.CashAccountID = e.banks[0]
	if _, err := e.svc.Subscribe(context.Background(), e.users[0], e.wealths[0], in); err == nil {
		t.Fatal("failure committed")
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("partial cash debit")
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthEvent{}).Count(&n).Error)
	if n != 0 {
		t.Fatal(n)
	}
	mustOK(t, e.db.Callback().Create().Remove("pending_order_failure"))
	root, err := e.svc.Subscribe(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	bad := orderInput("cancel_buy", "2026-10-16")
	bad.SourceEventID = root.EventID
	if _, err = e.svc.Operate(context.Background(), e.users[1], root.PositionID, bad); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	in.RequestKey = uuid.NewString()
	in.CashAccountID = e.banks[2]
	if _, err = e.svc.Subscribe(context.Background(), e.users[2], e.wealths[2], in); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
}
