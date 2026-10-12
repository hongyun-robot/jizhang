package service

import "jizhang/internal/model"

// Legacy transfers expose only the caller's side. The stored journal and money
// stay intact; synthetic labels are presentation data and never written back.
func (s *TransactionService) redactHistory(actor *model.User, rows []model.Transaction) error {
	accounts, e := s.deps.Accounts.List()
	if e != nil {
		return e
	}
	visible := map[int64]bool{}
	for _, a := range accounts {
		visible[a.ID] = ownsAccount(actor, a)
	}
	for i := range rows {
		t := &rows[i]
		if !visible[t.AccountID] {
			t.AccountNameOverride = "历史关联账户"
			t.HistoricalReadonly = true
		}
		if t.TransferAccountID != nil && !visible[*t.TransferAccountID] {
			t.TransferNameOverride = "历史关联账户"
			t.HistoricalReadonly = true
		}
		if t.LiabilityAccountID != nil && !visible[*t.LiabilityAccountID] {
			t.LiabilityAccountID = nil
			t.LiabilityTermID = nil
			t.LiabilityOperationID = nil
			t.InstallmentPlanID = nil
			t.HistoricalReadonly = true
		}
	}
	return nil
}
