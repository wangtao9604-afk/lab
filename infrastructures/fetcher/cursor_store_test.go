//go:build fetcher_integration
// +build fetcher_integration

package fetcher

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*RedisCursorStore, *redis.Client, *miniredis.Miniredis, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store, err := NewRedisCursorStore(client, CursorStoreOptions{
		KeyPrefix: "test",
		LeaseTTL:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRedisCursorStore: %v", err)
	}

	cleanup := func() {
		_ = client.Close()
		mr.Close()
	}
	return store, client, mr, cleanup
}

func TestAcquireLeadershipAndRenewal(t *testing.T) {
	store, _, mr, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	epoch, cancel, ok, err := store.AcquireLeadership(ctx)
	if err != nil || !ok {
		t.Fatalf("first acquire failed: ok=%v err=%v", ok, err)
	}
	if epoch == 0 {
		t.Fatalf("expect epoch >0")
	}

	// Another attempt before lease expiry should fail
	_, _, ok, err = store.AcquireLeadership(ctx)
	if err != nil {
		t.Fatalf("second acquire error: %v", err)
	}
	if ok {
		t.Fatalf("expected second acquire to fail while lease held")
	}

	// Stop renew and wait TTL, then should acquire again
	cancel()
	mr.FastForward(time.Second)

	_, _, ok, err = store.AcquireLeadership(ctx)
	if err != nil {
		t.Fatalf("third acquire error: %v", err)
	}
	if !ok {
		t.Fatalf("expected third acquire to succeed after release")
	}
}

func TestUpdateCursorCAS(t *testing.T) {
	store, _, _, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	epoch, cancel, ok, err := store.AcquireLeadership(ctx)
	if err != nil || !ok {
		t.Fatalf("acquire failed: ok=%v err=%v", ok, err)
	}
	defer cancel()

	cursor, ver, err := store.LoadCursor(ctx)
	if err != nil {
		t.Fatalf("load cursor error: %v", err)
	}
	if cursor != "" || ver != 0 {
		t.Fatalf("unexpected initial cursor %q ver %d", cursor, ver)
	}

	if rc, err := store.UpdateCursorCAS(ctx, epoch, 0, "cursor-1"); err != nil || rc != 1 {
		if err != nil {
			t.Fatalf("first update failed: %v", err)
		}
		t.Fatalf("unexpected return code %d", rc)
	}

	// Version should now be 1
	_, ver, err = store.LoadCursor(ctx)
	if err != nil {
		t.Fatalf("load after update error: %v", err)
	}
	if ver != 1 {
		t.Fatalf("expected version 1, got %d", ver)
	}

	// Wrong version should cause conflict
	rc, err := store.UpdateCursorCAS(ctx, epoch, 0, "cursor-2")
	if err != nil || rc != -1 {
		if err != nil {
			t.Fatalf("expected version conflict, got error %v", err)
		}
		t.Fatalf("expected rc -1, got %d", rc)
	}
}
