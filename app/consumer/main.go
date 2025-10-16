package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
	"qywx/models/schedule"
	prom "qywx/observe/prometheus"
)

var consumerHealthGauge = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "qywx",
	Subsystem: "consumer",
	Name:      "health_status",
	Help:      "Health status of the consumer service (1=healthy).",
})

func main() {
	log.InitLogFileBySvrName("consumer")
	cfg := config.GetInstance()

	prom.MustRegisterAll()
	consumerHealthGauge.Set(1)

	scheduler := schedule.GetInstance()

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	router.GET("/health", func(c *gin.Context) {
		consumerHealthGauge.Set(1)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	httpAddr := cfg.Services.Consumer.HTTPAddr
	if httpAddr == "" {
		httpAddr = ":11113"
	}

	srv := &http.Server{ // #nosec G114 - listen address来自配置
		Addr:    httpAddr,
		Handler: router,
	}

	if err := scheduler.Start(); err != nil {
		panic(fmt.Sprintf("failed to start scheduler: %v", err))
	}
	if err := scheduler.StartKafkaRuntime(); err != nil {
		panic(fmt.Sprintf("failed to start scheduler Kafka runtime: %v", err))
	}
	defer func() {
		if err := scheduler.Stop(); err != nil {
			log.GetInstance().Sugar.Warnf("stop scheduler: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.GetInstance().Sugar.Fatalf("HTTP server exited: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	consumerHealthGauge.Set(0)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.GetInstance().Sugar.Errorf("shutdown http server: %v", err)
	}
}

// handleStopRequest 处理停止调度器的请求（用于压测场景）
func handleStopRequest(c *gin.Context, scheduler *schedule.Scheduler) {
	log.GetInstance().Sugar.Info("Received stop request, stopping scheduler...")

	if err := scheduler.Stop(); err != nil {
		log.GetInstance().Sugar.Errorf("Failed to stop scheduler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	log.GetInstance().Sugar.Info("Scheduler stopped successfully")
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Scheduler stopped successfully",
	})
}
