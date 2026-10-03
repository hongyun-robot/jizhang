package database

import (
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"jizhang/internal/config"
	"jizhang/internal/logger"

	"go.uber.org/zap"
)

// Init 连接 MySQL 并返回 GORM 句柄。
func Init(cfg *config.Config) *gorm.DB {
	db, err := gorm.Open(mysql.Open(cfg.DBDSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		logger.L().Fatal("connect mysql failed", zap.Error(err))
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.L().Fatal("mysql pool failed", zap.Error(err))
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db
}
