package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/wealthdata"
)

func (s *WealthService) syncProduct(ctx context.Context, product int64, slots []time.Time, scheduled bool) (*model.WealthProduct, error) {
	token := uuid.NewString()
	claimed, err := s.repo.ClaimCollection(product, s.Now(), token, slots, scheduled)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return s.repo.Product(product, false)
	}
	item, err := s.repo.Product(product, false)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	changed := false
	if !s.configuredFor(item.Code) {
		err = wealthdata.ErrUnconfigured
	} else {
		var rows []wealthdata.NAV
		rows, err = s.market.NAVs(requestCtx, item.Code, "all")
		if err == nil {
			changed, err = s.SavePublicNAVs(product, rows, token)
		}
	}
	message := ""
	if err != nil {
		message = "银行理财净值查询失败，保留上次有效数据"
		if !s.configuredFor(item.Code) {
			message = wealthdata.ErrUnconfigured.Error()
		}
	}
	if finish := s.repo.FinishCollection(product, token, s.Now(), err == nil, changed, message); finish != nil {
		return nil, finish
	}
	return s.repo.Product(product, false)
}

// RefreshProduct returns the durable refreshing/cooldown state when another
// worker owns collection. No caller receives another owner's positions.
func (s *WealthService) RefreshProduct(ctx context.Context, actor *model.User, id int64) (*model.WealthProduct, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	p, err := s.repo.Product(id, false)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrWealthCode
	}
	has, err := s.repo.HasProduct(actor.ID, id)
	if err != nil {
		return nil, err
	}
	if !has && p.AdmissionStatus != "enabled" {
		return nil, ErrWealthCode
	}
	return s.syncProduct(ctx, id, nil, false)
}
func (s *WealthService) Refresh(ctx context.Context, actor *model.User, account int64) (WealthAccountView, error) {
	if _, err := s.account(s.repo.DB(), actor, account); err != nil {
		return WealthAccountView{}, err
	}
	data, err := s.repo.LoadAccounts([]int64{account})
	if err != nil {
		return WealthAccountView{}, err
	}
	for _, product := range data.Products {
		if _, err = s.RefreshProduct(ctx, actor, product.ID); err != nil {
			return WealthAccountView{}, err
		}
	}
	return s.Account(actor, account)
}
func (s *WealthService) Ensure(ctx context.Context) error {
	products, err := s.repo.TrackedProducts()
	if err != nil {
		return err
	}
	var first error
	for _, product := range products {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cursor, err := s.repo.SlotCursor(product.ID)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if _, err = s.syncProduct(ctx, product.ID, wealthSlots(s.Now(), cursor), true); err != nil && first == nil {
			first = err
		}
	}
	var pending []model.WealthPosition
	if err = s.repo.DB().Where("rebuild_pending = ?", true).Order("id").Limit(500).Find(&pending).Error; err != nil {
		return err
	}
	for _, p := range pending {
		if err = s.RebuildPosition(p.ID); err != nil && first == nil {
			first = err
		}
	}
	if err = s.ensureCashYields(ctx); err != nil && first == nil {
		first = err
	}
	return first
}
