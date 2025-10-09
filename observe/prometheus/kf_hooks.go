package prom

import (
	"time"

	"qywx/models/kf"
)

// InstallKFHooks wires Prometheus metrics with KF observability callbacks.
func InstallKFHooks() {
	kf.WithHooks(kf.Hooks{
		OnMessage: func(appID, msgType, source string) {
			KFMessagesTotal.WithLabelValues(labelOr(appID, "unknown"), labelOr(msgType, "unknown"), labelOr(source, "unknown")).Inc()
		},
		OnHandleLatency: func(appID, msgType string, latency time.Duration) {
			KFHandleSeconds.WithLabelValues(labelOr(appID, "unknown"), labelOr(msgType, "unknown")).Observe(latency.Seconds())
		},
		OnError: func(appID, stage string) {
			KFErrorsTotal.WithLabelValues(labelOr(appID, "unknown"), labelOr(stage, "unknown")).Inc()
		},
		OnProcessorInflight: func(appID string, delta int) {
			if delta > 0 {
				KFInFlightProcessors.WithLabelValues(labelOr(appID, "unknown")).Add(float64(delta))
			} else if delta < 0 {
				KFInFlightProcessors.WithLabelValues(labelOr(appID, "unknown")).Sub(float64(-delta))
			}
		},
		OnCursorCAS: func(appID, result string) {
			KFCursorCASTotal.WithLabelValues(labelOr(appID, "unknown"), labelOr(result, "unknown")).Inc()
		},
	})
}
