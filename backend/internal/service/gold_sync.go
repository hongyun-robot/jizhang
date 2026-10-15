package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
)

const GoldHourlyCron = "0 * * * *"

type GoldQuoteView struct {
	Quote         *model.GoldQuoteObservation `json:"quote"`
	LastAttemptAt *time.Time                  `json:"last_attempt_at"`
	LastSuccessAt *time.Time                  `json:"last_success_at"`
	NextRefreshAt *time.Time                  `json:"next_refresh_at"`
	Status        string                      `json:"status"`
	Error         string                      `json:"error,omitempty"`
	Refreshing    bool                        `json:"refreshing"`
}

func (s *GoldService) QuoteView() (GoldQuoteView, error) {
	v := GoldQuoteView{Status: "no_price"}
	var st model.GoldQuoteSync
	e := s.repo.DB().Where("product_key = ?", ccb.ProductKey).First(&st).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	v.LastAttemptAt = st.LastAttemptAt
	v.LastSuccessAt = st.LastSuccessAt
	v.NextRefreshAt = st.NextRefreshAt
	v.Error = st.LastError
	now := s.Now()
	v.Refreshing = st.LeaseUntil != nil && st.LeaseUntil.After(now)
	if st.ObservationID != nil {
		var q model.GoldQuoteObservation
		if e = s.repo.DB().First(&q, *st.ObservationID).Error; e != nil {
			return v, e
		}
		v.Quote = &q
		v.Status = "ok"
		if st.SourceUnchanged {
			v.Status = "source_unchanged"
		}
	}
	if st.LastError != "" {
		v.Status = "query_error"
	}
	anchor := st.LastSuccessAt
	if anchor == nil {
		anchor = st.FirstAttemptAt
	}
	if anchor != nil && now.Sub(*anchor) >= 2*time.Hour {
		v.Status = "update_error"
	}
	if v.Quote == nil && v.Status != "update_error" {
		v.Status = "no_price"
	}
	return v, nil
}

// Refresh returns cached status on cooldown; ordinary QuoteView never makes an HTTP request.
func (s *GoldService) Refresh(ctx context.Context, actor *model.User) (GoldQuoteView, error) {
	if fundActor(actor) != nil {
		return GoldQuoteView{}, ErrTxnForbidden
	}
	return s.syncGold(ctx, false)
}
func (s *GoldService) syncGold(ctx context.Context, scheduled bool) (GoldQuoteView, error) {
	if s.market == nil {
		return s.QuoteView()
	}
	now := s.Now()
	token := uuid.NewString()
	claimed := false
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.GoldQuoteSync{ProductKey: ccb.ProductKey}).Error; e != nil {
			return e
		}
		var st model.GoldQuoteSync
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_key = ?", ccb.ProductKey).First(&st).Error; e != nil {
			return e
		}
		if scheduled && st.ScheduledSlot == goldSlot(now) {
			return nil
		}
		if st.LeaseUntil != nil && st.LeaseUntil.After(now) {
			if scheduled {
				return tx.Model(&st).Update("scheduled_slot", goldSlot(now)).Error
			}
			return nil
		}
		if st.NextRefreshAt != nil && st.NextRefreshAt.After(now) {
			if scheduled {
				return tx.Model(&st).Update("scheduled_slot", goldSlot(now)).Error
			}
			return nil
		}
		until := now.Add(time.Minute)
		next := now.Add(30 * time.Second)
		changes := map[string]any{"lease_token": token, "lease_until": until, "next_refresh_at": next, "last_attempt_at": now}
		if st.FirstAttemptAt == nil {
			changes["first_attempt_at"] = now
		}
		if scheduled {
			changes["scheduled_slot"] = goldSlot(now)
		}
		res := tx.Model(&model.GoldQuoteSync{}).Where("product_key = ? AND (lease_until IS NULL OR lease_until <= ?)", ccb.ProductKey, now).Updates(changes)
		claimed = res.RowsAffected == 1
		return res.Error
	})
	if err != nil {
		return GoldQuoteView{}, err
	}
	if !claimed {
		// Join an in-flight request using only database polling; never fan out to CCB.
		for i := 0; i < 75; i++ {
			v, e := s.QuoteView()
			if e != nil || !v.Refreshing {
				return v, e
			}
			select {
			case <-ctx.Done():
				return v, nil
			case <-time.After(200 * time.Millisecond):
			}
		}
		return s.QuoteView()
	}
	q, fetchErr := s.market.Quote(ctx)
	fetched := s.Now()
	saved := false
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		var st model.GoldQuoteSync
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_key = ?", ccb.ProductKey).First(&st).Error; e != nil {
			return e
		}
		if st.LeaseToken != token || st.LeaseUntil == nil || !st.LeaseUntil.After(fetched) {
			return nil
		}
		changes := map[string]any{"lease_until": nil, "lease_token": ""}
		if fetchErr == nil && (!q.Buy.IsPositive() || !q.Sell.IsPositive() || q.At.IsZero() || q.At.After(fetched.Add(5*time.Minute))) {
			fetchErr = ccb.ErrUnavailable
		}
		var old *model.GoldQuoteObservation
		if st.ObservationID != nil {
			var previous model.GoldQuoteObservation
			if e := tx.First(&previous, *st.ObservationID).Error; e != nil {
				return e
			}
			old = &previous
			if fetchErr == nil && q.At.Before(previous.QuoteAt) {
				fetchErr = ccb.ErrUnavailable
			}
		}
		if fetchErr != nil {
			changes["last_error"] = ccb.ErrUnavailable.Error()
		} else {
			obs := model.GoldQuoteObservation{ProductKey: ccb.ProductKey, BuyPrice: q.Buy, SellPrice: q.Sell, QuoteAt: q.At, FetchedAt: fetched, AttemptKey: token}
			if old != nil && q.At.Equal(old.QuoteAt) && (!q.Buy.Equal(old.BuyPrice) || !q.Sell.Equal(old.SellPrice)) {
				obs.RevisionOf = &old.ID
			}
			if e := tx.Create(&obs).Error; e != nil {
				return e
			}
			changes["observation_id"] = obs.ID
			changes["last_success_at"] = fetched
			changes["source_unchanged"] = old != nil && q.At.Equal(old.QuoteAt)
			changes["last_error"] = ""
			saved = true
		}
		return tx.Model(&st).Updates(changes).Error
	})
	if err != nil {
		return GoldQuoteView{}, err
	}
	if saved {
		_ = s.repo.DB().Model(&model.GoldPosition{}).Where("product_key = ?", ccb.ProductKey).Update("rebuild_pending", true).Error
		s.changed()
	}
	return s.QuoteView()
}
func (s *GoldService) Ensure(ctx context.Context) error {
	var count int64
	if e := s.repo.DB().Model(&model.Account{}).Where("asset_class = ?", "gold").Count(&count).Error; e != nil {
		return e
	}
	if count > 0 {
		if _, e := s.syncGold(ctx, true); e != nil {
			return e
		}
	}
	var pending []model.GoldPosition
	if e := s.repo.DB().Where("rebuild_pending = ?", true).Find(&pending).Error; e != nil {
		return e
	}
	for _, p := range pending {
		if e := s.Rebuild(p.AccountID); e != nil {
			return e
		}
	}
	return nil
}
