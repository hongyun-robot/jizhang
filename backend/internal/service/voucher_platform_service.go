package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

var (
	ErrPlatformNotFound   = errors.New("平台 / 商家不存在")
	ErrPlatformDuplicate  = errors.New("本人目录中已存在同名平台 / 商家")
	ErrPlatformReferenced = errors.New("该平台 / 商家仍被本人记录使用，不能删除")
)

func normalizeVoucherPlatform(name string, required bool) (string, error) {
	name = strings.TrimSpace(name)
	if (required && name == "") || utf8.RuneCountInString(name) > 128 {
		return "", fmt.Errorf("%w：平台 / 商家名称须为 1 至 128 字", ErrVoucherInvalid)
	}
	return name, nil
}

func (s *VoucherService) ListPlatforms(actor *model.User) ([]model.VoucherPlatform, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	var user model.User
	if err := s.db.Select("id", "voucher_platforms_initialized").First(&user, actor.ID).Error; err != nil {
		return nil, err
	}
	if !user.VoucherPlatformsInitialized {
		if err := repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
			return initializePlatforms(tx, actor.ID)
		}); err != nil {
			return nil, err
		}
	}
	return repository.NewVoucherPlatformRepo(s.db).List(actor.ID)
}

func (s *VoucherService) CreatePlatform(actor *model.User, name string) (out *model.VoucherPlatform, err error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	name, err = normalizeVoucherPlatform(name, true)
	if err != nil {
		return nil, err
	}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		if e := initializePlatforms(tx, actor.ID); e != nil {
			return e
		}
		var e error
		out, e = repository.NewVoucherPlatformRepo(tx).Ensure(actor.ID, name)
		return e
	})
	return
}

// initializePlatforms runs in the caller's ledger transaction, never a nested write.
func initializePlatforms(tx *gorm.DB, ownerID int64) error {
	if ownerID <= 0 {
		return ErrTxnForbidden
	}
	var user model.User
	if err := tx.Select("id", "voucher_platforms_initialized").Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, ownerID).Error; err != nil {
		return err
	}
	if user.VoucherPlatformsInitialized {
		return nil
	}
	var templates []model.VoucherPlatformTemplate
	if err := tx.Find(&templates).Error; err != nil {
		return err
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].Name < templates[j].Name })
	repo := repository.NewVoucherPlatformRepo(tx)
	for _, template := range templates {
		if _, err := repo.Ensure(ownerID, template.Name); err != nil {
			return err
		}
	}
	return tx.Model(&user).Update("voucher_platforms_initialized", true).Error
}

func ensureVoucherPlatform(tx *gorm.DB, ownerID int64, name string) error {
	if name == "" {
		return nil
	}
	if err := initializePlatforms(tx, ownerID); err != nil {
		return err
	}
	_, err := repository.NewVoucherPlatformRepo(tx).Ensure(ownerID, name)
	return err
}

func platformOwned(repo *repository.VoucherPlatformRepo, actor *model.User, id int64) (*model.VoucherPlatform, error) {
	row, err := repo.GetForUpdate(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPlatformNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.OwnerID != actor.ID {
		return nil, ErrTxnForbidden
	}
	return row, nil
}

func (s *VoucherService) RenamePlatform(actor *model.User, id int64, name string) (out *model.VoucherPlatform, err error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	name, err = normalizeVoucherPlatform(name, true)
	if err != nil {
		return nil, err
	}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		repo := repository.NewVoucherPlatformRepo(tx)
		row, e := platformOwned(repo, actor, id)
		if e != nil {
			return e
		}
		if row.Name != name {
			if e = repo.Rename(row, name); errors.Is(e, gorm.ErrDuplicatedKey) {
				return ErrPlatformDuplicate
			} else if e != nil {
				return e
			}
		}
		out = row
		return nil
	})
	return
}

func (s *VoucherService) DeletePlatform(actor *model.User, id int64) error {
	if !CanView(actor) {
		return ErrTxnForbidden
	}
	return repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		repo := repository.NewVoucherPlatformRepo(tx)
		row, err := platformOwned(repo, actor, id)
		if err != nil {
			return err
		}
		referenced, err := repo.HasReferences(actor.ID, row.Name)
		if err != nil {
			return err
		}
		if referenced {
			return ErrPlatformReferenced
		}
		return repo.Delete(row)
	})
}
