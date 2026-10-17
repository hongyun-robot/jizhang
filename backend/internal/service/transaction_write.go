package service

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"time"
)

// write 在提交后才调用原钩子；纯内存测试替身无需数据库事务。
func (s *TransactionService) write(fn func(*TransactionService) error) error {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok {
		return fn(s)
	}
	dirty := false
	var periods []string
	err := repository.LedgerWrite(provider.DB(), func(tx *gorm.DB) error {
		inner := s.bound(tx)
		inner.OnWrite = func() { dirty = true }
		inner.OnWritePeriod = func(keys ...string) { periods = append(periods, keys...) }
		return fn(inner)
	})
	if err == nil && dirty {
		s.fireWrite()
		s.firePeriods(periods...)
	}
	return err
}

func (s *TransactionService) bound(tx *gorm.DB) *TransactionService {
	periods := s.deps.Periods
	if _, ok := periods.settings.(*repository.SettingRepo); ok {
		periods = NewPeriodService(repository.NewSettingRepo(tx))
	}
	inner := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(tx), Accounts: repository.NewAccountRepo(tx), Cats: repository.NewCategoryRepo(tx), Closings: repository.NewClosingRepo(tx), Periods: periods})
	inner.liabilityNow = s.liabilityNow
	return inner
}

func (s *TransactionService) ensurePlatform(ownerID int64, name string) error {
	if name == "" {
		return nil
	}
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok {
		return nil // 无数据库的测试仓储只保存流水本身。
	}
	return ensureVoucherPlatform(provider.DB(), ownerID, name)
}

func (s *TransactionService) Create(actor *model.User, in TxnInput) (out *model.Transaction, err error) {
	err = s.write(func(inner *TransactionService) error { var e error; out, e = inner.create(actor, in); return e })
	if out != nil {
		out.FillEffects()
	}
	return
}
func (s *TransactionService) CreateFromRule(rule *model.RecurringRule, date time.Time) (out *model.Transaction, err error) {
	err = s.write(func(inner *TransactionService) error {
		var e error
		out, e = inner.createFromRule(rule, date)
		return e
	})
	if out != nil {
		out.FillEffects()
	}
	return
}
func (s *TransactionService) Update(actor *model.User, id int64, in TxnInput) (out *model.Transaction, err error) {
	err = s.write(func(inner *TransactionService) error { var e error; out, e = inner.update(actor, id, in); return e })
	if out != nil {
		out.FillEffects()
	}
	return
}
func (s *TransactionService) Delete(actor *model.User, id int64) error {
	return s.write(func(inner *TransactionService) error { return inner.delete(actor, id) })
}

func (s *TransactionService) fillEffects(rows []model.Transaction) {
	s.fillLiabilityLinks(rows)
	s.fillFundLinks(rows)
	s.fillWealthLinks(rows)
	var ids []int64
	for i := range rows {
		rows[i].FillEffects()
		if rows[i].VoucherEventID != nil {
			ids = append(ids, *rows[i].VoucherEventID)
		}
	}
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok || len(ids) == 0 {
		return
	}
	var events []model.VoucherEvent
	if err := provider.DB().Select("id, voucher_id").Where("id IN ?", ids).Find(&events).Error; err != nil {
		return
	}
	byID := make(map[int64]int64, len(events))
	for _, e := range events {
		byID[e.ID] = e.VoucherID
	}
	for i := range rows {
		if rows[i].VoucherEventID != nil {
			if id, ok := byID[*rows[i].VoucherEventID]; ok {
				rows[i].VoucherID = &id
			}
		}
	}
}

func (s *TransactionService) fillWealthLinks(rows []model.Transaction) {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok {
		return
	}
	ids := []int64{}
	for _, row := range rows {
		if row.WealthEventID != nil {
			ids = append(ids, *row.WealthEventID)
		}
	}
	if len(ids) == 0 {
		return
	}
	links, err := repository.NewWealthRepo(provider.DB()).CashLinks(ids)
	if err != nil {
		return
	}
	byID := map[int64]repository.WealthCashLink{}
	for _, link := range links {
		byID[link.EventID] = link
	}
	for i := range rows {
		if rows[i].WealthEventID != nil {
			if link, ok := byID[*rows[i].WealthEventID]; ok {
				id := link.PositionID
				rows[i].WealthPositionID = &id
				rows[i].WealthCode = link.Code
			}
		}
	}
}

func (s *TransactionService) fillFundLinks(rows []model.Transaction) {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok {
		return
	}
	ids := []int64{}
	for _, r := range rows {
		if r.FundEventID != nil {
			ids = append(ids, *r.FundEventID)
		}
	}
	if len(ids) == 0 {
		return
	}
	links, e := repository.NewFundRepo(provider.DB()).CashLinks(ids)
	if e != nil {
		return
	}
	byID := map[int64]repository.FundCashLink{}
	for _, link := range links {
		byID[link.EventID] = link
	}
	for i := range rows {
		if rows[i].FundEventID != nil {
			if e, ok := byID[*rows[i].FundEventID]; ok {
				id := e.PositionID
				rows[i].FundPositionID = &id
				rows[i].FundCode = e.THSCode
			}
		}
	}
}
