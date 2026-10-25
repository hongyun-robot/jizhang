package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// Reorder changes the submitted visible rows in their existing global slots.
// Filtering or pagination never removes the order of rows outside that view.
func (s *TransactionService) Reorder(actor *model.User, in CatalogOrderInput, key string) (*CatalogOrderResult, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	if err := validateOrder(in); err != nil {
		return nil, err
	}
	if len(in.OrderedIDs) < 2 || len(key) < 1 || len(key) > 128 || strings.TrimSpace(key) != key {
		return nil, ErrOrderInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	filter := s.personalFilter(actor, repository.TxnFilter{CustomOrder: true})
	err = repository.LedgerWrite(s.deps.Accounts.DB(), func(tx *gorm.DB) error {
		r := repository.NewTransactionOrder(tx, actor.ID)
		found, err := r.Requested(in.OrderedIDs)
		if err != nil {
			return err
		}
		if len(found) != len(in.OrderedIDs) {
			return ErrOrderNotFound
		}
		ids, err := r.IDs(filter)
		if err != nil {
			return err
		}
		selected := make(map[int64]bool, len(in.OrderedIDs))
		for _, id := range in.OrderedIDs {
			selected[id] = true
		}
		current := make([]int64, 0, len(selected))
		for _, id := range ids {
			if selected[id] {
				current = append(current, id)
			}
		}
		if len(current) != len(selected) {
			return ErrTxnForbidden
		}
		previous, err := r.Request(key)
		if err != nil {
			return err
		}
		if previous != nil {
			if previous.Digest != digest || !slices.Equal(current, in.OrderedIDs) {
				return ErrOrderConflict
			}
			return nil
		}
		if !slices.Equal(current, in.PreviousIDs) {
			return ErrOrderConflict
		}
		position := 0
		for i, id := range ids {
			if selected[id] {
				ids[i] = in.OrderedIDs[position]
				position++
			}
		}
		if err := r.Save(ids); err != nil {
			return err
		}
		return r.Remember(key, digest)
	})
	if err != nil {
		return nil, err
	}
	return &CatalogOrderResult{OrderedIDs: slices.Clone(in.OrderedIDs)}, nil
}
