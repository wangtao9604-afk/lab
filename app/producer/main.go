package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "os/signal"
    "syscall"
    "time"

    "github.com/confluentinc/confluent-kafka-go/v2/kafka"
    "github.com/gin-gonic/gin"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "qywx/controllers"
    "qywx/infrastructures/config"
    "qywx/infrastructures/log"
    "qywx/infrastructures/mq/kmq"
    "qywx/infrastructures/stress"
    "qywx/infrastructures/wxmsg/kefu"
    "qywx/models/kf"
    prom "qywx/observe/prometheus"
)

var (
    producerHealthGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Namespace: "qywx",
        Subsystem: "producer",
        Name:      "health_status",
        Help:      "Health status of the producer service (1=healthy).",
    })
)

func main() {
    log.InitLogFileBySvrName("producer")
    cfg := config.GetInstance()

    prom.MustRegisterAll()
    producerHealthGauge.Set(1)

    router := gin.New()
    router.Use(gin.Recovery())
    router.GET("/metrics", gin.WrapH(promhttp.Handler()))
    router.GET("/health", func(c *gin.Context) {
        producerHealthGauge.Set(1)
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })

    kfService := kf.GetInstance()
    if err := kfService.Start(); err != nil {
        panic(fmt.Sprintf("failed to start KF service: %v", err))
    }
    defer func() {
        if err := kfService.Stop(); err != nil {
            log.GetInstance().Sugar.Warnf("stop KF service: %v", err)
        }
    }()

    brokers := cfg.Kafka.Brokers
    clientID := cfg.Kafka.Producer.Chat.ClientID
    topic := cfg.Kafka.Topics.CallbackInbound.Name
    if topic == "" {
        topic = kf.RawKafkaTopic
    }

    producer, err := kmq.NewProducer(brokers, clientID)
    if err != nil {
        panic(err)
    }
    defer func() {
        remaining := producer.Flush(5000)
        if remaining > 0 {
            log.GetInstance().Sugar.Warnf("producer flush left %d messages", remaining)
        }
        producer.Close()
    }()

    sink := &kafkaCallbackSink{producer: producer, topic: topic}
    controllers.SetCallbackSink(sink)

    file := router.Group("")
    {
        file.StaticFile("/WW_verify_T3PqKgtN54D3dFtw.txt", "./resources/WW_verify_T3PqKgtN54D3dFtw.txt")
    }

    login := router.Group("/login")
    {
        login.GET("", controllers.MobileShowLogin)
    }

    message := router.Group("/message")
    {
        message.GET("", controllers.VerifyUrl)
        message.POST("", controllers.RecvSuiteMessage)
    }

    // 压测端点 - 仅在压测模式下注册
    if cfg.Stress {
        log.GetInstance().Sugar.Warn("Stress test mode enabled, /stress endpoint registered")
        router.POST("/stress", func(c *gin.Context) {
            handleStressRequest(c, kfService)
        })
        router.POST("/stress/reset", func(c *gin.Context) {
            handleStressReset(c)
        })
    }

    httpAddr := cfg.Services.Producer.HTTPAddr
    if httpAddr == "" {
        httpAddr = ":11112"
    }

    srv := &http.Server{ // #nosec G114 - listen address from config
        Addr:    httpAddr,
        Handler: router,
    }

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            log.GetInstance().Sugar.Fatalf("HTTP server exited: %v", err)
        }
    }()

    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    producerHealthGauge.Set(0)
    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.GetInstance().Sugar.Errorf("shutdown http server: %v", err)
    }
}

// handleStressRequest 处理压测请求
func handleStressRequest(c *gin.Context, kfService *kf.KFService) {
    // 构造压测用的 KFCallbackMessage
    event := stress.BuildStressKFCallback()

    // 使用当前时间作为 epoch
    epoch := time.Now().Unix()

    // 调用 KFService 处理回调
    if err := kfService.HandleCallback(c.Request.Context(), event, epoch); err != nil {
        log.GetInstance().Sugar.Errorf("handle stress callback failed: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleStressReset 重置压测状态（MsgID 计数器）
func handleStressReset(c *gin.Context) {
    stress.ResetAllMsgIDs()
    log.GetInstance().Sugar.Info("Stress test MsgID counters reset")
    c.JSON(http.StatusOK, gin.H{
        "ok":      true,
        "message": "MsgID counters reset for all users",
    })
}

type kafkaCallbackSink struct {
    producer *kmq.Producer
    topic    string
}

func (s *kafkaCallbackSink) Deliver(ctx context.Context, event *kefu.KFCallbackMessage) error {
    if event == nil {
        return errors.New("nil callback event")
    }

    payload, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("marshal callback: %w", err)
    }

    headers := make([]kafka.Header, 0, 3)
    if event.Event != "" {
        headers = append(headers, kafka.Header{Key: "x-wecom-event", Value: []byte(event.Event)})
    }
    if event.OpenKFID != "" {
        headers = append(headers, kafka.Header{Key: "x-wecom-open-kfid", Value: []byte(event.OpenKFID)})
    }
    if event.Token != "" {
        headers = append(headers, kafka.Header{Key: "x-wecom-token", Value: []byte(event.Token)})
    }

    key := conversationKey(event)

    if err := s.producer.Produce(s.topic, []byte(key), payload, headers); err != nil {
        return fmt.Errorf("produce callback: %w", err)
    }
    return nil
}

func conversationKey(event *kefu.KFCallbackMessage) string {
    switch {
    case event.OpenKFID != "" && event.Token != "":
        return event.OpenKFID + "::" + event.Token
    case event.OpenKFID != "":
        return event.OpenKFID
    case event.Token != "":
        return event.Token
    case event.Event != "":
        return event.Event
    default:
        return "unknown"
    }
}
