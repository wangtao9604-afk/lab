package prom

import "github.com/prometheus/client_golang/prometheus"

var (
	KFMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kf",
			Name:      "messages_total",
			Help:      "Total KF messages processed partitioned by message type and source.",
		},
		[]string{"app", "msg_type", "source"},
	)

	KFHandleSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "kf",
			Name:      "handle_seconds",
			Help:      "Latency in seconds to handle KF messages per type.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"app", "msg_type"},
	)

	KFErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kf",
			Name:      "errors_total",
			Help:      "Count of KF processing errors partitioned by stage.",
		},
		[]string{"app", "stage"},
	)

	KFInFlightProcessors = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "kf",
			Name:      "inflight_processors",
			Help:      "Number of KF callback processors currently running.",
		},
		[]string{"app"},
	)

	KFCursorCASTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kf",
			Name:      "cursor_cas_total",
			Help:      "Cursor CAS outcomes within KF processing.",
		},
		[]string{"app", "result"},
	)
)
