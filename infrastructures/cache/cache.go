package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"qywx/infrastructures/config"
	"qywx/infrastructures/log"

	"github.com/redis/go-redis/v9"
	uuid "github.com/satori/go.uuid"
)

var (
	defaultCache *Cache
	initOnce     sync.Once
)

// ErrKeyNotFound 表示Redis中不存在指定的key
var ErrKeyNotFound = errors.New("key not found")

// Cache 统一的缓存管理器
type Cache struct {
	client *redis.Client
	ctx    context.Context
}

// GetInstance 获取缓存单例，如果未初始化则自动初始化
func GetInstance() *Cache {
	initOnce.Do(func() {
		redisConfig, exists := config.GetInstance().Redises["General"]
		if !exists {
			panic("Redis configuration 'General' not found in config.toml")
		}

		// 设置默认值（只在配置文件未设置时生效）
		if redisConfig.PoolSize == 0 {
			redisConfig.PoolSize = 10
		}
		if redisConfig.MaxRetries == 0 {
			redisConfig.MaxRetries = 3
		}
		if redisConfig.DialTimeout == 0 {
			redisConfig.DialTimeout = 5
		}
		if redisConfig.ReadTimeout == 0 {
			redisConfig.ReadTimeout = 3
		}
		if redisConfig.WriteTimeout == 0 {
			redisConfig.WriteTimeout = 3
		}

		var client *redis.Client
		dial := time.Duration(redisConfig.DialTimeout) * time.Second
		read := time.Duration(redisConfig.ReadTimeout) * time.Second
		write := time.Duration(redisConfig.WriteTimeout) * time.Second

		if redisConfig.UseSentinel {
			if len(redisConfig.SentinelAddrs) == 0 {
				panic("Redis sentinel mode enabled but sentinelAddrs is empty")
			}
			if redisConfig.MasterName == "" {
				panic("Redis sentinel mode enabled but masterName is empty")
			}
			client = redis.NewFailoverClient(&redis.FailoverOptions{
				MasterName:       redisConfig.MasterName,
				SentinelAddrs:    redisConfig.SentinelAddrs,
				SentinelPassword: redisConfig.SentinelPassword,
				Username:         redisConfig.User,
				Password:         redisConfig.Password,
				DB:               redisConfig.DB,
				PoolSize:         redisConfig.PoolSize,
				MinIdleConns:     redisConfig.MinIdleConns,
				DialTimeout:      dial,
				ReadTimeout:      read,
				WriteTimeout:     write,
				MaxRetries:       redisConfig.MaxRetries,
			})
		} else {
			client = redis.NewClient(&redis.Options{
				Addr:         redisConfig.Addr,
				Username:     redisConfig.User,
				Password:     redisConfig.Password,
				DB:           redisConfig.DB,
				PoolSize:     redisConfig.PoolSize,
				MinIdleConns: redisConfig.MinIdleConns,
				MaxRetries:   redisConfig.MaxRetries,
				DialTimeout:  dial,
				ReadTimeout:  read,
				WriteTimeout: write,
			})
		}

		ctx := context.Background()

		if err := pingWithRetry(ctx, client, 3); err != nil {
			panic(fmt.Sprintf("failed to connect to redis: %v", err))
		}

		defaultCache = &Cache{
			client: client,
			ctx:    ctx,
		}

		if redisConfig.UseSentinel {
			log.Infof("Cache initialized via sentinel %s (master=%s)", strings.Join(redisConfig.SentinelAddrs, ","), redisConfig.MasterName)
		} else {
			log.Infof("Cache initialized: %s", redisConfig.Addr)
		}
	})

	return defaultCache
}

// Get 获取单例实例（向后兼容，等同于GetInstance）
func Get() *Cache {
	return GetInstance()
}

func pingWithRetry(ctx context.Context, client *redis.Client, maxRetries int) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := client.Ping(ctx).Err(); err != nil {
			lastErr = err
			if !isRetryableRedisErr(err) || attempt == maxRetries-1 {
				return err
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return nil
	}
	return lastErr
}

func isRetryableRedisErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

// Close 关闭Redis连接
func (c *Cache) Close() error {
	return c.client.Close()
}

// ============ 核心API ============

// Store 存储对象（自动JSON序列化）
func (c *Cache) Store(key string, val any, timeout time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("marshal failed for key %s: %w", key, err)
	}
	return c.StoreBytes(key, data, timeout)
}

// StoreBytes 存储原始字节
func (c *Cache) StoreBytes(key string, data []byte, timeout time.Duration) error {
	err := c.client.Set(c.ctx, key, data, timeout).Err()
	if err != nil {
		return fmt.Errorf("store failed for key %s: %w", key, err)
	}
	return nil
}

