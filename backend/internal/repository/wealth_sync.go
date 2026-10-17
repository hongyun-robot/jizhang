package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"time"
)

var ErrWealthLease = errors.New("wealth collection lease expired")

func (r *WealthRepo) TrackedProducts() ([]model.WealthProduct, error) {
	rows := []model.WealthProduct{}
	query := `SELECT DISTINCT i.* FROM wealth_products i JOIN wealth_positions p ON p.product_id=i.id
 WHERE p.rebuild_pending=TRUE OR EXISTS(SELECT 1 FROM wealth_events e WHERE e.position_id=p.id AND e.voided_at IS NULL GROUP BY e.position_id
 HAVING SUM(CASE WHEN e.kind IN ('redeem','confirm_redeem') THEN -e.shares WHEN e.kind IN ('opening','buy','reinvest','confirm_buy') THEN e.shares ELSE 0 END)>0
 OR SUM(CASE WHEN e.kind='redeem' AND e.cash_date IS NULL THEN e.amount-e.fee WHEN e.kind='cash_dividend' AND e.cash_date IS NULL THEN e.amount WHEN e.kind='settlement' THEN -e.amount ELSE 0 END)>0
 OR SUM(CASE WHEN e.kind='subscribe' THEN e.amount WHEN e.kind IN ('confirm_buy','refund_buy') THEN -e.amount ELSE 0 END)>0
 )
	`
	if r.db.Migrator().HasTable(&model.WealthCashLink{}) {
		query += ` UNION SELECT DISTINCT i.* FROM wealth_products i JOIN wealth_cash_links c ON c.product_id=i.id WHERE c.active=TRUE`
	}
	err := r.db.Raw(query + ` ORDER BY id`).Scan(&rows).Error
	return rows, err
}
func (r *WealthRepo) SlotCursor(product int64) (*time.Time, error) {
	var row model.WealthSyncSlot
	err := r.db.Where("product_id = ?", product).Order("slot DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row.Slot, err
}
func (r *WealthRepo) ClaimCollection(product int64, now time.Time, token string, slots []time.Time, scheduled bool) (bool, error) {
	claimed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		p, err := r.At(tx).Product(product, true)
		if err != nil {
			return err
		}
		if p == nil {
			return gorm.ErrRecordNotFound
		}
		if p.LeaseUntil != nil && p.LeaseUntil.After(now) {
			return nil
		}
		if p.NextAttempt != nil && p.NextAttempt.After(now) {
			return nil
		}
		if p.LastAttempt != nil && now.Sub(*p.LastAttempt) < 60*time.Second {
			return nil
		}
		if scheduled {
			pending := []model.WealthSyncSlot{}
			if err = tx.Where("product_id = ? AND status = ?", product, "running").Order("slot").Find(&pending).Error; err != nil {
				return err
			}
			for _, slot := range pending {
				slots = append(slots, slot.Slot)
			}
			if len(slots) == 0 {
				return nil
			}
			// Recheck each durable slot after waiting for the product lock. Multiple
			// workers can have computed the same cursor before the first committed.
			active := []time.Time{}
			for _, slot := range slots {
				var old model.WealthSyncSlot
				err = tx.Where("product_id = ? AND slot = ?", product, slot).First(&old).Error
				if err == nil && old.Status != "running" {
					continue
				}
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				active = append(active, slot)
			}
			if len(active) == 0 {
				return nil
			}
			latest := active[0]
			for _, slot := range active {
				if slot.After(latest) {
					latest = slot
				}
			}
			for _, slot := range active {
				row := model.WealthSyncSlot{ProductID: product, Slot: slot, Status: "running", LeaseToken: token}
				if !slot.Equal(latest) {
					row.MergedInto = &latest
				}
				if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "product_id"}, {Name: "slot"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "lease_token", "merged_into"})}).Create(&row).Error; err != nil {
					return err
				}
			}
		}
		claimed = true
		return tx.Model(p).Updates(map[string]any{"lease_token": token, "lease_until": now.Add(90 * time.Second), "last_attempt": now, "next_attempt": now.Add(60 * time.Second), "sync_status": "refreshing"}).Error
	})
	return claimed && err == nil, err
}
func (r *WealthRepo) FinishCollection(product int64, token string, now time.Time, success, changed bool, message string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		p, err := r.At(tx).Product(product, true)
		if err != nil {
			return err
		}
		if p == nil || p.LeaseToken != token {
			return ErrWealthLease
		}
		status := "failed"
		updates := map[string]any{"lease_until": nil, "lease_token": "", "last_error": message, "failure_count": p.FailureCount + 1}
		if success {
			status = "no_new_nav"
			if changed {
				status = "updated"
			}
			updates["last_success"] = now
			updates["failure_count"] = 0
			updates["last_error"] = ""
		}
		updates["sync_status"] = status
		if err = tx.Model(p).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(&model.WealthSyncSlot{}).Where("product_id = ? AND lease_token = ? AND status = ?", product, token, "running").Updates(map[string]any{"status": status, "completed_at": now}).Error
	})
}
