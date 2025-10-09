package prom

import (
	"strconv"

	"qywx/infrastructures/mq/kmq"
)

// InstallKmqHooks wires Prometheus metrics with kmq observability callbacks.
func InstallKmqHooks() {
	kmq.WithPartitionHooks(kmq.PartitionHooks{
		OnGateBacklog: func(topic string, partition int32, backlog int64) {
			KmqGateBacklog.WithLabelValues(topic, strconv.Itoa(int(partition))).Set(float64(backlog))
		},
		OnBatchCommit: func(topic string, ok bool) {
			if ok {
				KmqBatchCommitTotal.WithLabelValues(topic).Inc()
			} else {
				KmqBatchCommitFailedTotal.WithLabelValues(topic).Inc()
			}
		},
		OnRebalance: func(group, typ string) {
			KmqRebalanceTotal.WithLabelValues(group, typ).Inc()
		},
		OnDLQ: func(topic, component string) {
			KmqDLQMessagesTotal.WithLabelValues(topic, component).Inc()
		},
	})
}
