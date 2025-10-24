package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"qywx/infrastructures/config"
	"qywx/infrastructures/db/mysql"
	"qywx/infrastructures/log"
	"qywx/infrastructures/mq/kmq"
	"qywx/models/recorder"
	prom "qywx/observe/prometheus"
)

var recorderHealthGauge = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "qywx",
	Subsystem: "recorder",
	Name:      "health_status",
	Help:      "Health status of the recorder service (1=healthy).",
})

func main() {
	log.InitLogFileBySvrName("recorder")
	cfg := config.GetInstance()
	logger := log.GetInstance().Sugar

	logger.Infof("Recorder service starting, PID=%d", os.Getpid())

	prom.MustRegisterAll()
	recorderHealthGauge.Set(1)

	// Install recorder metrics hooks
	// Use type assertion with interface matching
	type recorderHooks interface {
		OnConsumeLatency(topic string, latency time.Duration)
		OnBatchResult(result string)
		OnQAsParsed(count int)
		OnDBInsertLatency(latency time.Duration)
		OnDBInsertBatchResult(result string)
		OnDBInsertRows(result string, count int64)
		OnDBRetry(reason string)
		OnDBPoolStats(stats prom.PoolStats)
	}

	promHooks := prom.InstallRecorderHooks().(recorderHooks)
	InstallHooks(&Hooks{
		OnConsumeLatency:      promHooks.OnConsumeLatency,
		OnBatchResult:         promHooks.OnBatchResult,
		OnQAsParsed:           promHooks.OnQAsParsed,
		OnDBInsertLatency:     promHooks.OnDBInsertLatency,
		OnDBInsertBatchResult: promHooks.OnDBInsertBatchResult,
		OnDBInsertRows:        promHooks.OnDBInsertRows,
		OnDBRetry:             promHooks.OnDBRetry,
		OnDBPoolStats: func(stats DBPoolStats) {
			promHooks.OnDBPoolStats(prom.PoolStats{
				Open:         stats.Open,
				InUse:        stats.InUse,
				Idle:         stats.Idle,
				WaitCount:    stats.WaitCount,
				WaitDuration: stats.WaitDuration,
			})
		},
	})

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	httpAddr := cfg.Services.Recorder.HTTPAddr
	if httpAddr == "" {
		httpAddr = ":11114"
	}

	srv := &http.Server{
		Addr:    httpAddr,
		Handler: router,
	}

	db, err := mysql.Open()
	if err != nil {
		logger.Fatalf("failed to open mysql: %v", err)
	}
	logger.Info("MySQL connected")

	// Get sql.DB for pool stats monitoring
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatalf("failed to get sql.DB: %v", err)
	}

	repo := recorder.NewRepo(db)

	dlq, err := kmq.NewDLQ(
		cfg.Kafka.Brokers,
		"qywx-recorder",
		cfg.Kafka.RecorderDLQ.Topic,
	)
	if err != nil {
		logger.Warnf("failed to init DLQ: %v", err)
	}

	consumer, err := kmq.NewConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.Consumer.Recorder.GroupID,
		kmq.WithMaxInflightPerPartition(128),
		kmq.WithBatchCommit(2*time.Second, 100),
		kmq.WithLifecycleHooks(kmq.LifecycleHooks{
			OnAssigned: func(topic string, partition int32, startOffset int64) {
				logger.Infof("partition assigned: topic=%s partition=%d offset=%d", topic, partition, startOffset)
			},
			OnRevoked: func(topic string, partition int32) {
				logger.Infof("OnRevoked: partition revoked: topic=%s partition=%d", topic, partition)
			},
			OnLost: func(topic string, partition int32) {
				logger.Infof("OnLost: partition revoked: topic=%s partition=%d", topic, partition)
			},
		}),
	)
	if err != nil {
		logger.Fatalf("failed to create consumer: %v", err)
	}

	topic := cfg.Kafka.Topics.Recorder.Name
	if err := consumer.Subscribe([]string{topic}); err != nil {
		logger.Fatalf("failed to subscribe to %s: %v", topic, err)
	}
	logger.Infof("Subscribed to topic: %s", topic)

	handler := NewHandler(repo, dlq)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start DB pool stats reporter
	stopPoolStats := StartDBPoolStatsReporter(ctx, sqlDB, 10*time.Second)
	defer stopPoolStats()

	var wg sync.WaitGroup

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("HTTP server exited: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("Recorder service started, polling messages...")
		consumer.PollAndDispatchWithAck(ctx, handler.Handle, 100)
		logger.Info("Consumer polling loop exited")
	}()

	<-ctx.Done()

	logger.Info("Shutdown signal received, stopping consumer...")
	recorderHealthGauge.Set(0)

	consumer.Close()
	wg.Wait()
	logger.Info("Consumer shutdown complete")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("shutdown http server: %v", err)
	}
}
