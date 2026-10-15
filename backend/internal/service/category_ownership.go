package service

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"unicode/utf8"
)

func (s *CategoryService) TreeFor(actor *model.User) ([]*model.CategoryTreeNode, error) {
	if err := initializeCatalog(s.repo.DB(), actor, false); err != nil {
		return nil, err
	}
	return NewCategoryService(s.repo.ForOwner(actor.ID)).Tree()
}
func categoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return "", ErrInvalidCategory
	}
	return name, nil
}
func (s *CategoryService) CreateFor(actor *model.User, in CreateCategoryInput) (out *model.Category, err error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	in.Name, err = categoryName(in.Name)
	if err != nil {
		return nil, err
	}
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		if in.ParentID != nil {
			if e := checkCategoryOwnership(tx, actor, *in.ParentID); e != nil {
				return e
			}
		}
		in.OwnerID = actor.ID
		if !in.SortProvided && in.Sort == 0 {
			position, e := repository.CategoryOrder(tx, actor.ID, in.Kind, in.ParentID).NextPosition()
			if e != nil {
				return e
			}
			in.Sort = position
		}
		var e error
		out, e = NewCategoryService(repository.NewCategoryRepo(tx)).Create(in)
		return e
	})
	if err == nil {
		s.fireWrite()
	}
	return
}
func (s *CategoryService) UpdateFor(actor *model.User, id int64, name string, positions ...int) (out *model.Category, err error) {
	name, err = categoryName(name)
	if err != nil {
		return nil, err
	}
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		if e := checkCategoryOwnership(tx, actor, id); e != nil {
			return e
		}
		var e error
		repo := repository.NewCategoryRepo(tx)
		current, e := repo.GetByID(id)
		if e != nil {
			return e
		}
		position := current.Sort
		if len(positions) > 0 {
			position = positions[0]
		}
		out, e = NewCategoryService(repo).Update(id, name, position)
		return e
	})
	if err == nil {
		s.fireWrite()
	}
	return
}
func (s *CategoryService) DeleteFor(actor *model.User, id int64) error {
	err := repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		if e := checkCategoryOwnership(tx, actor, id); e != nil {
			return e
		}
		for _, ref := range []struct{ table, column string }{{"vouchers", "category_id"}, {"recurring_rules", "category_id"}, {"budgets", "category_id"}, {"installment_plans", "fee_category_id"}} {
			if tx.Migrator().HasTable(ref.table) {
				var n int64
				if e := tx.Table(ref.table).Where(ref.column+" = ?", id).Count(&n).Error; e != nil {
					return e
				}
				if n > 0 {
					return ErrCategoryHasReferences
				}
			}
		}
		return NewCategoryService(repository.NewCategoryRepo(tx)).Delete(id)
	})
	if err == nil {
		s.fireWrite()
	}
	return err
}
