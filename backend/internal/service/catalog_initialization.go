package service

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// Personal initialization locks both the ledger and user; completion and instances
// commit together. The database templates remain independent of user edits.
func initializeCatalog(db *gorm.DB, actor *model.User, assets bool) error {
	if !CanView(actor) {
		return ErrTxnForbidden
	}
	return repository.LedgerWrite(db, func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, actor.ID).Error; err != nil {
			return err
		}
		if (assets && user.AssetAccountsInitialized) || (!assets && user.CategoriesInitialized) {
			return nil
		}
		var first int64
		if err := tx.Model(&model.User{}).Select("MIN(id)").Scan(&first).Error; err != nil {
			return err
		}
		owner := actor.ID
		if assets {
			if first == owner {
				if err := tx.Model(&model.Account{}).Where("type = ? AND owner_id IS NULL", model.AccountAsset).Update("owner_id", owner).Error; err != nil {
					return err
				}
			}
			if tx.Migrator().HasTable(&model.AccountTemplate{}) {
				var templates []model.AccountTemplate
				if err := tx.Order("sort,code").Find(&templates).Error; err != nil {
					return err
				}
				for _, t := range templates {
					var n int64
					if err := tx.Model(&model.Account{}).Where("owner_id = ? AND type = ? AND name = ?", owner, model.AccountAsset, t.Name).Count(&n).Error; err != nil {
						return err
					}
					if n == 0 {
						a := model.Account{Name: t.Name, Type: model.AccountAsset, OwnerID: &owner, Status: model.StatusActive, Sort: t.Sort}
						if err := tx.Create(&a).Error; err != nil {
							return err
						}
					}
				}
			}
			return tx.Model(&user).Update("asset_accounts_initialized", true).Error
		}
		if first == owner {
			if err := tx.Model(&model.Category{}).Where("owner_id IS NULL").Update("owner_id", owner).Error; err != nil {
				return err
			}
		}
		var owned int64
		if err := tx.Model(&model.Category{}).Where("owner_id = ?", owner).Count(&owned).Error; err != nil {
			return err
		}
		if owned == 0 && tx.Migrator().HasTable(&model.CategoryTemplate{}) {
			var templates []model.CategoryTemplate
			if err := tx.Order("parent_id IS NOT NULL, sort, id").Find(&templates).Error; err != nil {
				return err
			}
			ids := map[int64]int64{}
			for _, t := range templates {
				c := model.Category{OwnerID: &owner, Kind: t.Kind, Name: t.Name, Sort: t.Sort}
				if t.ParentID != nil {
					id, ok := ids[*t.ParentID]
					if !ok {
						return ErrCategoryParentInvalid
					}
					c.ParentID = &id
				}
				if err := tx.Create(&c).Error; err != nil {
					return err
				}
				ids[t.ID] = c.ID
			}
		}
		return tx.Model(&user).Update("categories_initialized", true).Error
	})
}

func checkCategoryOwnership(db *gorm.DB, actor *model.User, ids ...int64) error {
	if !CanView(actor) {
		return ErrTxnForbidden
	}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		var category model.Category
		if err := db.First(&category, id).Error; err != nil {
			return err
		}
		if category.OwnerID == nil || *category.OwnerID != actor.ID {
			return ErrTxnForbidden
		}
	}
	return nil
}
