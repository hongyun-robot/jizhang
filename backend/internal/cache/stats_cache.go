package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"jizhang/internal/logger"

	"go.uber.org/zap"
)

// StatsCache 统计短缓存接口（service 层依赖倒置，便于单测）。
type StatsCache interface {
	// Get 返回 (值, 是否命中)。
	Get(key string) (string, bool, error)
	// Set 写入带 TTL 的缓存。
	Set(key, value string, ttl time.Duration) error
	// DelByPrefix 删除指定前缀的全部键（写操作主动失效）。
	DelByPrefix(prefix string) error
}

// redisStatsCache 基于 Redis 的统计缓存实现。
type redisStatsCache struct {
	rdb *redis.Client
}

// NewStatsCache 构造 Redis 统计缓存。
func NewStatsCache(rdb *redis.Client) StatsCache {
	return &redisStatsCache{rdb: rdb}
}

func (c *redisStatsCache) Get(key string) (string, bool, error) {
	val, err := c.rdb.Get(context.Background(), key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (c *redisStatsCache) Set(key, value string, ttl time.Duration) error {
	return c.rdb.Set(context.Background(), key, value, ttl).Err()
}

func (c *redisStatsCache) DelByPrefix(prefix string) error {
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	logger.L().Debug("stats cache invalidated", zap.String("prefix", prefix))
	return nil
}
