package kmq

import "sync/atomic"

// PartitionHooks aggregates optional observability callbacks that can be
// injected from upper layers without introducing concrete monitoring
// dependencies (e.g. Prometheus).
type PartitionHooks struct {
	OnGateBacklog func(topic string, partition int32, backlog int64)
	OnBatchCommit func(topic string, ok bool)
	OnRebalance   func(group, eventType string)
	OnDLQ         func(topic, component string)
}

var injectedHooks atomic.Value

func init() {
	injectedHooks.Store(PartitionHooks{})
}

// WithPartitionHooks installs the given callbacks globally. Passing a zero
// value resets hooks to no-op behaviour.
func WithPartitionHooks(h PartitionHooks) {
	injectedHooks.Store(h)
}

func currentHooks() PartitionHooks {
	v := injectedHooks.Load()
	return v.(PartitionHooks)
}

// ReportGateBacklog notifies observers about backlog changes on commit gate.
func ReportGateBacklog(topic string, partition int32, backlog int64) {
	if cb := currentHooks().OnGateBacklog; cb != nil {
		cb(topic, partition, backlog)
	}
}

// ReportBatchCommit records the result of a batch commit attempt.
func ReportBatchCommit(topic string, ok bool) {
	if cb := currentHooks().OnBatchCommit; cb != nil {
		cb(topic, ok)
	}
}

// ReportRebalance emits group rebalance events (assigned | revoked | lost).
func ReportRebalance(group, eventType string) {
	if cb := currentHooks().OnRebalance; cb != nil {
		cb(group, eventType)
	}
}

// ReportDLQ reports DLQ messages from producer or consumer components.
func ReportDLQ(topic, component string) {
	if cb := currentHooks().OnDLQ; cb != nil {
		cb(topic, component)
	}
}
