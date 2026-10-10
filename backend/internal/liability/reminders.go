package liability

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"time"
)

func RefreshReminders(db *gorm.DB, now time.Time, create bool) error {
	// Older test stores may intentionally contain only core tables.
	if !db.Migrator().HasTable(&model.Notification{}) {
		return nil
	}
	var bills []model.LiabilityStatement
	if e := db.Where("issued_at IS NOT NULL").Find(&bills).Error; e != nil {
		return e
	}
	var items []model.LiabilityDebtItem
	var credits []model.LiabilityCreditEvent
	if e := db.Where("active = ?", true).Find(&items).Error; e != nil {
		return e
	}
	if _, e := FillAmounts(db, items, credits); e != nil {
		return e
	}
	totals := map[int64]decimal.Decimal{}
	for _, d := range items {
		totals[d.StatementID] = totals[d.StatementID].Add(d.Remaining)
	}
	var accounts []model.Account
	if e := db.Where("billing_enabled = ?", true).Find(&accounts).Error; e != nil {
		return e
	}
	am := map[int64]model.Account{}
	for _, a := range accounts {
		am[a.ID] = a
	}
	today, _ := Parse(Date(now))
	for _, b := range bills {
		a, ok := am[b.AccountID]
		if !ok {
			continue
		}
		due, e := Parse(b.DueDate)
		if e != nil {
			return e
		}
		days := int(due.Sub(today).Hours() / 24)
		left := totals[b.ID]
		key := fmt.Sprintf("repayment_due:statement:%d", b.ID)
		var n model.Notification
		e = db.Where("dedup_key = ?", key).First(&n).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if n.ID == 0 && (!create || a.Status != model.StatusActive || days > 3 || !left.IsPositive()) {
			continue
		}
		initial := left.StringFixed(2)
		if n.ID != 0 {
			var prior map[string]any
			_ = json.Unmarshal([]byte(n.Payload), &prior)
			if v, ok := prior["initial_debt"].(string); ok {
				initial = v
			}
		}
		resolved := !left.IsPositive()
		payload, _ := json.Marshal(map[string]any{"dedup_key": key, "account_id": a.ID, "account_name": a.Name, "statement_id": b.ID, "debt": left.StringFixed(2), "initial_debt": initial, "repayment_date": b.DueDate, "days": days, "overdue": days < 0 && !resolved, "resolved": resolved})
		if n.ID == 0 {
			n = model.Notification{Kind: model.NotifRepaymentDue, DedupKey: &key, StatementID: &b.ID}
		}
		n.OwnerID = a.OwnerID
		if resolved {
			if n.ResolvedAt == nil {
				x := now
				n.ResolvedAt = &x
			}
		} else if n.ResolvedAt != nil && days <= 3 {
			n.ResolvedAt = nil
			n.ReadAt = nil
		}
		n.Payload = string(payload)
		if e := db.Save(&n).Error; e != nil {
			return e
		}
	}
	return nil
}
