package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"time"
)

type FundRepo struct{ db *gorm.DB }

func NewFundRepo(db *gorm.DB) *FundRepo      { return &FundRepo{db: db} }
func (r *FundRepo) DB() *gorm.DB             { return r.db }
func (r *FundRepo) At(db *gorm.DB) *FundRepo { return NewFundRepo(db) }
func (r *FundRepo) Instrument(code string) (*model.FundInstrument, error) {
	var row model.FundInstrument
	e := r.db.Where("ths_code = ?", code).First(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, e
}
func (r *FundRepo) SaveInstrument(row *model.FundInstrument) error {
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "ths_code"}}, DoUpdates: clause.AssignmentColumns([]string{"ticker", "name", "asset_type", "manager", "management", "updated_at"})}).Create(row).Error
}
func (r *FundRepo) Position(id int64, lock bool) (*model.FundPosition, error) {
	var row model.FundPosition
	q := r.db
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.First(&row, id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, e
}
func (r *FundRepo) PositionForCode(accountID, instrumentID int64) (*model.FundPosition, error) {
	var p model.FundPosition
	e := r.db.Where("account_id = ? AND instrument_id = ?", accountID, instrumentID).First(&p).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, e
}
func (r *FundRepo) Mutation(owner int64, key string) (*model.FundMutation, error) {
	var m model.FundMutation
	e := r.db.Where("owner_id = ? AND request_key = ?", owner, key).First(&m).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, e
}
func (r *FundRepo) Events(positionID int64) ([]model.FundEvent, error) {
	rows := []model.FundEvent{}
	e := r.db.Where("position_id = ?", positionID).Order("effective_date,id").Find(&rows).Error
	return rows, e
}
func (r *FundRepo) NAVs(instrumentID int64) ([]model.FundNAV, error) {
	rows := []model.FundNAV{}
	e := r.db.Where("instrument_id = ?", instrumentID).Order("nav_date").Find(&rows).Error
	return rows, e
}
func (r *FundRepo) NAVAt(instrumentID int64, date time.Time) (*model.FundNAV, error) {
	var row model.FundNAV
	e := r.db.Where("instrument_id = ? AND nav_date <= ?", instrumentID, date).Order("nav_date DESC").First(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, e
}
func (r *FundRepo) Dividends(instrumentID int64) ([]model.FundDividend, error) {
	rows := []model.FundDividend{}
	e := r.db.Where("instrument_id = ?", instrumentID).Order("ex_date").Find(&rows).Error
	return rows, e
}
func (r *FundRepo) SaveNAV(row *model.FundNAV, now time.Time) (bool, error) {
	changed := false
	e := r.db.Transaction(func(tx *gorm.DB) error {
		var old model.FundNAV
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("instrument_id = ? AND nav_date = ?", row.InstrumentID, row.NAVDate).First(&old).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			row.Revision = 1
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
			changed = result.RowsAffected > 0
			if result.Error != nil || !changed {
				return result.Error
			}
			return tx.Model(&model.FundPosition{}).Where("instrument_id = ?", row.InstrumentID).Update("rebuild_pending", true).Error
		}
		if e != nil {
			return e
		}
		if old.UnitNAV.Equal(row.UnitNAV) {
			return nil
		}
		if e := tx.Create(&model.FundNAVRevision{InstrumentID: old.InstrumentID, NAVDate: old.NAVDate, UnitNAV: old.UnitNAV, Revision: old.Revision, RecordedAt: now}).Error; e != nil {
			return e
		}
		changed = true
		if e = tx.Model(&old).Updates(map[string]any{"unit_nav": row.UnitNAV, "revision": old.Revision + 1, "fetched_at": row.FetchedAt}).Error; e != nil {
			return e
		}
		return tx.Model(&model.FundPosition{}).Where("instrument_id = ?", row.InstrumentID).Update("rebuild_pending", true).Error
	})
	return changed && e == nil, e
}
func (r *FundRepo) Claim(instrumentID int64, now time.Time) (bool, error) {
	result := r.db.Model(&model.FundInstrument{}).Where("id = ? AND (lease_until IS NULL OR lease_until <= ?) AND (next_attempt IS NULL OR next_attempt <= ?)", instrumentID, now, now).
		Updates(map[string]any{"lease_until": now.Add(90 * time.Second), "last_attempt": now})
	return result.RowsAffected == 1, result.Error
}

// ClaimScheduled persists the morning attempt before I/O and uses the existing
// last-attempt timestamp to suppress duplicate evening half-hour slots.
func (r *FundRepo) ClaimScheduled(instrumentID int64, now, target, slot time.Time, morning bool) (bool, error) {
	q := r.db.Model(&model.FundInstrument{}).Where("id = ? AND (lease_until IS NULL OR lease_until <= ?)", instrumentID, now)
	updates := map[string]any{"lease_until": now.Add(90 * time.Second), "last_attempt": now}
	if morning {
		q = q.Where("(morning_attempted_for IS NULL OR morning_attempted_for < ?)", target)
		updates["morning_attempted_for"] = target
	} else {
		q = q.Where("(evening_completed_for IS NULL OR evening_completed_for < ?) AND (last_attempt IS NULL OR last_attempt < ?) AND (next_attempt IS NULL OR next_attempt <= ?)", target, slot, now)
	}
	result := q.Updates(updates)
	return result.RowsAffected == 1, result.Error
}
func (r *FundRepo) Tracked() ([]model.FundInstrument, error) {
	rows := []model.FundInstrument{}
	e := r.db.Raw(`SELECT DISTINCT i.* FROM fund_instruments i JOIN fund_positions p ON p.instrument_id=i.id
	 WHERE i.history_loaded=FALSE OR EXISTS (SELECT 1 FROM fund_events e WHERE e.position_id=p.id AND e.voided_at IS NULL
	 GROUP BY e.position_id HAVING SUM(CASE WHEN e.kind='redeem' THEN -e.shares WHEN e.kind IN ('opening','buy','reinvest') THEN e.shares ELSE 0 END)>0)
	 ORDER BY i.last_attempt ASC,i.id ASC`).Scan(&rows).Error
	return rows, e
}

type FundData struct {
	Positions   []model.FundPosition
	Instruments []model.FundInstrument
	Events      []model.FundEvent
	NAVs        []model.FundNAV
	Dividends   []model.FundDividend
}

// LoadAccounts accepts IDs already constrained by the caller's authenticated account scope.
func (r *FundRepo) LoadAccounts(ids []int64) (FundData, error) {
	d := FundData{Positions: []model.FundPosition{}, Instruments: []model.FundInstrument{}, Events: []model.FundEvent{}, NAVs: []model.FundNAV{}, Dividends: []model.FundDividend{}}
	if len(ids) == 0 {
		return d, nil
	}
	if e := r.db.Where("account_id IN ?", ids).Order("id").Find(&d.Positions).Error; e != nil {
		return d, e
	}
	positions := []int64{}
	instruments := []int64{}
	for _, p := range d.Positions {
		positions = append(positions, p.ID)
		instruments = append(instruments, p.InstrumentID)
	}
	if len(positions) == 0 {
		return d, nil
	}
	for _, q := range []struct {
		dest  any
		where string
		ids   []int64
		order string
	}{{&d.Events, "position_id IN ?", positions, "effective_date,id"}, {&d.NAVs, "instrument_id IN ?", instruments, "nav_date"}, {&d.Instruments, "id IN ?", instruments, "id"}, {&d.Dividends, "instrument_id IN ?", instruments, "ex_date"}} {
		if e := r.db.Where(q.where, q.ids).Order(q.order).Find(q.dest).Error; e != nil {
			return d, e
		}
	}
	return d, nil
}
