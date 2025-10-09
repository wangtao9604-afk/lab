package main

import (
	"sync/atomic"
	"time"
)

// Hooks defines optional callbacks for recorder observability.
// Prometheus package injects concrete implementation via InstallRecorderHooks.
type Hooks struct {
	// ===== Kafka consumption metrics =====
	// Latency to process one Kafka record (one round of QAs)
	OnConsumeLatency func(topic string, latency time.Duration)
	// Kafka record processing result: ok|decode_error|db_error|dlq
	OnBatchResult func(result string)
	// Total QA pairs parsed from Kafka records
	OnQAsParsed func(count int)

	// ===== DB metrics =====
	// Latency of one DB batch insert
	OnDBInsertLatency func(latency time.Duration)
	// DB insert batch result: ok|error|retry_ok
	OnDBInsertBatchResult func(result string)
	// DB insert rows result: inserted|ignored|error
	OnDBInsertRows func(result string, count int64)
	// DB retry attempts by reason: timeout|deadlock|other
	OnDBRetry func(reason string)

	// ===== DB pool stats =====
	OnDBPoolStats func(stats DBPoolStats)
}

// DBPoolStats holds sql.DB.Stats() snapshot
type DBPoolStats struct {
	Open         int
	InUse        int
	Idle         int
	WaitCount    int64
	WaitDuration time.Duration
}

var globalHooks atomic.Value // stores *Hooks

// InstallHooks sets the global hooks instance (called by prometheus package)
func InstallHooks(h *Hooks) {
	if h != nil {
		globalHooks.Store(h)
	}
}

// getHooks returns the current hooks or nil
func getHooks() *Hooks {
	v := globalHooks.Load()
	if v == nil {
		return nil
	}
	return v.(*Hooks)
}

// ReportConsumeLatency reports Kafka message processing latency
func ReportConsumeLatency(topic string, latency time.Duration) {
	if h := getHooks(); h != nil && h.OnConsumeLatency != nil {
		h.OnConsumeLatency(topic, latency)
	}
}

// ReportBatchResult reports Kafka batch processing result
func ReportBatchResult(result string) {
	if h := getHooks(); h != nil && h.OnBatchResult != nil {
		h.OnBatchResult(result)
	}
}

// ReportQAsParsed reports number of QA pairs parsed
func ReportQAsParsed(count int) {
	if h := getHooks(); h != nil && h.OnQAsParsed != nil {
		h.OnQAsParsed(count)
	}
}

// ReportDBInsertLatency reports DB insert latency
func ReportDBInsertLatency(latency time.Duration) {
	if h := getHooks(); h != nil && h.OnDBInsertLatency != nil {
		h.OnDBInsertLatency(latency)
	}
}

// ReportDBInsertBatchResult reports DB insert batch result
func ReportDBInsertBatchResult(result string) {
	if h := getHooks(); h != nil && h.OnDBInsertBatchResult != nil {
		h.OnDBInsertBatchResult(result)
	}
}

// ReportDBInsertRows reports DB insert row counts by result
func ReportDBInsertRows(result string, count int64) {
	if h := getHooks(); h != nil && h.OnDBInsertRows != nil {
		h.OnDBInsertRows(result, count)
	}
}

// ReportDBRetry reports DB retry attempts
func ReportDBRetry(reason string) {
	if h := getHooks(); h != nil && h.OnDBRetry != nil {
		h.OnDBRetry(reason)
	}
}

// ReportDBPoolStats reports DB connection pool stats
func ReportDBPoolStats(stats DBPoolStats) {
	if h := getHooks(); h != nil && h.OnDBPoolStats != nil {
		h.OnDBPoolStats(stats)
	}
}
