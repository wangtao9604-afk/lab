package fetcher

import (
	"sync/atomic"
	"time"
)

// Hooks defines optional callbacks for fetcher observability.
type Hooks struct {
	OnAcquireAttempt   func(appID, instance, result string)
	OnLeaderChanged    func(appID, instance string, leader bool)
	OnCASResult        func(appID, result string)
	OnLeaseRenew       func(appID, result string)
	OnPullBatch        func(appID string, latency time.Duration, size int)
	OnFanout           func(appID string, count int)
	OnOfflineStart     func(appID string, at time.Time)
	OnOfflineStop      func(appID string, startedAt time.Time, duration time.Duration)
	OnLocalShadowDirty func(appID string, dirty bool)
}

var injectedHooks atomic.Value

func init() {
	injectedHooks.Store(Hooks{})
}

// WithHooks installs the provided callbacks globally. Passing a zero value resets to no-op.
func WithHooks(h Hooks) {
	injectedHooks.Store(h)
}

func currentHooks() Hooks {
	v := injectedHooks.Load()
	return v.(Hooks)
}

// ReportAcquireAttempt notifies observers about leadership acquire attempts.
func ReportAcquireAttempt(appID, instance, result string) {
	if cb := currentHooks().OnAcquireAttempt; cb != nil {
		cb(appID, instance, result)
	}
}

// ReportLeaderChanged notifies observers about leadership status change.
func ReportLeaderChanged(appID, instance string, leader bool) {
	if cb := currentHooks().OnLeaderChanged; cb != nil {
		cb(appID, instance, leader)
	}
}

// ReportCASResult records the outcome of a cursor CAS operation.
func ReportCASResult(appID, result string) {
	if cb := currentHooks().OnCASResult; cb != nil {
		cb(appID, result)
	}
}

// ReportLeaseRenew records the outcome of a lease renew attempt.
func ReportLeaseRenew(appID, result string) {
	if cb := currentHooks().OnLeaseRenew; cb != nil {
		cb(appID, result)
	}
}

// ReportPullBatch reports the latency and size of a pull batch.
func ReportPullBatch(appID string, latency time.Duration, size int) {
	if cb := currentHooks().OnPullBatch; cb != nil {
		cb(appID, latency, size)
	}
}

// ReportFanout records the number of messages fanout to downstream processors.
func ReportFanout(appID string, count int) {
	if cb := currentHooks().OnFanout; cb != nil {
		cb(appID, count)
	}
}

// ReportOfflineStart notifies observers that offline mode has started.
func ReportOfflineStart(appID string, at time.Time) {
	if cb := currentHooks().OnOfflineStart; cb != nil {
		cb(appID, at)
	}
}

// ReportOfflineStop notifies observers that offline mode has ended.
func ReportOfflineStop(appID string, startedAt time.Time, duration time.Duration) {
	if cb := currentHooks().OnOfflineStop; cb != nil {
		cb(appID, startedAt, duration)
	}
}

// ReportLocalShadowDirty reports whether the local shadow cursor is dirty.
func ReportLocalShadowDirty(appID string, dirty bool) {
	if cb := currentHooks().OnLocalShadowDirty; cb != nil {
		cb(appID, dirty)
	}
}