// StoreString 直接存储字符串（不进行JSON序列化）
func (c *Cache) StoreString(key string, val string, timeout time.Duration) error {
	err := c.client.Set(c.ctx, key, val, timeout).Err()
	if err != nil {
		return fmt.Errorf("store string failed for key %s: %w", key, err)
	}
	return nil
}

// Fetch 获取对象（自动JSON反序列化）
func (c *Cache) Fetch(key string, dest any) error {
	data, err := c.FetchBytes(key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal failed for key %s: %w", key, err)
	}
	return nil
}

// FetchBytes 获取原始字节
func (c *Cache) FetchBytes(key string) ([]byte, error) {
	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, key)
		}
		return nil, fmt.Errorf("fetch failed for key %s: %w", key, err)
	}
	return data, nil
}

// FetchString 获取字符串
func (c *Cache) FetchString(key string) (string, error) {
	val, err := c.client.Get(c.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("%w: %s", ErrKeyNotFound, key)
		}
		return "", fmt.Errorf("fetch string failed for key %s: %w", key, err)
	}
	return val, nil
}

// Delete 删除键
func (c *Cache) Delete(key string) error {
	err := c.client.Del(c.ctx, key).Err()
	if err != nil {
		return fmt.Errorf("delete failed for key %s: %w", key, err)
	}
	return nil
}

// Exists 检查键是否存在
func (c *Cache) Exists(key string) bool {
	count, err := c.client.Exists(c.ctx, key).Result()
	if err != nil {
		log.Errorf("check exists failed: %v, key: %s", err, key)
		return false
	}
	return count > 0
}

// TTL 获取键的剩余过期时间
func (c *Cache) TTL(key string) (time.Duration, error) {
	ttl, err := c.client.TTL(c.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("get ttl failed for key %s: %w", key, err)
	}
	if ttl < 0 {
		return 0, fmt.Errorf("key %s has no expiration or does not exist", key)
	}
	return ttl, nil
}

// Expire 设置键的过期时间
func (c *Cache) Expire(key string, timeout time.Duration) error {
	err := c.client.Expire(c.ctx, key, timeout).Err()
	if err != nil {
		return fmt.Errorf("set expire failed for key %s: %w", key, err)
	}
	return nil
}

// ============ 高级功能 ============

// Increment 原子递增
func (c *Cache) Increment(key string) (int64, error) {
	val, err := c.client.Incr(c.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("increment failed for key %s: %w", key, err)
	}
	return val, nil
}

// IncrementBy 原子递增指定值
func (c *Cache) IncrementBy(key string, value int64) (int64, error) {
	val, err := c.client.IncrBy(c.ctx, key, value).Result()
	if err != nil {
		return 0, fmt.Errorf("increment by failed for key %s: %w", key, err)
	}
	return val, nil
}

// SetNX 仅在键不存在时设置（用于分布式锁）
func (c *Cache) SetNX(key string, val any, timeout time.Duration) (bool, error) {
	data, err := json.Marshal(val)
	if err != nil {
		return false, fmt.Errorf("marshal failed for key %s: %w", key, err)
	}

	ok, err := c.client.SetNX(c.ctx, key, data, timeout).Result()
	if err != nil {
		return false, fmt.Errorf("setnx failed for key %s: %w", key, err)
	}
	return ok, nil
}

// ============ 业务方法 ============

// StoreWithToken 存储数据并返回访问令牌
func (c *Cache) StoreWithToken(val any, timeoutSeconds int) (string, error) {
	token := uuid.NewV4().String()
	key := c.makeKeyPrefix(token)
	err := c.Store(key, val, time.Duration(timeoutSeconds)*time.Second)
	return token, err
}

// FetchWithToken 使用令牌获取数据
func (c *Cache) FetchWithToken(token string, dest any) error {
	key := c.makeKeyPrefix(token)
	return c.Fetch(key, dest)
}

// FetchAndDelete 获取数据后删除（一次性令牌）
func (c *Cache) FetchAndDelete(token string, dest any) error {
	key := c.makeKeyPrefix(token)
	if err := c.Fetch(key, dest); err != nil {
		return err
	}
	// 获取成功后删除
	if err := c.Delete(key); err != nil {
		log.Errorf("delete after fetch failed: %v, key: %s", err, key)
	}
	return nil
}

func (c *Cache) makeKeyPrefix(token string) string {
	return fmt.Sprintf("qywx:access_token:%s", token)
}

// ============ 便利的包级函数 ============

// Store 直接使用默认实例存储
func Store(key string, val any, timeout time.Duration) error {
	return GetInstance().Store(key, val, timeout)
}

// Fetch 直接使用默认实例获取
func Fetch(key string, dest any) error {
	return GetInstance().Fetch(key, dest)
}

// Delete 直接使用默认实例删除
func Delete(key string) error {
	return GetInstance().Delete(key)
}

// Exists 直接使用默认实例检查存在性
func Exists(key string) bool {
	return GetInstance().Exists(key)
}
