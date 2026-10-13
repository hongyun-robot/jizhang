package service

import (
	"context"
	"errors"
	"gorm.io/gorm/clause"
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"time"
)

type FundSyncResult struct {
	Updated    int  `json:"updated"`
	Failed     int  `json:"failed"`
	Configured bool `json:"configured"`
}

func (s *FundService) syncInstrument(ctx context.Context, id int64, cold bool) error {
	return s.syncFundInstrument(ctx, id, cold, nil)
}

func (s *FundService) syncFundInstrument(ctx context.Context, id int64, cold bool, run *fundRun) error {
	var item model.FundInstrument
	if e := s.repo.DB().First(&item, id).Error; e != nil {
		return e
	}
	if !s.Configured() {
		_ = s.repo.DB().Model(&item).Updates(map[string]any{"sync_status": "unconfigured", "last_error": fuyao.ErrUnconfigured.Error()}).Error
		return fuyao.ErrUnconfigured
	}
	now := s.Now()
	if run != nil {
		current, active := fundRunAt(now)
		if !active || current.Morning != run.Morning || !current.TargetDate.Equal(run.TargetDate) || !current.SlotStart.Equal(run.SlotStart) {
			return nil
		}
	}
	var previous *model.FundNAV
	var e error
	if run != nil && !run.Morning {
		if fundSameDay(item.EveningCompletedFor, run.TargetDate) {
			return nil
		}
		previous, e = s.repo.NAVAt(id, fundToday(now))
		if e != nil {
			return e
		}
		if previous != nil && !fundDay(previous.NAVDate).Before(run.TargetDate) {
			if e = s.rebuildFundInstrument(id); e != nil {
				return e
			}
			return s.repo.DB().Model(&item).Update("evening_completed_for", run.TargetDate).Error
		}
	}
	var claimed bool
	if run == nil {
		claimed, e = s.repo.Claim(id, now)
	} else {
		claimed, e = s.repo.ClaimScheduled(id, now, run.TargetDate, run.SlotStart, run.Morning)
	}
	if e != nil {
		return e
	}
	if !claimed {
		if cold {
			deadline := time.NewTimer(16 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				if price, err := s.repo.NAVAt(id, fundToday(s.Now())); err != nil {
					return err
				} else if price != nil {
					return nil
				}
				select {
				case <-ctx.Done():
					return ErrFundPrice
				case <-deadline.C:
					return ErrFundPrice
				case <-ticker.C:
				}
			}
		}
		return nil
	}
	window := "month"
	if cold || !item.HistoryLoaded || item.LastAttempt == nil || now.Sub(*item.LastAttempt) > 28*24*time.Hour {
		window = "fyear"
	}
	rows, e := s.market.NAVs(ctx, item.THSCode, window)
	if e == nil && len(rows) == 0 {
		e = ErrFundPrice
	}
	changed := false
	if e == nil {
		for _, row := range rows {
			date := fundDay(row.Date)
			if date.After(fundToday(now)) || !row.Value.IsPositive() {
				e = ErrFundInvalid
				break
			}
			if run != nil && run.Morning && date.After(run.TargetDate) {
				continue
			}
			n := model.FundNAV{InstrumentID: id, NAVDate: date, UnitNAV: row.Value, FetchedAt: now}
			var ch bool
			ch, e = s.repo.SaveNAV(&n, now)
			changed = changed || ch
			if e != nil {
				break
			}
		}
	}
	if e != nil {
		failures := item.FailureCount + 1
		minutes := 1 << min(failures, 6)
		status := "error"
		if errors.Is(e, fuyao.ErrRateLimited) {
			status = "rate_limited"
		}
		_ = s.repo.DB().Model(&item).Updates(map[string]any{"sync_status": status, "last_error": e.Error(), "lease_until": nil, "failure_count": failures, "next_attempt": now.Add(time.Duration(minutes) * time.Minute)}).Error
		if changed {
			s.changed()
		}
		return e
	}
	divs, divErr := s.market.Dividends(ctx, item.THSCode)
	if divErr == nil {
		for _, d := range divs {
			row := model.FundDividend{InstrumentID: id, SourceKey: d.Key, RegistrationDate: fundDay(d.RegistrationDate), ExDate: fundDay(d.ExDate), PaymentDate: d.PaymentDate, PerTenCash: d.PerTenCash}
			if e = s.repo.DB().Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instrument_id"}, {Name: "source_key"}}, DoUpdates: clause.AssignmentColumns([]string{"registration_date", "ex_date", "payment_date", "per_ten_cash"})}).Create(&row).Error; e != nil {
				divErr = e
				break
			}
		}
	}
	status, msg := "ready", ""
	if divErr != nil {
		status = "dividend_error"
		msg = "净值已更新，分红资料暂不可用"
	}
	e = s.repo.DB().Model(&item).Updates(map[string]any{"sync_status": status, "last_error": msg, "lease_until": nil, "failure_count": 0, "next_attempt": now.Add(time.Minute), "history_loaded": item.HistoryLoaded || window == "fyear"}).Error
	if e != nil {
		return e
	}
	rebuildError := s.rebuildFundInstrument(id)
	if rebuildError == nil && run != nil && !run.Morning {
		latest, err := s.repo.NAVAt(id, fundToday(now))
		if err != nil {
			rebuildError = err
		} else if latest != nil && (previous == nil || fundDay(latest.NAVDate).After(fundDay(previous.NAVDate)) || latest.Revision != previous.Revision) {
			rebuildError = s.repo.DB().Model(&item).Update("evening_completed_for", run.TargetDate).Error
		}
	}
	s.changed()
	return rebuildError
}

