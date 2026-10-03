package seed

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"jizhang/internal/config"
	"jizhang/internal/logger"
	"jizhang/internal/model"

	"go.uber.org/zap"
)

// Admin 幂等创建初始管理员：已存在任何用户时跳过。
func Admin(db *gorm.DB, cfg *config.Config) error {
	var n int64
	if err := db.Model(&model.User{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := &model.User{
		Username:     cfg.AdminUsername,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
		Status:       model.StatusActive,
	}
	if err := db.Create(admin).Error; err != nil {
		return err
	}
	logger.L().Info("seeded admin user", zap.String("username", admin.Username))
	return nil
}
