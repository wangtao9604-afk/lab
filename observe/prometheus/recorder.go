package prom

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// ===== Kafka consumption metrics =====
	RecorderConsumeBatchSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "consume_batch_seconds",
			Help:      "End-to-end latency to process one Kafka record (one round of QAs).",
			Buckets:   []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10},
		},
		[]string{"topic"},
	)

	RecorderBatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "batches_total",
			Help:      "Kafka records processed by result.",
		},
		[]string{"result"}, // ok|decode_error|db_error
	)

	RecorderQAsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "qas_total",
			Help:      "Total QA pairs parsed from Kafka records.",
		},
	)

	// ===== DB metrics =====
	RecorderDBInsertBatchSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_insert_total_seconds",
			Help:      "Total latency of DB batch insert including retries.",
			Buckets:   []float64{0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
		},
	)

	RecorderDBInsertBatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_insert_batches_total",
			Help:      "DB insert batches by result.",
		},
		[]string{"result"}, // ok|error|retry_ok
	)

	RecorderDBInsertRowsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_insert_rows_total",
			Help:      "Rows attempted outcome.",
		},
		[]string{"result"}, // inserted|ignored|error
	)

	RecorderDBRetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_retry_total",
			Help:      "DB retry attempts by reason.",
		},
		[]string{"reason"}, // timeout|deadlock|other
	)

	// ===== DB connection pool metrics =====
	RecorderDBPoolOpen = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_pool_open",
			Help:      "sql.DB OpenConnections.",
		},
	)

	RecorderDBPoolInUse = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_pool_in_use",
			Help:      "sql.DB InUse.",
		},
	)

	RecorderDBPoolIdle = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_pool_idle",
			Help:      "sql.DB Idle.",
		},
	)

	RecorderDBWaitCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_wait_count_total",
			Help:      "sql.DB cumulative wait count. Use rate() to see waits per second.",
		},
	)

	RecorderDBWaitSeconds = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "recorder",
			Name:      "db_wait_seconds_total",
			Help:      "sql.DB cumulative wait duration in seconds. Use rate() to see wait time per second.",
		},
	)
)

// PoolStats holds DB connection pool statistics
type PoolStats struct {
	Open         int
	InUse        int
	Idle         int
	WaitCount    int64
	WaitDuration time.Duration
}

// RecorderHooks is the concrete Hooks implementation for Prometheus
type RecorderHooks struct {
	onConsumeLatency      func(topic string, latency time.Duration)
	onBatchResult         func(result string)
	onQAsParsed           func(count int)
	onDBInsertLatency     func(latency time.Duration)
	onDBInsertBatchResult func(result string)
	onDBInsertRows        func(result string, count int64)
	onDBRetry             func(reason string)
	onDBPoolStats         func(stats PoolStats)
}

func (h *RecorderHooks) OnConsumeLatency(topic string, latency time.Duration) {
	if h.onConsumeLatency != nil {
		h.onConsumeLatency(topic, latency)
	}
}

func (h *RecorderHooks) OnBatchResult(result string) {
	if h.onBatchResult != nil {
		h.onBatchResult(result)
	}
}

func (h *RecorderHooks) OnQAsParsed(count int) {
	if h.onQAsParsed != nil {
		h.onQAsParsed(count)
	}
}

func (h *RecorderHooks) OnDBInsertLatency(latency time.Duration) {
	if h.onDBInsertLatency != nil {
		h.onDBInsertLatency(latency)
	}
}

func (h *RecorderHooks) OnDBInsertBatchResult(result string) {
	if h.onDBInsertBatchResult != nil {
		h.onDBInsertBatchResult(result)
	}
}

func (h *RecorderHooks) OnDBInsertRows(result string, count int64) {
	if h.onDBInsertRows != nil {
		h.onDBInsertRows(result, count)
	}
}

func (h *RecorderHooks) OnDBRetry(reason string) {
	if h.onDBRetry != nil {
		h.onDBRetry(reason)
	}
}

func (h *RecorderHooks) OnDBPoolStats(stats PoolStats) {
	if h.onDBPoolStats != nil {
		h.onDBPoolStats(stats)
	}
}

// InstallRecorderHooks creates and returns Hooks implementation for Prometheus.
// Returns interface{} to avoid circular dependency - main will use reflection-free installation.
func InstallRecorderHooks() interface{} {
	var lastWaitCount atomic.Int64
	var lastWaitDuration atomic.Int64 // Store as nanoseconds

	return &RecorderHooks{
		onConsumeLatency: func(topic string, latency time.Duration) {
			RecorderConsumeBatchSeconds.WithLabelValues(topic).Observe(latency.Seconds())
		},
		onBatchResult: func(result string) {
			RecorderBatchesTotal.WithLabelValues(result).Inc()
		},
		onQAsParsed: func(count int) {
			RecorderQAsTotal.Add(float64(count))
		},
		onDBInsertLatency: func(latency time.Duration) {
			RecorderDBInsertBatchSeconds.Observe(latency.Seconds())
		},
		onDBInsertBatchResult: func(result string) {
			RecorderDBInsertBatchesTotal.WithLabelValues(result).Inc()
		},
		onDBInsertRows: func(result string, count int64) {
			RecorderDBInsertRowsTotal.WithLabelValues(result).Add(float64(count))
		},
		onDBRetry: func(reason string) {
			RecorderDBRetryTotal.WithLabelValues(reason).Inc()
		},
		onDBPoolStats: func(stats PoolStats) {
			RecorderDBPoolOpen.Set(float64(stats.Open))
			RecorderDBPoolInUse.Set(float64(stats.InUse))
			RecorderDBPoolIdle.Set(float64(stats.Idle))

			// For counters, compute delta from last reading
			prevCount := lastWaitCount.Swap(stats.WaitCount)
			if prevCount > 0 && stats.WaitCount > prevCount {
				RecorderDBWaitCount.Add(float64(stats.WaitCount - prevCount))
			}

			durationNanos := stats.WaitDuration.Nanoseconds()
			prevDuration := lastWaitDuration.Swap(durationNanos)
			if prevDuration > 0 && durationNanos > prevDuration {
				deltaSeconds := float64(durationNanos-prevDuration) / 1e9
				RecorderDBWaitSeconds.Add(deltaSeconds)
			}
		},
	}
}
