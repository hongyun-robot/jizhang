package service

import (
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

func (s *TransactionService) guardSettlementFunds(t *model.Transaction) error {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok || !provider.DB().Migrator().HasTable(&model.LiabilityCreditEvent{}) {
		return nil
	}
	var count int64
	if err := provider.DB().Model(&model.LiabilityCreditEvent{}).Where("source_id = ? AND active = ? AND mode = ?", t.ID, true, "settlement").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return liability.ErrManaged
	}
	return nil
}

func (s *TransactionService) fillLiabilityLinks(rows []model.Transaction) {
	provider, ok := s.deps.Txns.(interface{ DB() *gorm.DB })
	if !ok || len(rows) == 0 {
		return
	}
	db := provider.DB()
	var accounts []model.Account
	if db.Where("type = ?", model.AccountLiability).Find(&accounts).Error != nil {
		return
	}
	credit := map[int64]bool{}
	for _, a := range accounts {
		credit[a.ID] = true
	}
	termIds := []int64{}
	opIds := []int64{}
	for _, t := range rows {
		if t.LiabilityTermID != nil {
			termIds = append(termIds, *t.LiabilityTermID)
		}
		if t.LiabilityOperationID != nil {
			opIds = append(opIds, *t.LiabilityOperationID)
		}
	}
	linkedTerms := map[int64]int64{}
	linkedOps := map[int64]int64{}
	if len(termIds) > 0 {
		var results []struct {
			ID        int64
			AccountID int64
		}
		if db.Model(&model.InstallmentTerm{}).Select("installment_terms.id, p.account_id").Joins("JOIN installment_plans p ON p.id = installment_terms.plan_id").Where("installment_terms.id IN ?", termIds).Scan(&results).Error == nil {
			for _, x := range results {
				linkedTerms[x.ID] = x.AccountID
			}
		}
	}
	if len(opIds) > 0 {
		var ops []model.LiabilityOperation
		if db.Where("id IN ?", opIds).Find(&ops).Error == nil {
			for _, o := range ops {
				linkedOps[o.ID] = o.AccountID
			}
		}
	}
	for i, t := range rows {
		id := int64(0)
		if credit[t.AccountID] {
			id = t.AccountID
		}
		if t.TransferAccountID != nil && credit[*t.TransferAccountID] {
			id = *t.TransferAccountID
		}
		if t.LiabilityTermID != nil {
			id = linkedTerms[*t.LiabilityTermID]
		}
		if t.LiabilityOperationID != nil {
			id = linkedOps[*t.LiabilityOperationID]
		}
		if id > 0 {
			v := id
			rows[i].LiabilityAccountID = &v
		}
	}
}
