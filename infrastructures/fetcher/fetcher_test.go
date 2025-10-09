//go:build fetcher_integration
// +build fetcher_integration

package fetcher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
)

func TestFetcherRun(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	f, err := New(Options{
		Redis: RedisOptions{
			Addr: mr.Addr(),
		},
		Cursor: CursorStoreOptions{
			KeyPrefix: "test-run",
			LeaseTTL:  200 * time.Millisecond,
		},
		PollInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New fetcher failed: %v", err)
	}
	defer f.Close()

	var leaderCount int32
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = f.Run(ctx, func(ctx context.Context, lease LeaderLease) error {
			atomic.AddInt32(&leaderCount, 1)
			// 立即结束当前领导周期
			lease.Release()
			return nil
		})
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fetcher Run did not exit")
	}

	if atomic.LoadInt32(&leaderCount) == 0 {
		t.Fatalf("expected callback to be invoked at least once")
	}
}
