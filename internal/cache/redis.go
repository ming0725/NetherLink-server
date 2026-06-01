package cache

import (
	"net"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"nlserver/internal/config"
)

func NewRedisClient(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         RedisAddr(cfg),
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.Database,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
}

func RedisAddr(cfg config.RedisConfig) string {
	return net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
