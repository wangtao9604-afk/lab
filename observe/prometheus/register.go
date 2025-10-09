package prom

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var regOnce sync.Once

// MustRegisterAll registers all Prometheus collectors exactly once and installs hooks.
func MustRegisterAll() {
	regOnce.Do(func() {
		prometheus.MustRegister(
			// kmq
			KmqGateBacklog,
			KmqBatchCommitTotal,
			KmqBatchCommitFailedTotal,
			KmqRebalanceTotal,
			KmqDLQMessagesTotal,

			// fetcher
			FetcherAcquireTotal,
			FetcherLeader,
			FetcherCASTotal,
			FetcherLeaseRenewTotal,
			FetcherPullBatchSeconds,
			FetcherPullBatchSize,
			FetcherFanoutMessagesTotal,
			FetcherOfflineMode,
			FetcherOfflineDuration,
			FetcherLocalShadowDirty,

			// kf
			KFMessagesTotal,
			KFHandleSeconds,
			KFErrorsTotal,
			KFInFlightProcessors,
			KFCursorCASTotal,

			// recorder
			RecorderConsumeBatchSeconds,
			RecorderBatchesTotal,
			RecorderQAsTotal,
			RecorderDBInsertBatchSeconds,
			RecorderDBInsertBatchesTotal,
			RecorderDBInsertRowsTotal,
			RecorderDBRetryTotal,
			RecorderDBPoolOpen,
			RecorderDBPoolInUse,
			RecorderDBPoolIdle,
			RecorderDBWaitCount,
			RecorderDBWaitSeconds,
		)

		InstallKmqHooks()
		InstallFetcherHooks()
		InstallKFHooks()
	})
}
