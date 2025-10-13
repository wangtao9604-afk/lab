//go:build ignore

// fetcher_redis_offline.go
// Redis Fetcher with Local Cursor Shadow (offline fast-forward) and split-brain protection.
//
// What you get in this single file:
//   - Redis leader election (lease + renew) with idempotent cancel
//   - CAS-based cursor update with fencing (epoch) and version check
//   - **Local shadow file**: when remote is unreachable, keep pulling and persist cursor locally (Dirty=true)
//   - **Startup fast-forward**: if local shadow is Dirty, first fast-forward remote cursor to local using CAS
//   - **Version conflict policy**: on rc == -1, DO NOT rollback to remote cursor; only refresh remote version
//   - **Offline protection window**: MaxOfflineDuration to bound split-brain during asymmetric partitions
//
// Required external types (replace import path to your actual module):
//   - WeComClient.FetchBatch(ctx, cursor) ([]WeComMessage, nextCursor string, hasMore bool, err error)
//   - KafkaOut.Produce(msg WeComMessage) error
//
// Notes:
//   - This file is self-contained (also includes a robust file-based local store).
//   - Replace "your/module/path/qywx_cursor_fetcher/shared" with your repo path.
//   - Works with Redis single node or Sentinel (go-redis v9 Failover client).
//
// Author: you
// License: MIT (optional)

package redisfetcher_offline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"your/module/path/qywx_cursor_fetcher/shared"
)

// =============================
// Redis client & cursor store
// =============================

