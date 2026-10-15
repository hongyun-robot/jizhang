package repository

import (
	"errors"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

type VoucherPlatformRepo struct{ db *gorm.DB }

func NewVoucherPlatformRepo(db *gorm.DB) *VoucherPlatformRepo {
	return &VoucherPlatformRepo{db: db}
}

func (r *VoucherPlatformRepo) List(ownerID int64) ([]model.VoucherPlatform, error) {
	rows := make([]model.VoucherPlatform, 0)
	if err := r.db.Where("owner_id = ?", ownerID).Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *VoucherPlatformRepo) Ensure(ownerID int64, name string) (*model.VoucherPlatform, error) {
	position, err := PlatformOrder(r.db, ownerID).NextPosition()
	if err != nil {
		return nil, err
	}
	row := model.VoucherPlatform{OwnerID: ownerID, Name: name, Sort: position}
	// 唯一约束只处理同名竞争，其他存储错误继续返回。
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	// 锁定读取使用最新已提交版本，能读取在 MySQL 事务快照建立后并发创建的同名项。
	var existing model.VoucherPlatform
	if err := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("owner_id = ? AND name = ?", ownerID, name).First(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *VoucherPlatformRepo) GetForUpdate(id int64) (*model.VoucherPlatform, error) {
	var row model.VoucherPlatform
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error
	return &row, err
}

func (r *VoucherPlatformRepo) Rename(row *model.VoucherPlatform, name string) error {
	err := r.db.Model(row).Where("owner_id = ?", row.OwnerID).Update("name", name).Error
	var mysqlErr *driver.MySQLError
	var sqliteErr interface{ Code() int }
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &mysqlErr) && mysqlErr.Number == 1062) ||
		(errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067) {
		return gorm.ErrDuplicatedKey
	}
	return err
}

func (r *VoucherPlatformRepo) HasReferences(ownerID int64, name string) (bool, error) {
	for _, table := range []string{"vouchers", "transactions"} {
		match := "TRIM(t.platform, ?) = ? COLLATE BINARY"
		args := []any{ownerID, " \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000", name}
		if r.db.Dialector.Name() == "mysql" {
			match = "BINARY REGEXP_REPLACE(t.platform,'^[[:space:]]+|[[:space:]]+$','') = BINARY ?"
			args = []any{ownerID, name}
		}
		var count int64
		// Table queries include retained, soft-deleted vouchers. Account ownership,
		// rather than original creator or transfer destination, determines scope.
		err := r.db.Table(table+" t").Joins("JOIN accounts a ON a.id=t.account_id").
			Where("a.owner_id = ? AND "+match, args...).Count(&count).Error
		if err != nil || count > 0 {
			return count > 0, err
		}
	}
	return false, nil
}

func (r *VoucherPlatformRepo) Delete(row *model.VoucherPlatform) error {
	return r.db.Where("owner_id = ?", row.OwnerID).Delete(row).Error
}
