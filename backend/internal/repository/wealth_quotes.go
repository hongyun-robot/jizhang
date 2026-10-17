package repository

import (
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

// SaveNAVBatch is atomic and never deletes previous public history. Callers
// validate the entire source batch before entering this transaction.
func (r *WealthRepo) SaveNAVBatch(product int64, rows []model.WealthNAV, now time.Time, lease ...string) ([]int64, bool, error) {
	owners := []int64{}
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		bound := r.At(tx)
		p, err := bound.Product(product, true)
		if err != nil {
			return err
		}
		if p == nil {
			return gorm.ErrRecordNotFound
		}
		if len(lease) > 0 && (p.LeaseToken != lease[0] || p.LeaseUntil == nil || p.LeaseUntil.Before(now)) {
			return ErrWealthLease
		}
		for _, row := range rows {
			var old model.WealthNAV
			lower, upper := WealthDateBounds(row.NAVDate)
			err := tx.Where("product_id = ? AND nav_date >= ? AND nav_date < ?", product, lower, upper).First(&old).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			row.ProductID = product
			if err == nil {
				equal := old.UnitNAV.Equal(row.UnitNAV) && ((old.TotalNAV == nil && row.TotalNAV == nil) || (old.TotalNAV != nil && row.TotalNAV != nil && old.TotalNAV.Equal(*row.TotalNAV)))
				if equal {
					continue
				}
				row.ID = old.ID
				row.Revision = old.Revision + 1
				if err = tx.Model(&old).Updates(map[string]any{"unit_nav": row.UnitNAV, "total_nav": row.TotalNAV, "revision": row.Revision, "fetched_at": row.FetchedAt}).Error; err != nil {
					return err
				}
			} else {
				row.Revision = 1
				if err = tx.Create(&row).Error; err != nil {
					return err
				}
				manuals := []model.WealthManualNAV{}
				if err = tx.Where("product_id = ? AND nav_date >= ? AND nav_date < ? AND withdrawn = ?", product, lower, upper, false).Find(&manuals).Error; err != nil {
					return err
				}
				for _, manual := range manuals {
					before, _ := json.Marshal(manual)
					after, _ := json.Marshal(row)
					if err = tx.Create(&model.WealthManualAudit{OwnerID: manual.OwnerID, ProductID: product, ManualNAVID: manual.ID, ActorID: 0, Action: "automatic_takeover", BeforeJSON: string(before), AfterJSON: string(after), CreatedAt: now}).Error; err != nil {
						return err
					}
				}
			}
			if err = tx.Create(&model.WealthNAVRevision{ProductID: product, NAVDate: row.NAVDate, UnitNAV: row.UnitNAV, TotalNAV: row.TotalNAV, Revision: row.Revision, RecordedAt: now}).Error; err != nil {
				return err
			}
			changed = true
		}
		if !changed {
			return nil
		}
		if err = tx.Model(&model.WealthPosition{}).Where("product_id = ?", product).Distinct().Pluck("owner_id", &owners).Error; err != nil {
			return err
		}
		for _, owner := range owners {
			if err = bound.BumpSeries(owner, product); err != nil {
				return err
			}
		}
		if NewWealthCashRepo(tx).Available() {
			var cashOwners []int64
			if err = tx.Model(&model.WealthCashLink{}).Where("product_id = ? AND active = ?", product, true).Distinct().Pluck("owner_id", &cashOwners).Error; err != nil {
				return err
			}
			seen := map[int64]bool{}
			for _, owner := range owners {
				seen[owner] = true
			}
			for _, owner := range cashOwners {
				if !seen[owner] {
					owners = append(owners, owner)
					seen[owner] = true
				}
			}
		}
		return nil
	})
	return owners, changed && err == nil, err
}

func (r *WealthRepo) ManualAt(owner, product int64, date time.Time) (*model.WealthManualNAV, error) {
	var row model.WealthManualNAV
	lower, upper := WealthDateBounds(date)
	err := r.db.Where("owner_id = ? AND product_id = ? AND nav_date >= ? AND nav_date < ?", owner, product, lower, upper).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) HasProduct(owner, product int64) (bool, error) {
	var n int64
	err := r.db.Model(&model.WealthPosition{}).Where("owner_id = ? AND product_id = ?", owner, product).Count(&n).Error
	if err == nil && n == 0 && NewWealthCashRepo(r.db).Available() {
		err = r.db.Model(&model.WealthCashLink{}).Where("owner_id = ? AND product_id = ?", owner, product).Count(&n).Error
	}
	return n > 0, err
}
func (r *WealthRepo) ManualAudits(owner, product int64) ([]model.WealthManualAudit, error) {
	rows := []model.WealthManualAudit{}
	err := r.db.Where("owner_id = ? AND product_id = ?", owner, product).Order("id DESC").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) PositionsForProduct(owner, product int64) ([]model.WealthPosition, error) {
	rows := []model.WealthPosition{}
	q := r.db.Where("product_id = ?", product)
	if owner > 0 {
		q = q.Where("owner_id = ?", owner)
	}
	err := q.Order("owner_id,id").Find(&rows).Error
	return rows, err
}

// Manual and automatic writes share this same product row lock.
func (r *WealthRepo) AutomaticAt(product int64, date time.Time) (*model.WealthNAV, error) {
	var row model.WealthNAV
	lower, upper := WealthDateBounds(date)
	err := r.db.Where("product_id = ? AND nav_date >= ? AND nav_date < ?", product, lower, upper).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// SQL DATE is a calendar value. Driver loc=Local converts UTC midnight to
// Shanghai 08:00 when given a time.Time. String day bounds preserve the date,
// use the date index, and also match SQLite's timestamp-backed DATE fixtures.
func WealthDateBounds(date time.Time) (string, string) {
	return date.Format("2006-01-02"), date.AddDate(0, 0, 1).Format("2006-01-02")
}

func (r *WealthRepo) EnsureSeries(owner, product int64) error {
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.WealthSeries{OwnerID: owner, ProductID: product, Version: 1}).Error
}
