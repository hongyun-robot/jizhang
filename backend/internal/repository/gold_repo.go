package repository

import (
	"gorm.io/gorm"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"time"
)

type GoldRepo struct{ db *gorm.DB }

func NewGoldRepo(db *gorm.DB) *GoldRepo { return &GoldRepo{db} }
func (r *GoldRepo) DB() *gorm.DB        { return r.db }
func (r *GoldRepo) Events(accountID int64) ([]model.GoldEvent, error) {
	rows := []model.GoldEvent{}
	e := r.db.Where("account_id = ?", accountID).Order("trade_date,id").Find(&rows).Error
	return rows, e
}
func (r *GoldRepo) Position(accountID int64) (*model.GoldPosition, error) {
	var row model.GoldPosition
	e := r.db.Where("account_id = ?", accountID).First(&row).Error
	if e == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &row, e
}
func (r *GoldRepo) Observation(at time.Time, dayOnly bool) (*model.GoldQuoteObservation, error) {
	if !dayOnly {
		var state model.GoldQuoteSync
		if e := r.db.Where("product_key = ?", ccb.ProductKey).First(&state).Error; e == gorm.ErrRecordNotFound {
			return nil, nil
		} else if e != nil {
			return nil, e
		}
		if state.ObservationID == nil {
			return nil, nil
		}
		var row model.GoldQuoteObservation
		e := r.db.First(&row, *state.ObservationID).Error
		return &row, e
	}
	q := r.db.Where("product_key = ? AND quote_at <= ? AND fetched_at <= ?", ccb.ProductKey, at, at)
	if dayOnly {
		start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
		q = q.Where("fetched_at >= ?", start)
	}
	var row model.GoldQuoteObservation
	e := q.Order("quote_at DESC, fetched_at DESC, id DESC").First(&row).Error
	if e == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &row, e
}
func (r *GoldRepo) Observations(start, end time.Time) ([]model.GoldQuoteObservation, error) {
	rows := []model.GoldQuoteObservation{}
	e := r.db.Where("product_key = ? AND fetched_at >= ? AND fetched_at <= ?", ccb.ProductKey, start, end).Order("fetched_at,id").Find(&rows).Error
	return rows, e
}
