package service

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
)

func TestWealthCashMySQLNewAccountConcurrentCreationAndRollback(t *testing.T) {
	e := newWealthMySQLEnv(t)
	mustOK(t, cashMigration(t, e, "000020_cmbcwm_wealth_product.up.sql"))
	mustOK(t, cashMigration(t, e, "000021_wealth_cash_accounts.up.sql"))
	s := NewAccountService(e.accounts)
	s.Now = e.svc.Now
	in := cashAccountInput(2)
	var before int64
	mustOK(t, e.db.Model(&model.Account{}).Count(&before).Error)
	var group sync.WaitGroup
	ids := make(chan int64, 8)
	failures := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			account, err := s.CreateFor(e.users[0], in)
			if err != nil {
				failures <- err
			} else {
				ids <- account.ID
			}
		}()
	}
	group.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var accountID int64
	for id := range ids {
		if accountID == 0 {
			accountID = id
		} else if accountID != id {
			t.Fatal("concurrent retry created more than one account", accountID, id)
		}
	}
	var count int64
	mustOK(t, e.db.Model(&model.Account{}).Count(&count).Error)
	if count != before+1 {
		t.Fatal(count, before)
	}
	for _, table := range []string{"wealth_cash_links", "wealth_cash_audits", "account_create_requests"} {
		if table == "wealth_cash_audits" { // audits identify accounts through the immutable link
			mustOK(t, e.db.Table("wealth_cash_audits a").Joins("JOIN wealth_cash_links l ON l.id=a.link_id").Where("l.account_id = ?", accountID).Count(&count).Error)
		} else {
			mustOK(t, e.db.Table(table).Where("account_id = ?", accountID).Count(&count).Error)
		}
		if count != 1 {
			t.Fatal(table, count)
		}
	}
	// Failure after inserting the account and link must also release its request
	// key, so a retry can complete without an orphan or duplicate account.
	mustOK(t, e.db.Exec("CREATE TRIGGER reject_cash_creation BEFORE INSERT ON wealth_cash_audits FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated cash creation failure'").Error)
	failed := cashAccountInput(2)
	failed.RequestKey = uuid.NewString()
	if _, err := s.CreateFor(e.users[0], failed); err == nil {
		t.Fatal("audit failure accepted")
	}
	mustOK(t, e.db.Model(&model.Account{}).Count(&count).Error)
	if count != before+1 {
		t.Fatal("failed create left account", count)
	}
	mustOK(t, e.db.Model(&model.AccountCreateRequest{}).Where("request_key = ?", failed.RequestKey).Count(&count).Error)
	if count != 0 {
		t.Fatal("failed request consumed key")
	}
	mustOK(t, e.db.Exec("DROP TRIGGER reject_cash_creation").Error)
	retry, err := s.CreateFor(e.users[0], failed)
	mustOK(t, err)
	if !retry.IsWealthCash() {
		t.Fatal(retry)
	}
}
