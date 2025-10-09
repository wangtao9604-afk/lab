package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNotLeader       = errors.New("fetcher: not leader")
	ErrVersionConflict = errors.New("fetcher: cursor version conflict")
)

// CursorStoreOptions 描述 RedisCursorStore 所需的业务参数。
type CursorStoreOptions struct {
	KeyPrefix string
	LeaseTTL  time.Duration
	KfID      string
}

// RedisCursorStore 封装光标与租约的持久化逻辑。
type RedisCursorStore struct {
	client *redis.Client
	ttl    time.Duration

	keys struct {
		Epoch   string
		Lease   string
		Version string
	}

	keyCursorValue string
	kfID           string

	casScript   *redis.Script
	renewScript *redis.Script
}

func NewRedisCursorStore(client *redis.Client, opts CursorStoreOptions) (*RedisCursorStore, error) {
	if client == nil {
		return nil, errors.New("fetcher: redis client is nil")
	}
	if opts.KeyPrefix == "" {
		opts.KeyPrefix = "fetcher"
	}
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = 15 * time.Second
	}

	store := &RedisCursorStore{
		client:      client,
		ttl:         opts.LeaseTTL,
		casScript:   redis.NewScript(casUpdateLua),
		renewScript: redis.NewScript(renewLeaseLua),
		kfID:        opts.KfID,
	}

	store.keys.Epoch = fmt.Sprintf("%s:cursor:epoch", opts.KeyPrefix)
	store.keys.Lease = fmt.Sprintf("%s:cursor:lease", opts.KeyPrefix)
	store.keys.Version = fmt.Sprintf("%s:cursor:version", opts.KeyPrefix)
	if store.kfID != "" {
		store.keyCursorValue = fmt.Sprintf("%s:cursor:%s:value", opts.KeyPrefix, store.kfID)
	} else {
		store.keyCursorValue = fmt.Sprintf("%s:cursor:value", opts.KeyPrefix)
	}

	if err := pingRedis(context.Background(), client); err != nil {
		return nil, err
	}
	return store, nil
}

// AcquireLeadership 尝试抢占租约，返回围栏 epoch、取消续租函数和是否成功。
func (s *RedisCursorStore) AcquireLeadership(ctx context.Context) (epoch int64, cancel func(), ok bool, err error) {
	if s == nil {
		return 0, nil, false, errors.New("fetcher: cursor store is nil")
	}

	epoch, err = s.client.Incr(ctx, s.keys.Epoch).Result()
	if err != nil {
		return 0, nil, false, fmt.Errorf("fetcher: incr epoch failed: %w", err)
	}

	set, err := s.client.SetNX(ctx, s.keys.Lease, strconv.FormatInt(epoch, 10), s.ttl).Result()
	if err != nil {
		return 0, nil, false, fmt.Errorf("fetcher: setnx lease failed: %w", err)
	}
	if !set {
		return 0, nil, false, nil
	}

	stop := make(chan struct{})
	once := sync.Once{}
	cancel = func() { once.Do(func() { close(stop) }) }

	go s.renewLoop(ctx, epoch, stop)
	return epoch, cancel, true, nil
}

func (s *RedisCursorStore) renewLoop(ctx context.Context, epoch int64, stop <-chan struct{}) {
	ticker := time.NewTicker(s.ttl / 3)
	defer ticker.Stop()

	args := []interface{}{strconv.FormatInt(epoch, 10), int64(s.ttl / time.Millisecond)}
	for {
		select {
		case <-ticker.C:
			s.renewLeaseWithRetry(ctx, args)
		case <-stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *RedisCursorStore) renewLeaseWithRetry(ctx context.Context, args []interface{}) {
	const maxRetries = 3
	leaseKey := []string{s.keys.Lease}

	status := ""
	defer func() {
		if status != "" {
			ReportLeaseRenew(s.kfID, status)
		}
	}()

	for attempt := 0; attempt < maxRetries; attempt++ {
		_, err := s.renewScript.Run(ctx, s.client, leaseKey, args...).Result()
		if err == nil {
			status = "renewed"
			return
		}
		if errors.Is(err, redis.Nil) {
			status = "lost"
			return
		}
		if !isRetryableRedisErr(err) {
			status = "error"
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	status = "error"
}

// LoadCursor 返回当前 cursor 与版本号；初始时返回 "",0。
func (s *RedisCursorStore) LoadCursor(ctx context.Context) (cursor string, version int64, err error) {
	res, err := s.client.MGet(ctx, s.keyCursorValue, s.keys.Version).Result()
	if err != nil {
		return "", 0, fmt.Errorf("fetcher: mget cursor failed: %w", err)
	}

	if res[0] != nil {
		switch v := res[0].(type) {
		case string:
			cursor = v
		case []byte:
			cursor = string(v)
		}
	}
	if res[1] != nil {
		switch v := res[1].(type) {
		case string:
			version, _ = strconv.ParseInt(v, 10, 64)
		case []byte:
			version, _ = strconv.ParseInt(string(v), 10, 64)
		}
	}
	return cursor, version, nil
}

// UpdateCursorCAS 使用 Lua 检查 epoch + version，返回 Redis 脚本结果：
//
//	1=成功；0=失去领导权；-1=版本冲突；其它值视为失败。
func (s *RedisCursorStore) UpdateCursorCAS(ctx context.Context, epoch int64, expectVersion int64, newCursor string) (int64, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		ret, err := s.casScript.Run(ctx, s.client,
			[]string{s.keyCursorValue, s.keys.Version, s.keys.Lease},
			strconv.FormatInt(epoch, 10),
			strconv.FormatInt(expectVersion, 10),
			newCursor,
		).Int64()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				ReportCASResult(s.kfID, "lost")
				return 0, nil
			}
			if isRetryableRedisErr(err) && attempt < maxRetries-1 {
				lastErr = err
				time.Sleep(50 * time.Millisecond)
				continue
			}
			ReportCASResult(s.kfID, "error")
			return -2, fmt.Errorf("fetcher: lua cas failed: %w", err)
		}

		switch ret {
		case 1:
			ReportCASResult(s.kfID, "success")
			return ret, nil
		case 0:
			ReportCASResult(s.kfID, "lost")
			return ret, nil
		case -1:
			ReportCASResult(s.kfID, "conflict")
			return ret, nil
		default:
			ReportCASResult(s.kfID, "error")
			return ret, fmt.Errorf("fetcher: unexpected cas return %d", ret)
		}
	}

	if lastErr != nil {
		ReportCASResult(s.kfID, "error")
		return -2, fmt.Errorf("fetcher: lua cas failed after retries: %w", lastErr)
	}
	ReportCASResult(s.kfID, "error")
	return -2, fmt.Errorf("fetcher: lua cas failed: unknown error")
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

const casUpdateLua = `
-- KEYS: 1=valueKey, 2=versionKey, 3=leaseKey
-- ARGV: 1=epoch, 2=expectVersion, 3=newCursor
local lease = redis.call('GET', KEYS[3])
if (not lease) or (lease ~= ARGV[1]) then
  return 0
end
local ver = redis.call('GET', KEYS[2])
if (not ver) then ver = "0" end
if (tonumber(ver) ~= tonumber(ARGV[2])) then
  return -1
end
redis.call('SET', KEYS[1], ARGV[3])
redis.call('INCR', KEYS[2])
return 1
`

const renewLeaseLua = `
-- KEYS: 1=leaseKey
-- ARGV: 1=epoch, 2=ttl_ms
local lease = redis.call('GET', KEYS[1])
if (not lease) or (lease ~= ARGV[1]) then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`
