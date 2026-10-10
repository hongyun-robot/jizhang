package repository

import (
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

type VoucherPlatformRepo struct{ db *gorm.DB }

func NewVoucherPlatformRepo(db *gorm.DB) *VoucherPlatformRepo {
	return &VoucherPlatformRepo{db: db}
}

func (r *VoucherPlatformRepo) List() ([]model.VoucherPlatform, error) {
	rows := make([]model.VoucherPlatform, 0)
	if err := r.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	// 使用相同的精确排序，避免测试和生产数据库的默认排序规则不同。
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name == rows[j].Name {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

func (r *VoucherPlatformRepo) Ensure(name string) (*model.VoucherPlatform, error) {
	row := model.VoucherPlatform{Name: name}
	// 唯一约束只处理同名竞争，其他存储错误继续返回。
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	// 锁定读取使用最新已提交版本，能读取在 MySQL 事务快照建立后并发创建的同名项。
	var existing model.VoucherPlatform
	if err := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("name = ?", name).First(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}
