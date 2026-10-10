package service

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func (s *ClosingService) bound(tx *gorm.DB) *ClosingService {
	periods := s.deps.Periods
	if _, ok := periods.settings.(*repository.SettingRepo); ok {
		periods = NewPeriodService(repository.NewSettingRepo(tx))
	}
	inner := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(tx), Audits: repository.NewAuditRepo(tx), Periods: periods, Stats: repository.NewStatsRepo(tx)})
	inner.now = s.now
	return inner
}
func (s *ClosingService) Close(actor *model.User, key string) (out *model.PeriodClosing, err error) {
	err = repository.LedgerWrite(s.deps.Closings.DB(), func(tx *gorm.DB) error { var e error; out, e = s.bound(tx).close(actor, key); return e })
	return
}
func (s *ClosingService) Reopen(actor *model.User, key string) error {
	return repository.LedgerWrite(s.deps.Closings.DB(), func(tx *gorm.DB) error { return s.bound(tx).reopen(actor, key) })
}
