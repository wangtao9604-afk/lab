package prom

import "github.com/prometheus/client_golang/prometheus"

var (
	KmqGateBacklog = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "qywx",
			Subsystem: "kmq",
			Name:      "gate_backlog",
			Help:      "Per-partition commit gate backlog.",
		},
		[]string{"topic", "partition"},
	)

	KmqBatchCommitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kmq",
			Name:      "batch_commit_total",
			Help:      "Successful batch commits per topic.",
		},
		[]string{"topic"},
	)

	KmqBatchCommitFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kmq",
			Name:      "batch_commit_failed_total",
			Help:      "Failed batch commit attempts per topic.",
		},
		[]string{"topic"},
	)

	KmqRebalanceTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kmq",
			Name:      "rebalance_total",
			Help:      "Kafka consumer rebalance events.",
		},
		[]string{"group", "type"},
	)

	KmqDLQMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "kmq",
			Name:      "dlq_messages_total",
			Help:      "Number of messages routed to DLQ by component.",
		},
		[]string{"topic", "component"},
	)
)
