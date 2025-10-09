package prom

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 压测相关 Prometheus 指标

var (
	// StressSequenceViolations 消息顺序违规计数器
	// 不使用 user_id/expected_seq/actual_seq 作为 labels 以避免高基数
	// 具体违规详情通过日志记录
	StressSequenceViolations = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "stress",
			Name:      "sequence_violations_total",
			Help:      "Total number of message sequence violations detected in stress test mode (details in logs)",
		},
	)

	// StressMessagesProcessed 压测消息处理计数器
	// Labels:
	//   - result: 处理结果 (ok, sequence_error, invalid_format)
	// 移除 user_id label 以避免高基数（1000+ 时序）
	StressMessagesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "qywx",
			Subsystem: "stress",
			Name:      "messages_processed_total",
			Help:      "Total number of stress test messages processed",
		},
		[]string{"result"},
	)

	// StressProcessingDuration 压测消息处理延迟（包括 3 秒 sleep）
	// 移除 user_id label 以避免高基数
	StressProcessingDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "qywx",
			Subsystem: "stress",
			Name:      "processing_duration_seconds",
			Help:      "Duration of stress test message processing (including 3s sleep)",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 3.0, 3.5, 4.0, 5.0, 10.0},
		},
	)
)
