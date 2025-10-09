package kf

import (
	"sync/atomic"
	"time"
)

// Hooks defines optional callbacks for KF observability.
type Hooks struct {
	OnMessage           func(appID, msgType, source string)
	OnHandleLatency     func(appID, msgType string, latency time.Duration)
	OnError             func(appID, stage string)
	OnProcessorInflight func(appID string, delta int)
	OnCursorCAS         func(appID, result string)
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
	return injectedHooks.Load().(Hooks)
}

// ReportMessage records a processed message with its type and source.
func ReportMessage(appID, msgType, source string) {
	if cb := currentHooks().OnMessage; cb != nil {
		cb(appID, msgType, source)
	}
}

// ReportHandleLatency observes the latency for handling a message type.
func ReportHandleLatency(appID, msgType string, latency time.Duration) {
	if cb := currentHooks().OnHandleLatency; cb != nil {
		cb(appID, msgType, latency)
	}
}

// ReportError increments error counters by stage.
func ReportError(appID, stage string) {
	if cb := currentHooks().OnError; cb != nil {
		cb(appID, stage)
	}
}

// ReportProcessorInflight adjusts the inflight processor gauge.
func ReportProcessorInflight(appID string, delta int) {
	if cb := currentHooks().OnProcessorInflight; cb != nil {
		cb(appID, delta)
	}
}

// ReportCursorCAS captures cursor CAS outcomes.
func ReportCursorCAS(appID, result string) {
	if cb := currentHooks().OnCursorCAS; cb != nil {
		cb(appID, result)
	}
}
