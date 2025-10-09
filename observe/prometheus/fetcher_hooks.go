package prom

import (
	"time"

	"qywx/infrastructures/fetcher"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	FetcherAcquireTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "acquire_total",
			Help:      "Number of leadership acquire attempts partitioned by result.",
		},
		[]string{"app", "instance", "result"},
	)

	FetcherLeader = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "leader",
			Help:      "Current leader status per fetcher instance (1=leader,0=not).",
		},
		[]string{"app", "instance"},
	)

	FetcherCASTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "cas_total",
			Help:      "Count of cursor CAS results partitioned by outcome.",
		},
		[]string{"app", "result"},
	)

	FetcherLeaseRenewTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "lease_renew_total",
			Help:      "Lease renew outcomes for fetcher leadership.",
		},
		[]string{"app", "result"},
	)

	FetcherPullBatchSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "pull_batch_seconds",
			Help:      "Latency in seconds of message pull batches per app.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"app"},
	)

	FetcherPullBatchSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "pull_batch_size",
			Help:      "Number of messages fetched per batch.",
			Buckets:   []float64{1, 5, 10, 20, 50, 100, 200, 500},
		},
		[]string{"app"},
	)

	FetcherFanoutMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "fanout_messages_total",
			Help:      "Total messages faned out to downstream processors.",
		},
		[]string{"app"},
	)

	FetcherOfflineMode = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "offline_mode",
			Help:      "Whether fetcher is operating in offline/local shadow mode (1=yes).",
		},
		[]string{"app"},
	)

	FetcherOfflineDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "offline_duration_seconds",
			Help:      "Duration of offline/local shadow periods in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"app"},
	)

	FetcherLocalShadowDirty = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "fetcher",
			Name:      "local_shadow_dirty",
			Help:      "Whether local cursor shadow is ahead of remote state (1=dirty).",
		},
		[]string{"app"},
	)
)

// InstallFetcherHooks wires Prometheus metrics with fetcher observability callbacks.
func InstallFetcherHooks() {
	fetcher.WithHooks(fetcher.Hooks{
		OnAcquireAttempt: func(appID, instance, result string) {
			FetcherAcquireTotal.WithLabelValues(labelOr(appID, labelOr(instance, "unknown")), labelOr(instance, "unknown"), labelOr(result, "unknown")).Inc()
		},
		OnLeaderChanged: func(appID, instance string, leader bool) {
			value := 0.0
			if leader {
				value = 1
			}
			FetcherLeader.WithLabelValues(labelOr(appID, labelOr(instance, "unknown")), labelOr(instance, "unknown")).Set(value)
		},
		OnCASResult: func(appID, result string) {
			FetcherCASTotal.WithLabelValues(labelOr(appID, "unknown"), labelOr(result, "unknown")).Inc()
		},
		OnLeaseRenew: func(appID, result string) {
			FetcherLeaseRenewTotal.WithLabelValues(labelOr(appID, "unknown"), labelOr(result, "unknown")).Inc()
		},
		OnPullBatch: func(appID string, latency time.Duration, size int) {
			app := labelOr(appID, "unknown")
			FetcherPullBatchSeconds.WithLabelValues(app).Observe(latency.Seconds())
			FetcherPullBatchSize.WithLabelValues(app).Observe(float64(size))
		},
		OnFanout: func(appID string, count int) {
			FetcherFanoutMessagesTotal.WithLabelValues(labelOr(appID, "unknown")).Add(float64(count))
		},
		OnOfflineStart: func(appID string, at time.Time) {
			FetcherOfflineMode.WithLabelValues(labelOr(appID, "unknown")).Set(1)
		},
		OnOfflineStop: func(appID string, startedAt time.Time, duration time.Duration) {
			app := labelOr(appID, "unknown")
			FetcherOfflineMode.WithLabelValues(app).Set(0)
			if duration > 0 {
				FetcherOfflineDuration.WithLabelValues(app).Observe(duration.Seconds())
			}
		},
		OnLocalShadowDirty: func(appID string, dirty bool) {
			value := 0.0
			if dirty {
				value = 1
			}
			FetcherLocalShadowDirty.WithLabelValues(labelOr(appID, "unknown")).Set(value)
		},
	})
}

func labelOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