func (s *FundService) rebuildFundInstrument(id int64) error {
	positions := []model.FundPosition{}
	if e := s.repo.DB().Where("instrument_id = ?", id).Find(&positions).Error; e != nil {
		return e
	}
	if e := s.repo.DB().Model(&model.FundPosition{}).Where("instrument_id = ?", id).Update("rebuild_pending", true).Error; e != nil {
		return e
	}
	var rebuildError error
	for _, p := range positions {
		if e := s.RebuildPosition(p.ID); e != nil {
			if rebuildError == nil {
				rebuildError = e
			}
		}
	}
	return rebuildError
}
func (s *FundService) Ensure(ctx context.Context) FundSyncResult {
	result := FundSyncResult{Configured: s.Configured()}
	// Repair committed financial events using cached prices, even after a full
	// redemption or when the quote provider is unavailable.
	pending := []model.FundPosition{}
	if e := s.repo.DB().Where("rebuild_pending = ?", true).Find(&pending).Error; e != nil {
		result.Failed++
	} else {
		for _, p := range pending {
			if ctx.Err() != nil {
				break
			}
			if e = s.RebuildPosition(p.ID); e != nil {
				result.Failed++
			}
		}
	}
	if !s.Configured() {
		return result
	}
	run, active := fundRunAt(s.Now())
	if !active {
		return result
	}
	rows, e := s.repo.Tracked()
	if e != nil {
		result.Failed++
		return result
	}
	for _, i := range rows {
		if ctx.Err() != nil {
			break
		}
		if e = s.syncFundInstrument(ctx, i.ID, false, &run); e != nil {
			result.Failed++
		} else {
			result.Updated++
		}
	}
	return result
}
func (s *FundService) Refresh(ctx context.Context, actor *model.User, accountID int64) (FundAccountView, error) {
	if _, e := s.account(s.repo.DB(), actor, accountID); e != nil {
		return FundAccountView{}, e
	}
	positions := []model.FundPosition{}
	if e := s.repo.DB().Where("account_id = ? AND owner_id = ?", accountID, actor.ID).Find(&positions).Error; e != nil {
		return FundAccountView{}, e
	}
	seen := map[int64]bool{}
	for _, p := range positions {
		if seen[p.InstrumentID] {
			continue
		}
		seen[p.InstrumentID] = true
		_ = s.syncInstrument(ctx, p.InstrumentID, false)
	}
	return s.Account(actor, accountID)
}
