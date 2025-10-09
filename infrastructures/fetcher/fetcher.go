package fetcher

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Options 汇总构建 fetcher 所需的配置项。
type Options struct {
	Redis        RedisOptions
	Cursor       CursorStoreOptions
	PollInterval time.Duration
	Instance     string
}

// LeaderLease 暴露给回调的租约信息。
type LeaderLease struct {
	Epoch int64
	stop  func()
}

// Release 主动停止续约，让租约尽快失效。
func (l LeaderLease) Release() {
	if l.stop != nil {
		l.stop()
	}
}

// Fetcher 负责租约竞争与回调调度。
type Fetcher struct {
	redisClient *redis.Client
	store       *RedisCursorStore
	poll        time.Duration
	instance    string
}

// New 创建 Fetcher 并初始化 Redis 连接。
func New(opts Options) (*Fetcher, error) {
	client, err := newRedisClient(opts.Redis)
	if err != nil {
		return nil, err
	}

	store, err := NewRedisCursorStore(client, opts.Cursor)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	poll := opts.PollInterval
	if poll <= 0 {
		poll = 800 * time.Millisecond
	}

	instance := opts.Instance
	if instance == "" {
		instance = opts.Cursor.KeyPrefix
		if instance == "" {
			instance = "fetcher"
		}
	}

	return &Fetcher{
		redisClient: client,
		store:       store,
		poll:        poll,
		instance:    instance,
	}, nil
}

// Close 关闭底层 Redis 连接。
func (f *Fetcher) Close() error {
	if f == nil || f.redisClient == nil {
		return nil
	}
	return f.redisClient.Close()
}

// LeaderCallback 是成为 leader 后执行的业务逻辑。
type LeaderCallback func(ctx context.Context, lease LeaderLease) error

// Run 在给定 context 下循环竞争租约并执行回调。
func (f *Fetcher) Run(ctx context.Context, cb LeaderCallback) error {
	if f == nil {
		return errors.New("fetcher: nil instance")
	}
	if cb == nil {
		return errors.New("fetcher: leader callback is nil")
	}

	ticker := time.NewTicker(f.poll)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		appID, instance := f.labels()

		epoch, cancelRenew, ok, err := f.store.AcquireLeadership(ctx)
		if err != nil {
			ReportAcquireAttempt(appID, instance, "error")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(f.poll):
				continue
			}
		}
		if !ok {
			ReportAcquireAttempt(appID, instance, "miss")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		ReportAcquireAttempt(appID, instance, "success")

		leaderCtx, leaderCancel := context.WithCancel(ctx)
		lease := LeaderLease{Epoch: epoch, stop: func() {
			cancelRenew()
			leaderCancel()
		}}

		ReportLeaderChanged(appID, instance, true)
		err = cb(leaderCtx, lease)

		// 确保续约停止
		lease.Release()
		ReportLeaderChanged(appID, instance, false)

		if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			// 留出短暂间隔再重试
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(f.poll):
			}
		}
	}
}

// RedisClient 暴露底层 redis client，便于测试或外部使用。
func (f *Fetcher) RedisClient() *redis.Client {
	if f == nil {
		return nil
	}
	return f.redisClient
}

func (f *Fetcher) CursorStore() *RedisCursorStore {
	if f == nil {
		return nil
	}
	return f.store
}

func (f *Fetcher) labels() (string, string) {
	if f == nil {
		return "", ""
	}
	appID := ""
	if f.store != nil {
		appID = f.store.kfID
	}
	return appID, f.instance
}