type RedisConf struct {
	// Single
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`

	// Sentinel
	UseSentinel   bool     `yaml:"useSentinel"`
	SentinelAddrs []string `yaml:"sentinelAddrs"`
	MasterName    string   `yaml:"masterName"`
	SentinelPass  string   `yaml:"sentinelPassword"`

	// Lease
	LeaseTTLSeconds int `yaml:"leaseTTLSeconds"` // e.g. 10~30
}

func NewRedisClient(conf RedisConf) (*redis.Client, error) {
	if conf.UseSentinel {
		return redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       conf.MasterName,
			SentinelAddrs:    conf.SentinelAddrs,
			SentinelPassword: conf.SentinelPass,
			Password:         conf.Password,
			DB:               conf.DB,
			MaxRetries:       2,
			DialTimeout:      5 * time.Second,
			ReadTimeout:      2 * time.Second,
			WriteTimeout:     2 * time.Second,
		}), nil
	}
	return redis.NewClient(&redis.Options{
		Addr:         conf.Addr,
		Password:     conf.Password,
		DB:           conf.DB,
		MaxRetries:   2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}), nil
}

// RedisCursorStore keeps cursor and lease keys in Redis.
type RedisCursorStore struct {
	rdb  *redis.Client
	keys struct {
		EpochCounter string // fencing token counter
		LeaseKey     string // lease value = epoch
		ValueKey     string // cursor value
		VersionKey   string // cursor version (int, starts at 0)
	}
	ttl time.Duration

	// Lua scripts
	casUpdate *redis.Script
	renew     *redis.Script
}

func NewRedisCursorStore(rdb *redis.Client, keyPrefix string, leaseTTL time.Duration) *RedisCursorStore {
	s := &RedisCursorStore{rdb: rdb, ttl: leaseTTL}
	s.keys.EpochCounter = keyPrefix + ":cursor:epoch"
	s.keys.LeaseKey = keyPrefix + ":cursor:lease"
	s.keys.ValueKey = keyPrefix + ":cursor:value"
	s.keys.VersionKey = keyPrefix + ":cursor:version"

	s.casUpdate = redis.NewScript(casUpdateLua)
	s.renew = redis.NewScript(renewLeaseLua)
	return s
}

// AcquireLeadership tries to become leader; returns epoch & a cancel func for lease renew (idempotent).
func (s *RedisCursorStore) AcquireLeadership(ctx context.Context) (epoch int64, cancelRenew func(), ok bool, err error) {
	// 1) Fence token (monotonic epoch)
	epoch, err = s.rdb.Incr(ctx, s.keys.EpochCounter).Result()
	if err != nil {
		return 0, nil, false, fmt.Errorf("incr epoch: %w", err)
	}
	// 2) Compete lease: SETNX lease=epoch PX ttl
	set, err := s.rdb.SetNX(ctx, s.keys.LeaseKey, strconv.FormatInt(epoch, 10), s.ttl).Result()
	if err != nil {
		return 0, nil, false, fmt.Errorf("setnx lease: %w", err)
	}
	if !set {
		return 0, nil, false, nil // lost race
	}
	// 3) Renew goroutine (idempotent cancel)
	stop := make(chan struct{})
	var once sync.Once
	cancel := func() { once.Do(func() { close(stop) }) }
	go func() {
		ticker := time.NewTicker(s.ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = s.renew.Run(ctx, s.rdb, []string{s.keys.LeaseKey},
					strconv.FormatInt(epoch, 10), int64(s.ttl/time.Millisecond)).Result()
			case <-stop, <-ctx.Done():
				return
			}
		}
	}()
	return epoch, cancel, true, nil
}

// LoadCursor reads (cursor, version) from Redis.
func (s *RedisCursorStore) LoadCursor(ctx context.Context) (cursor string, version int64, err error) {
	res, err := s.rdb.MGet(ctx, s.keys.ValueKey, s.keys.VersionKey).Result()
	if err != nil {
		return "", 0, err
	}
	cur := ""
	if res[0] != nil {
		switch v := res[0].(type) {
		case string:
			cur = v
		case []byte:
			cur = string(v)
		}
	}
	var ver int64 = 0
	if res[1] != nil {
		switch v := res[1].(type) {
		case string:
			ver, _ = strconv.ParseInt(v, 10, 64)
		case []byte:
			ver, _ = strconv.ParseInt(string(v), 10, 64)
		}
	}
	return cur, ver, nil
}

// UpdateCursorCAS: 1=success, 0=not-leader (lease lost), -1=version-conflict, -2=other error
func (s *RedisCursorStore) UpdateCursorCAS(ctx context.Context, epoch int64, expectVersion int64, newCursor string) (int64, error) {
	ret, err := s.casUpdate.Run(ctx, s.rdb,
		[]string{s.keys.ValueKey, s.keys.VersionKey, s.keys.LeaseKey},
		strconv.FormatInt(epoch, 10), strconv.FormatInt(expectVersion, 10), newCursor).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return -2, err
	}
	return ret, nil
}

// Lua scripts.
const casUpdateLua = `
-- KEYS: 1=valueKey, 2=versionKey, 3=leaseKey
-- ARGV: 1=epoch, 2=expectVersion, 3=newCursor
local lease = redis.call('GET', KEYS[3])
if (not lease) or (lease ~= ARGV[1]) then
  return 0 -- not leader
end
local ver = redis.call('GET', KEYS[2])
if (not ver) then ver = "0" end
if (tonumber(ver) ~= tonumber(ARGV[2])) then
  return -1 -- version conflict
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
  return 0 -- not leader anymore
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`

// =============================
// Local shadow store (atomic file)
// =============================

// Record is persisted on disk to survive process crash and to mark Dirty (unsynced-to-remote).
type Record struct {
	AppID     string `json:"app_id"`
	Epoch     int64  `json:"epoch"`      // fence token (audit only)
	Version   int64  `json:"version"`    // local version, ++ on offline step
	Cursor    string `json:"cursor"`     // latest cursor
	Dirty     bool   `json:"dirty"`      // true if not yet synced to remote
	UpdatedAt int64  `json:"updated_at"` // unix seconds
}

// FileCursorStore persists Record atomically: tmp -> fsync -> rename -> dir fsync.
type FileCursorStore struct{ path string }

func NewFileCursorStore(path string) *FileCursorStore { return &FileCursorStore{path: path} }

func (s *FileCursorStore) Load(ctx context.Context) (*Record, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &r, nil
}

func (s *FileCursorStore) Save(ctx context.Context, r *Record) error {
	tmp := s.path + ".tmp"
	dir := filepath.Dir(s.path)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	// defer only for early exits; success path sets f=nil
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	r.UpdatedAt = time.Now().Unix()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil { // needed for Windows rename
		return fmt.Errorf("close tmp: %w", err)
	}
	f = nil

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	if df, err := os.Open(dir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

// =============================
// Fetcher with offline fast-forward
// =============================

type OfflineConf struct {
	AppID              string        `yaml:"appId"`
	LocalPath          string        `yaml:"localPath"`
	MaxOfflineDuration time.Duration `yaml:"maxOfflineDuration"` // 0 = unlimited (not recommended)
	PollIntervalNoData time.Duration `yaml:"pollIntervalNoData"` // e.g. 1s
}

type Fetcher struct {
	Store *RedisCursorStore
	Local *FileCursorStore
	Wx    shared.WeComClient
	Out   *shared.KafkaOut
	Conf  OfflineConf
}

func (f *Fetcher) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		epoch, cancelRenew, ok, err := f.Store.AcquireLeadership(ctx)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if !ok {
			time.Sleep(800 * time.Millisecond)
			continue
		}

		err = f.runAsLeader(ctx, epoch) // run loop only; renewal cancel is owned hereafter
		if cancelRenew != nil {
			cancelRenew() // idempotent
		}
		if err != nil && ctx.Err() == nil {
			time.Sleep(time.Second)
		}
	}
	return ctx.Err()
}

// runAsLeader performs: startup reconciliation (local Dirty -> remote fast-forward) then main loop.
func (f *Fetcher) runAsLeader(ctx context.Context, epoch int64) error {
	// ---- Startup reconciliation ----
	localRec, _ := f.Local.Load(ctx)

	curRemote, verRemote, err := f.Store.LoadCursor(ctx)
	var cur string
	var verLocal int64 = 0

	if err == nil {
		cur = curRemote
		// If local Dirty exists, first fast-forward remote to local cursor (CAS)
		if localRec != nil && localRec.Dirty && localRec.Cursor != "" {
			const maxSync = 5
			for i := 0; i < maxSync; i++ {
				rc, errCAS := f.Store.UpdateCursorCAS(ctx, epoch, verRemote, localRec.Cursor)
				if errCAS != nil {
					// transient remote error; fall back to main loop (we won't rollback cur)
					break
				}
				if rc == 1 {
					// remote advanced to local; write clean snapshot
					verRemote++
					cur = localRec.Cursor
					_ = f.Local.Save(ctx, &Record{
						AppID: f.Conf.AppID, Epoch: epoch, Version: verRemote, Cursor: cur, Dirty: false,
					})
					break
				}
				if rc == 0 {
					// lost leadership
					return nil
				}
				// rc == -1: version conflict → refresh version and retry if needed
				cr, vr, e := f.Store.LoadCursor(ctx)
				if e != nil {
					break
				}
				verRemote = vr
				if cr == localRec.Cursor {
					cur = cr
					_ = f.Local.Save(ctx, &Record{
						AppID: f.Conf.AppID, Epoch: epoch, Version: verRemote, Cursor: cur, Dirty: false,
					})
					break
				}
			}
		} else {
			// write a clean snapshot for crash safety
			_ = f.Local.Save(ctx, &Record{
				AppID: f.Conf.AppID, Epoch: epoch, Version: verRemote, Cursor: cur, Dirty: false,
			})
		}
	} else {
		// remote unreachable: continue from local shadow (if any)
		if localRec != nil {
			cur = localRec.Cursor
			verLocal = localRec.Version
		} else {
			cur = "" // or any initial cursor agreed with upstream
		}
	}

	// ---- Main loop ----
	offlineSince := time.Time{}
	for ctx.Err() == nil {
		// 1) Pull a batch from WeCom
		msgs, nextCur, hasMore, fetchErr := f.Wx.FetchBatch(ctx, cur)
		if fetchErr != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 2) Fan-out to Kafka
		for _, m := range msgs {
			_ = f.Out.Produce(m) // DLQ inside, do not block pulling
		}

		// 3) Try to advance remote cursor with CAS
		rc, casErr := f.Store.UpdateCursorCAS(ctx, epoch, verRemote, nextCur)
		switch rc {
		case 1:
			// success: remote ver++, clear offline window, write clean snapshot
			verRemote++
			cur = nextCur
			offlineSince = time.Time{}
			_ = f.Local.Save(ctx, &Record{
				AppID: f.Conf.AppID, Epoch: epoch, Version: verRemote, Cursor: cur, Dirty: false,
			})

		case 0:
			// lost leadership: exit to outer Run() to re-elect
			return nil

		case -1:
			// version conflict: DO NOT rollback to remote cursor; only refresh remote version
			cr, vr, e := f.Store.LoadCursor(ctx)
			if e != nil {
				// remote just went unreachable → offline advance with local shadow
				if offlineSince.IsZero() {
					offlineSince = time.Now()
				}
				verLocal++
				cur = nextCur // keep our progress
				_ = f.Local.Save(ctx, &Record{
					AppID: f.Conf.AppID, Epoch: epoch, Version: verLocal, Cursor: cur, Dirty: true,
				})
				if f.Conf.MaxOfflineDuration > 0 && time.Since(offlineSince) > f.Conf.MaxOfflineDuration {
					return fmt.Errorf("offline too long, exit leadership")
				}
				continue
			}
			_ = cr        // we intentionally DO NOT adopt remote cursor
			verRemote = vr
			continue // try next round with updated expectVersion

		default:
			// -2 or casErr != nil: remote unreachable → offline advance
			_ = casErr
			if offlineSince.IsZero() {
				offlineSince = time.Now()
			}
			verLocal++
			cur = nextCur
			_ = f.Local.Save(ctx, &Record{
				AppID: f.Conf.AppID, Epoch: epoch, Version: verLocal, Cursor: cur, Dirty: true,
			})
			if f.Conf.MaxOfflineDuration > 0 && time.Since(offlineSince) > f.Conf.MaxOfflineDuration {
				return fmt.Errorf("offline too long, exit leadership")
			}
			continue
		}

		// 4) No more data → backoff a bit
		if !hasMore {
			if f.Conf.PollIntervalNoData > 0 {
				time.Sleep(f.Conf.PollIntervalNoData)
			} else {
				time.Sleep(time.Second)
			}
		}
	}
	return ctx.Err()
}
