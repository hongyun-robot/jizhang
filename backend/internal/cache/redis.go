package cache

import (
	"context"

	"github.com/redis/go-redis/v9"

	"jizhang/internal/config"
	"jizhang/internal/logger"

	"go.uber.org/zap"
)

// Init 连接 Redis。
func Init(cfg *config.Config) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.L().Fatal("connect redis failed", zap.Error(err))
	}
	return rdb
}
