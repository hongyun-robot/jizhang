package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"slices"
	"strings"
)

var (
	ErrOrderInvalid  = errors.New("排序参数无效，请提交同一列表的完整且不重复的标识")
	ErrOrderConflict = errors.New("列表已变化，请刷新后重新排序")
	ErrOrderNotFound = errors.New("排序条目不存在，请刷新列表")
)

type CatalogOrderInput struct {
	OrderedIDs  []int64 `json:"ordered_ids"`
	PreviousIDs []int64 `json:"previous_ids"`
}
type CatalogOrderResult struct {
	OrderedIDs []int64 `json:"ordered_ids"`
}
type CategoryOrderInput struct {
	CatalogOrderInput
	Kind     model.CategoryKind `json:"kind"`
	ParentID *int64             `json:"parent_id"`
}

func validateOrder(in CatalogOrderInput) error {
	if in.OrderedIDs == nil || in.PreviousIDs == nil || len(in.OrderedIDs) != len(in.PreviousIDs) {
		return ErrOrderInvalid
	}
	seen := map[int64]bool{}
	for _, id := range in.OrderedIDs {
		if id <= 0 || seen[id] {
			return ErrOrderInvalid
		}
		seen[id] = true
	}
	for _, id := range in.PreviousIDs {
		if !seen[id] {
			return ErrOrderInvalid
		}
		delete(seen, id)
	}
	return nil
}
func saveCatalogOrder(db *gorm.DB, actor *model.User, in CatalogOrderInput, initialized func(model.User) bool, scope func(*gorm.DB) *repository.CatalogOrder, category *CategoryOrderInput, requestKeys ...string) (*CatalogOrderResult, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	if err := validateOrder(in); err != nil {
		return nil, err
	}
	key := uuid.NewString()
	if len(requestKeys) > 0 {
		key = requestKeys[0]
	}
	if len(key) < 1 || len(key) > 128 || strings.TrimSpace(key) != key {
		return nil, ErrOrderInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	err = repository.LedgerWrite(db, func(tx *gorm.DB) error {
		s := scope(tx)
		if category != nil && category.ParentID != nil {
			parents, err := s.Requested([]int64{*category.ParentID})
			if err != nil {
				return err
			}
			if len(parents) != 1 {
				return ErrOrderNotFound
			}
			p := parents[0]
			if p.OwnerID == nil || *p.OwnerID != actor.ID {
				return ErrTxnForbidden
			}
			if p.ParentID != nil || p.Kind != category.Kind {
				return ErrOrderInvalid
			}
		}
		rows, err := s.Requested(in.OrderedIDs)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.OwnerID == nil || *row.OwnerID != actor.ID {
				return ErrTxnForbidden
			}
		}
		if len(rows) != len(in.OrderedIDs) {
			return ErrOrderNotFound
		}
		for _, row := range rows {
			if !s.Matches(row) {
				return ErrOrderInvalid
			}
		}
		user, err := s.User()
		if err != nil {
			return err
		}
		if !initialized(user) {
			return ErrOrderConflict
		}
		current, err := s.IDs()
		if err != nil {
			return err
		}
		if len(current) != len(in.OrderedIDs) {
			return ErrOrderConflict
		}
		request, err := s.Request(key)
		if err != nil {
			return err
		}
		if request != nil {
			if request.Digest != digest || !slices.Equal(current, in.OrderedIDs) {
				return ErrOrderConflict
			}
			return nil
		}
		if slices.Equal(current, in.OrderedIDs) {
			return s.Remember(key, digest)
		}
		if !slices.Equal(current, in.PreviousIDs) {
			return ErrOrderConflict
		}
		if err := s.Save(in.OrderedIDs); err != nil {
			return err
		}
		return s.Remember(key, digest)
	})
	if err != nil {
		return nil, err
	}
	return &CatalogOrderResult{OrderedIDs: slices.Clone(in.OrderedIDs)}, nil
}
func (s *CategoryService) ReorderFor(actor *model.User, in CategoryOrderInput, keys ...string) (*CatalogOrderResult, error) {
	if in.Kind != model.KindExpense && in.Kind != model.KindIncome || in.ParentID != nil && *in.ParentID <= 0 {
		return nil, ErrOrderInvalid
	}
	out, err := saveCatalogOrder(s.repo.DB(), actor, in.CatalogOrderInput, func(u model.User) bool { return u.CategoriesInitialized }, func(tx *gorm.DB) *repository.CatalogOrder {
		return repository.CategoryOrder(tx, actor.ID, in.Kind, in.ParentID)
	}, &in, keys...)
	if err == nil {
		s.fireWrite()
	}
	return out, err
}
func (s *AccountService) ReorderFor(actor *model.User, in CatalogOrderInput, keys ...string) (*CatalogOrderResult, error) {
	out, err := saveCatalogOrder(s.repo.DB(), actor, in, func(u model.User) bool { return u.AssetAccountsInitialized && u.LiabilityAccountsInitialized }, func(tx *gorm.DB) *repository.CatalogOrder { return repository.AccountOrder(tx, actor.ID) }, nil, keys...)
	if err == nil {
		s.fireWrite()
	}
	return out, err
}
func (s *VoucherService) ReorderPlatforms(actor *model.User, in CatalogOrderInput, keys ...string) (*CatalogOrderResult, error) {
	return saveCatalogOrder(s.db, actor, in, func(u model.User) bool { return u.VoucherPlatformsInitialized }, func(tx *gorm.DB) *repository.CatalogOrder { return repository.PlatformOrder(tx, actor.ID) }, nil, keys...)
}
