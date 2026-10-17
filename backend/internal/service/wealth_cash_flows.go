package service

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
)

var ErrWealthCashFlow = errors.New("理财零钱时间无效：普通转出须填写实际收益截止日，转入确认日不得早于入账日")

type WealthCashFlowInput struct {
	AccountID        int64  `json:"account_id"`
	ApplicationTime  string `json:"application_time"`
	TransferMode     string `json:"transfer_mode"`
	ConfirmationDate string `json:"confirmation_date"`
	RightsEndDate    string `json:"rights_end_date"`
}

func (s *TransactionService) saveCashInputs(t *model.Transaction, in TxnInput) error {
	if len(in.WealthCashTimings) == 0 {
		return s.saveCashFlow(t, in.WealthCash)
	}
	if len(in.WealthCashTimings) > 2 || in.WealthCash != nil {
		return ErrWealthCashFlow
	}
	seen := map[int64]bool{}
	for _, timing := range in.WealthCashTimings {
		if timing.AccountID <= 0 || seen[timing.AccountID] {
			return ErrWealthCashFlow
		}
		seen[timing.AccountID] = true
		if err := s.saveCashFlow(t, &timing); err != nil {
			return err
		}
	}
	return nil
}

// Timing belongs to the existing cash transaction. The same ledger transaction
// also covers transfers between two wallets; each side retains its own timing.
func (s *TransactionService) saveCashFlow(t *model.Transaction, input *WealthCashFlowInput) error {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok {
		return nil
	}
	db := provider.DB()
	if !repository.NewWealthCashRepo(db).Available() {
		if input != nil {
			return ErrWealthCashSetup
		}
		return nil
	}
	ids := []int64{t.AccountID}
	if t.TransferAccountID != nil {
		ids = append(ids, *t.TransferAccountID)
	}
	var accounts []model.Account
	if err := db.Where("id IN ?", ids).Order("id").Find(&accounts).Error; err != nil {
		return err
	}
	old := []model.WealthCashFlow{}
	if err := db.Where("txn_id = ?", t.ID).Find(&old).Error; err != nil {
		return err
	}
	existing := map[int64]model.WealthCashFlow{}
	for _, row := range old {
		existing[row.AccountID] = row
	}
	primary := int64(0)
	for _, a := range accounts {
		if a.IsWealthCash() {
			if a.ID == t.AccountID {
				primary = a.ID
				break
			}
			primary = a.ID
		}
	}
	if input != nil && input.AccountID != 0 {
		primary = input.AccountID
	}
	applied := input == nil
	for _, account := range accounts {
		row, had := existing[account.ID]
		if !account.IsWealthCash() && !had {
			continue
		}
		if account.OwnerID == nil || *account.OwnerID != t.CreatedBy {
			return ErrTxnForbidden
		}
		if !had {
			row = model.WealthCashFlow{TxnID: t.ID, AccountID: account.ID, OwnerID: t.CreatedBy, TransferMode: "quick", ApplicationTime: "12:00:00"}
			now := s.liabilityClock()
			if wealthDay(t.EntryDate).Equal(wealthToday(now)) {
				row.ApplicationTime = now.In(wealthdata.Shanghai).Format("15:04:05")
			}
		}
		if input != nil && account.ID == primary {
			applied = true
			if input.ApplicationTime != "" {
				row.ApplicationTime = input.ApplicationTime
			}
			if input.TransferMode != "" {
				row.TransferMode = input.TransferMode
			} else {
				row.TransferMode = "quick"
			}
			row.ConfirmationDate, row.RightsEndDate = nil, nil
			if input.ConfirmationDate != "" {
				date, err := time.Parse("2006-01-02", input.ConfirmationDate)
				if err != nil || date.Before(wealthDay(t.EntryDate)) || date.Year() > 2100 {
					return ErrWealthCashFlow
				}
				row.ConfirmationDate = &date
			}
			if input.RightsEndDate != "" {
				date, err := time.Parse("2006-01-02", input.RightsEndDate)
				if err != nil || date.Year() < 2000 || date.Year() > 2100 {
					return ErrWealthCashFlow
				}
				row.RightsEndDate = &date
			}
		}
		clock, err := time.Parse("15:04:05", row.ApplicationTime)
		if err != nil || clock.Format("15:04:05") != row.ApplicationTime || (row.TransferMode != "quick" && row.TransferMode != "ordinary") {
			return ErrWealthCashFlow
		}
		outgoing := account.ID == t.AccountID && (t.Kind == model.TxnExpense || t.Kind == model.TxnTransfer)
		if row.ConfirmationDate != nil && wealthDay(*row.ConfirmationDate).Before(wealthDay(t.EntryDate)) {
			return ErrWealthCashFlow
		}
		if outgoing && row.TransferMode == "ordinary" && row.RightsEndDate == nil {
			return ErrWealthCashFlow
		}
		if !outgoing {
			row.TransferMode = "quick"
			row.RightsEndDate = nil
		}
		if err = db.Save(&row).Error; err != nil {
			return err
		}
	}
	if !applied {
		return ErrWealthCashFlow
	}
	if err := db.Where("txn_id = ? AND account_id NOT IN ?", t.ID, ids).Delete(&model.WealthCashFlow{}).Error; err != nil {
		return err
	}
	result := []model.Transaction{*t}
	if err := s.fillCashFlows(result); err != nil {
		return err
	}
	t.WealthCashFlows = result[0].WealthCashFlows
	return nil
}

func (s *TransactionService) deleteCashFlow(id int64) error {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok || !repository.NewWealthCashRepo(provider.DB()).Available() {
		return nil
	}
	return provider.DB().Where("txn_id = ?", id).Delete(&model.WealthCashFlow{}).Error
}
func (s *TransactionService) fillCashFlows(rows []model.Transaction) error {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok || len(rows) == 0 || !repository.NewWealthCashRepo(provider.DB()).Available() {
		return nil
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	metadata := []model.WealthCashFlow{}
	if err := provider.DB().Where("txn_id IN ?", ids).Find(&metadata).Error; err != nil {
		return err
	}
	byID := map[int64][]model.WealthCashFlow{}
	for _, row := range metadata {
		byID[row.TxnID] = append(byID[row.TxnID], row)
	}
	for i := range rows {
		rows[i].WealthCashFlows = byID[rows[i].ID]
	}
	return nil
}
