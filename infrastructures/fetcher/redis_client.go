package fetcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisOptions 描述 fetcher 访问 cursor 存储所需的 Redis 连接参数。
type RedisOptions struct {
	UseSentinel      bool
	Addr             string
	Password         string
	DB               int
	SentinelAddrs    []string
	SentinelPassword string
	MasterName       string

	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
	MinIdleConns int
}

func (o *RedisOptions) normalize() {
	if o.DialTimeout <= 0 {
		o.DialTimeout = 5 * time.Second
	}
	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 2 * time.Second
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 2 * time.Second
	}
	if o.PoolSize <= 0 {
		o.PoolSize = 10
	}
	if o.MinIdleConns < 0 {
		o.MinIdleConns = 0
	}
}

func newRedisClient(opts RedisOptions) (*redis.Client, error) {
	opts.normalize()

	if opts.UseSentinel {
		if len(opts.SentinelAddrs) == 0 {
			return nil, errors.New("fetcher: sentinel mode enabled but sentinelAddrs is empty")
		}
		if opts.MasterName == "" {
			return nil, errors.New("fetcher: sentinel mode requires masterName")
		}
		return redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       opts.MasterName,
			SentinelAddrs:    opts.SentinelAddrs,
			SentinelPassword: opts.SentinelPassword,
			Password:         opts.Password,
			DB:               opts.DB,
			DialTimeout:      opts.DialTimeout,
			ReadTimeout:      opts.ReadTimeout,
			WriteTimeout:     opts.WriteTimeout,
			PoolSize:         opts.PoolSize,
			MinIdleConns:     opts.MinIdleConns,
		}), nil
	}

	if opts.Addr == "" {
		return nil, errors.New("fetcher: addr is required in standalone mode")
	}

	return redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		PoolSize:     opts.PoolSize,
		MinIdleConns: opts.MinIdleConns,
	}), nil
}

func pingRedis(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return errors.New("fetcher: nil redis client")
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("fetcher: ping redis failed: %w", err)
	}
	return nil
}
