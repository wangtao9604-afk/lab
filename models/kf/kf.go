package kf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"qywx/infrastructures/cache"
	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/fetcher"
	"qywx/infrastructures/log"
	"qywx/infrastructures/stress"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/msgqueue"
)

// KFService 微信客服服务主体
type KFService struct {
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	started          atomic.Bool // 使用atomic避免锁竞争
	mu               sync.Mutex  // 仅用于Start/Stop的互斥
	kafkaProducer    *msgqueue.KafkaProducer
	cursorMgr        *cursorManager
	fetcher          *fetcher.Fetcher
	cursorStore      *fetcher.RedisCursorStore
	localCursorStore *fetcher.FileCursorStore
	cursorState      *cursorRuntime
	shadowAppID      string
	shadowPath       string
	stressMu         sync.Mutex // 压测模式串行执行
}

const RawKafkaTopic = "wx_raw_event"

// cursorManager cursor管理器
type cursorManager struct {
	prefix string // Redis key前缀
}

type cursorRuntime struct {
	epoch         int64
	cursor        string
	remoteVersion int64
	localVersion  int64
	dirty         bool
	offlineSince  time.Time
}

const (
	errorStageCallback        = "callback"
	errorStageFetchToken      = "fetch_token"
	errorStageFetchExternalID = "fetch_external_user"
	errorStageSyncMessages    = "sync_messages"
	errorStageProduceKafka    = "produce_kafka"
	errorStageCursorSave      = "cursor_save"
	errorStageCursorUpdate    = "cursor_update"
	errorStageCursorReload    = "cursor_reload"
	errorStageDecode          = "decode"
	errorStageCommitOffset    = "commit_offset"
	errorStageKafkaInit       = "kafka_init"
	errorStageKafkaSubscribe  = "kafka_subscribe"
	errorStageKafkaFatal      = "kafka_fatal"
)

func (s *KFService) appID() string {
	if s != nil && s.shadowAppID != "" {
		return s.shadowAppID
	}
	suite := config.GetInstance().SuiteConfig
	if suite.KfID != "" {
		return suite.KfID
	}
	return suite.SuiteId
}

func callbackMsgType(event *kefu.KFCallbackMessage) string {
	if event == nil {
		return "unknown"
	}
	if event.Event != "" {
		return event.Event
	}
	if event.MsgType != "" {
		return event.MsgType
	}
	return "unknown"
}

func messageTypeLabel(msg *kefu.KFRecvMessage) string {
	if msg == nil {
		return "unknown"
	}
	t := msg.MsgType
	if t == "event" && msg.Event != nil && msg.Event.EventType != "" {
		return "event:" + msg.Event.EventType
	}
	if t == "" {
		return "unknown"
	}
	return t
}

func messageSourceLabel(msg *kefu.KFRecvMessage) string {
	if msg == nil {
		return "unknown"
	}
	switch msg.Origin {
	case 3:
		return "customer"
	case 4:
		return "system"
	case 5:
		return "servicer"
	default:
		return "unknown"
	}
}

// 单例模式相关变量
var (
	instance *KFService
	once     sync.Once
)

// GetInstance 获取KF服务单例实例
func GetInstance() *KFService {
	once.Do(func() {
		instance = newKFService()
	})
	return instance
}

// newKFService 创建客服服务实例（私有函数，仅供单例模式使用）
func newKFService() *KFService {
	ctx, cancel := context.WithCancel(context.Background())

	return &KFService{
		ctx:       ctx,
		cancel:    cancel,
		cursorMgr: &cursorManager{prefix: "kf_cursor:"},
	}
}

// Start 启动服务
func (s *KFService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.Load() {
		return fmt.Errorf("kf service already started")
	}

	if err := s.initKafkaProducer(); err != nil {
		return fmt.Errorf("init kafka producer failed: %w", err)
	}

	if err := s.startFetcher(); err != nil {
		s.closeKafkaProducer()
		return fmt.Errorf("start fetcher failed: %w", err)
	}

	s.started.Store(true)
	log.GetInstance().Sugar.Info("KF service started")

	return nil
}

// Stop 优雅停止服务
func (s *KFService) Stop() error {
	s.mu.Lock()
	if !s.started.Load() {
		s.mu.Unlock()
		return fmt.Errorf("kf service not started")
	}
	s.started.Store(false)
	s.mu.Unlock()

	// 发送取消信号
	s.cancel()

	// 等待所有goroutine退出
	done := make(chan bool)
	go func() {
		s.wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		log.GetInstance().Sugar.Info("KF service stopped gracefully")
		s.closeFetcher()
		s.closeKafkaProducer()
		return nil
	case <-time.After(10 * time.Second):
		log.GetInstance().Sugar.Warn("KF service stop timeout")
		s.closeFetcher()
		s.closeKafkaProducer()
		return fmt.Errorf("stop timeout")
	}
}

// Receive 接收回调事件（高频调用，使用atomic避免锁）
func (s *KFService) Receive(event *kefu.KFCallbackMessage) error {
	// 使用atomic.Load，无锁操作
	if !s.started.Load() {
		return fmt.Errorf("kf service not started")
	}

	if event == nil {
		return fmt.Errorf("nil callback event")
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.HandleCallback(s.ctx, event, 0); err != nil {
			log.GetInstance().Sugar.Errorf("handle kf callback failed: %v", err)
		}
	}()
	return nil
}

// HandleCallback 直接处理回调事件，可供外部调用（例如 Kafka 消费者）。
func (s *KFService) HandleCallback(ctx context.Context, event *kefu.KFCallbackMessage, epoch int64) error {
	appID := s.appID()
	ReportProcessorInflight(appID, 1)
	var start time.Time
	if epoch == 0 {
		start = time.Now()
	}
	defer func() {
		ReportProcessorInflight(appID, -1)
		if epoch == 0 && !start.IsZero() && event != nil {
			ReportHandleLatency(appID, callbackMsgType(event), time.Since(start))
		}
	}()

	if event == nil {
		ReportError(appID, errorStageCallback)
		return fmt.Errorf("nil callback event")
	}

	if epoch == 0 {
		ReportMessage(appID, callbackMsgType(event), "callback")
	}

	// 检查是否处于压测模式
	if config.GetInstance().Stress {
		return s.handleWithStress(ctx, event, epoch)
	}

	if epoch > 0 && s.cursorStore != nil {
		return s.handleWithCursorStore(ctx, event, epoch)
	}
	return s.handleWithCursorManager(event, s.cursorMgr)
}

func (s *KFService) initKafkaProducer() error {
	cfg := config.GetInstance()
	topic := strings.TrimSpace(cfg.Kafka.Topics.Chat.Name)
	if topic == "" {
		return fmt.Errorf("schedule kafka topic not configured")
	}

	producer, err := msgqueue.NewKafkaProducer(msgqueue.KafkaProducerOptions{
		Brokers:  cfg.Kafka.Brokers,
		Topic:    topic,
		ClientID: cfg.Kafka.Producer.Chat.ClientID,
		DLQTopic: cfg.Kafka.ChatDLQ.Topic,
	})
	if err != nil {
		return fmt.Errorf("create kafka producer: %w", err)
	}

	s.kafkaProducer = producer
	log.GetInstance().Sugar.Infof("KF service Kafka producer ready, topic=%s", topic)
	return nil
}

func (s *KFService) closeKafkaProducer() {
	if s.kafkaProducer != nil {
		s.kafkaProducer.Close()
		s.kafkaProducer = nil
	}
}

func (s *KFService) closeFetcher() {
	if s.fetcher != nil {
		s.fetcher.Close()
		s.fetcher = nil
	}
	s.cursorStore = nil
	s.localCursorStore = nil
	s.cursorState = nil
}

func (s *KFService) startFetcher() error {
	fetcherCfg := config.GetInstance().Fetcher
	suiteCfg := config.GetInstance().SuiteConfig
	s.shadowAppID = suiteCfg.KfID
	if s.shadowAppID == "" {
		s.shadowAppID = suiteCfg.SuiteId
	}
	if !fetcherCfg.Enabled {
		return nil
	}

	redisOpts := fetcher.RedisOptions{
		UseSentinel:      fetcherCfg.Redis.UseSentinel,
		Addr:             fetcherCfg.Redis.Addr,
		Password:         fetcherCfg.Redis.Password,
		DB:               fetcherCfg.Redis.DB,
		SentinelAddrs:    fetcherCfg.Redis.SentinelAddrs,
		SentinelPassword: fetcherCfg.Redis.SentinelPassword,
		MasterName:       fetcherCfg.Redis.MasterName,
	}

	leaseTTL := time.Duration(fetcherCfg.LeaseTTLSeconds) * time.Second
	if leaseTTL <= 0 {
		leaseTTL = 15 * time.Second
	}
	pollInterval := time.Duration(fetcherCfg.PollIntervalMs) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = 800 * time.Millisecond
	}
	shadowPath := strings.TrimSpace(fetcherCfg.LocalShadowPath)
	if shadowPath == "" {
		base := "kf_cursor_shadow.json"
		if suiteCfg.KfID != "" {
			base = fmt.Sprintf("kf_cursor_%s.json", suiteCfg.KfID)
		}
		shadowPath = filepath.Join("var", base)
	}
	if !filepath.IsAbs(shadowPath) {
		if abs, err := filepath.Abs(shadowPath); err == nil {
			shadowPath = abs
		}
	}

	s.shadowPath = shadowPath
	s.localCursorStore = fetcher.NewFileCursorStore(shadowPath)
	log.GetInstance().Sugar.Infof("Fetcher local shadow path: %s", shadowPath)

	instance, err := fetcher.New(fetcher.Options{
		Instance: fmt.Sprintf("pid-%d", os.Getpid()),
		Redis:    redisOpts,
		Cursor: fetcher.CursorStoreOptions{
			KeyPrefix: fetcherCfg.KeyPrefix,
			LeaseTTL:  leaseTTL,
			KfID:      suiteCfg.KfID,
		},
		PollInterval: pollInterval,
	})
	if err != nil {
		return err
	}

	s.fetcher = instance
	s.cursorStore = instance.CursorStore()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.runFetcherLoop(s.ctx, instance); err != nil && !errors.Is(err, context.Canceled) {
			log.GetInstance().Sugar.Errorf("fetcher loop exited: %v", err)
		}
	}()

	return nil
}

func (s *KFService) runFetcherLoop(ctx context.Context, f *fetcher.Fetcher) error {
	return f.Run(ctx, func(leaderCtx context.Context, lease fetcher.LeaderLease) error {
		log.GetInstance().Sugar.Infof("fetcher acquired leadership epoch=%d", lease.Epoch)
		if err := s.setupCursorState(leaderCtx, lease.Epoch); err != nil {
			if errors.Is(err, fetcher.ErrNotLeader) || errors.Is(err, context.Canceled) {
				return err
			}
			log.GetInstance().Sugar.Errorf("initialize cursor state failed: %v", err)
			return err
		}
		defer s.clearCursorState()

		err := s.consumeRawTopic(leaderCtx, lease.Epoch)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.GetInstance().Sugar.Errorf("raw consumer loop ended: %v", err)
		}
		return err
	})
}

func (s *KFService) setupCursorState(ctx context.Context, epoch int64) error {
	if s.cursorStore == nil {
		return fmt.Errorf("cursor store not initialized")
	}

	state := &cursorRuntime{epoch: epoch}
	appID := s.appID()

	var localRec *fetcher.CursorRecord
	if s.localCursorStore != nil {
		rec, err := s.localCursorStore.Load(ctx)
		if err != nil {
			log.GetInstance().Sugar.Warnf("load local cursor shadow failed: %v", err)
		} else {
			localRec = rec
		}
	}

	remoteCursor, remoteVersion, err := s.cursorStore.LoadCursor(ctx)
	if err == nil {
		state.remoteVersion = remoteVersion
		state.cursor = remoteCursor
		state.localVersion = 0
		s.setCursorDirty(state, false)

		if localRec != nil && localRec.Dirty && localRec.Cursor != "" {
			if err := s.fastForwardRemote(ctx, state, localRec); err != nil {
				if errors.Is(err, fetcher.ErrNotLeader) {
					return err
				}
				log.GetInstance().Sugar.Warnf("fast forward remote cursor failed, enter offline mode: %v", err)
				state.cursor = localRec.Cursor
				state.localVersion = localRec.Version
				s.setCursorDirty(state, true)
			}
		} else {
			if state.cursor == "" && localRec != nil && localRec.Cursor != "" {
				state.cursor = localRec.Cursor
			}
		}
	} else {
		if localRec != nil {
			state.cursor = localRec.Cursor
			state.localVersion = localRec.Version
		} else {
			state.cursor = ""
			state.localVersion = 0
		}
		s.setCursorDirty(state, true)
		log.GetInstance().Sugar.Warnf("load remote cursor failed, enter offline mode: %v", err)
		ReportError(appID, errorStageCursorReload)
	}

	s.cursorState = state
	s.persistCursorState(ctx, state)
	return nil
}

func (s *KFService) clearCursorState() {
	if s.cursorState != nil {
		s.setCursorDirty(s.cursorState, false)
	}
	s.cursorState = nil
}

func (s *KFService) setCursorDirty(state *cursorRuntime, dirty bool) {
	if state == nil {
		return
	}
	prevDirty := state.dirty
	if dirty {
		state.dirty = true
		if state.offlineSince.IsZero() {
			state.offlineSince = time.Now()
			fetcher.ReportOfflineStart(s.shadowAppID, state.offlineSince)
		}
	} else {
		state.dirty = false
		started := state.offlineSince
		state.offlineSince = time.Time{}
		if prevDirty {
			if !started.IsZero() {
				fetcher.ReportOfflineStop(s.shadowAppID, started, time.Since(started))
			} else {
				fetcher.ReportOfflineStop(s.shadowAppID, time.Time{}, 0)
			}
		}
	}
	fetcher.ReportLocalShadowDirty(s.shadowAppID, state.dirty)
}

func (s *KFService) fastForwardRemote(ctx context.Context, state *cursorRuntime, rec *fetcher.CursorRecord) error {
	if state == nil || rec == nil {
		return nil
	}

	const maxAttempts = 5
	expectedVersion := state.remoteVersion
	targetCursor := rec.Cursor
	appID := s.appID()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		rc, err := s.cursorStore.UpdateCursorCAS(ctx, state.epoch, expectedVersion, targetCursor)
		if err != nil {
			ReportCursorCAS(appID, "error")
			ReportError(appID, errorStageCursorUpdate)
			return err
		}
		switch rc {
		case 1:
			ReportCursorCAS(appID, "success")
			state.remoteVersion = expectedVersion + 1
			state.cursor = targetCursor
			state.localVersion = 0
			s.setCursorDirty(state, false)
			s.persistCursorState(ctx, state)
			return nil
		case 0:
			ReportCursorCAS(appID, "lost")
			return fetcher.ErrNotLeader
		case -1:
			ReportCursorCAS(appID, "conflict")
			cur, ver, err := s.cursorStore.LoadCursor(ctx)
			if err != nil {
				ReportError(appID, errorStageCursorReload)
				return err
			}
			expectedVersion = ver
			state.remoteVersion = ver
			if cur == targetCursor {
				state.cursor = targetCursor
				state.localVersion = 0
				s.setCursorDirty(state, false)
				s.persistCursorState(ctx, state)
				return nil
			}
			continue
		default:
			ReportCursorCAS(appID, "error")
			return fmt.Errorf("unexpected cas return %d", rc)
		}
	}

	ReportCursorCAS(appID, "error")
	return fmt.Errorf("fast forward attempts exceeded")
}

func (s *KFService) persistCursorState(ctx context.Context, state *cursorRuntime) {
	if s.localCursorStore == nil || state == nil {
		return
	}

	version := state.remoteVersion
	if state.dirty {
		version = state.localVersion
	}
	rec := &fetcher.CursorRecord{
		AppID:   s.shadowAppID,
		Epoch:   state.epoch,
		Version: version,
		Cursor:  state.cursor,
		Dirty:   state.dirty,
	}
	if err := s.localCursorStore.Save(ctx, rec); err != nil {
		log.GetInstance().Sugar.Warnf("save cursor shadow failed: %v", err)
		ReportError(s.appID(), errorStageCursorSave)
	}
}

func (s *KFService) updateCursorState(ctx context.Context, state *cursorRuntime, newCursor string) error {
	if state == nil || newCursor == "" {
		return nil
	}

	const maxConflicts = 5
	attempts := 0
	appID := s.appID()

	for {
		rc, err := s.cursorStore.UpdateCursorCAS(ctx, state.epoch, state.remoteVersion, newCursor)
		if err != nil {
			log.GetInstance().Sugar.Warnf("Update cursor via CAS failed, switch to local shadow: %v", err)
			ReportCursorCAS(appID, "error")
			ReportError(appID, errorStageCursorUpdate)
			state.localVersion++
			state.cursor = newCursor
			s.setCursorDirty(state, true)
			s.persistCursorState(ctx, state)
			return nil
		}

		switch rc {
		case 1:
			ReportCursorCAS(appID, "success")
			state.remoteVersion++
			state.cursor = newCursor
			state.localVersion = 0
			s.setCursorDirty(state, false)
			s.persistCursorState(ctx, state)
			return nil
		case 0:
			ReportCursorCAS(appID, "lost")
			return fetcher.ErrNotLeader
		case -1:
			ReportCursorCAS(appID, "conflict")
			attempts++
			if attempts >= maxConflicts {
				log.GetInstance().Sugar.Warn("Update cursor conflict retries exceeded, rely on local shadow")
				state.localVersion++
				state.cursor = newCursor
				s.setCursorDirty(state, true)
				s.persistCursorState(ctx, state)
				return nil
			}
			remoteCursor, remoteVersion, err := s.cursorStore.LoadCursor(ctx)
			if err != nil {
				log.GetInstance().Sugar.Warnf("Reload cursor after conflict failed, keep local shadow: %v", err)
				ReportError(appID, errorStageCursorReload)
				state.localVersion++
				state.cursor = newCursor
				s.setCursorDirty(state, true)
				s.persistCursorState(ctx, state)
				return nil
			}
			state.remoteVersion = remoteVersion
			if remoteCursor == newCursor {
				state.cursor = newCursor
				state.localVersion = 0
				s.setCursorDirty(state, false)
				s.persistCursorState(ctx, state)
				return nil
			}
			continue
		default:
			ReportCursorCAS(appID, "error")
			return fmt.Errorf("unexpected cas return %d", rc)
		}
	}
}

func (s *KFService) consumeRawTopic(ctx context.Context, epoch int64) error {
	cfg := config.GetInstance()
	if cfg.Kafka.Brokers == "" {
		return fmt.Errorf("kafka brokers not configured")
	}
	appID := s.appID()

	groupID := cfg.Fetcher.KafkaGroupID
	if groupID == "" {
		groupID = cfg.Kafka.Consumer.Chat.GroupID + "-raw"
	}
	clientID := cfg.Fetcher.KafkaClientID
	if clientID == "" {
		clientID = cfg.Kafka.Consumer.Chat.GroupID + "-raw-consumer"
	}

	consumerConf := &kafka.ConfigMap{
		"bootstrap.servers":        cfg.Kafka.Brokers,
		"group.id":                 groupID,
		"enable.auto.commit":       false,
		"enable.auto.offset.store": false,
		"auto.offset.reset":        "earliest",
		"socket.keepalive.enable":  true,
	}
	if clientID != "" {
		(*consumerConf)["client.id"] = clientID
	}

	consumer, err := kafka.NewConsumer(consumerConf)
	if err != nil {
		ReportError(appID, errorStageKafkaInit)
		return fmt.Errorf("create raw consumer failed: %w", err)
	}
	defer consumer.Close()

	if err := consumer.SubscribeTopics([]string{RawKafkaTopic}, nil); err != nil {
		ReportError(appID, errorStageKafkaSubscribe)
		return fmt.Errorf("subscribe raw topic failed: %w", err)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		ev := consumer.Poll(200)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			var event kefu.KFCallbackMessage
			if err := json.Unmarshal(e.Value, &event); err != nil {
				log.GetInstance().Sugar.Errorf("unmarshal raw kf message failed: %v", err)
				ReportError(appID, errorStageDecode)
				if _, err := consumer.CommitMessage(e); err != nil {
					log.GetInstance().Sugar.Warnf("commit raw offset failed: %v", err)
					ReportError(appID, errorStageCommitOffset)
				}
				continue
			}

			if err := s.HandleCallback(ctx, &event, epoch); err != nil {
				if errors.Is(err, fetcher.ErrNotLeader) {
					log.GetInstance().Sugar.Warn("lost leadership while handling raw callback")
					return err
				}
				log.GetInstance().Sugar.Errorf("handle raw callback failed: %v", err)
				return err
			}

			if _, err := consumer.CommitMessage(e); err != nil {
				log.GetInstance().Sugar.Warnf("commit raw offset failed: %v", err)
				ReportError(appID, errorStageCommitOffset)
			}

		case kafka.Error:
			if e.IsFatal() {
				ReportError(appID, errorStageKafkaFatal)
				return fmt.Errorf("kafka fatal error: %w", e)
			}
			log.GetInstance().Sugar.Warnf("Kafka consumer error: %v", e)
		default:
			// 忽略其他事件
		}
	}
}

// doHandleEvent 实际处理事件的核心逻辑（简化版：不再管理deadline）
func (s *KFService) handleWithCursorManager(event *kefu.KFCallbackMessage, cm *cursorManager) error {
	appID := s.appID()
	log.GetInstance().Sugar.Info("Processing KF event: Token=", event.Token, ", OpenKFID=", event.OpenKFID)

	cfg := config.GetInstance()
	platform := common.Platform(cfg.Platform)

	accessToken, err := tokenstore.Instance().FetchCorpToken(
		platform,
		cfg.SuiteConfig.SuiteId,
		cfg.SuiteConfig.SuiteId,
		"",
		cfg.SuiteConfig.Secret,
	)
	if err != nil {
		ReportError(appID, errorStageFetchToken)
		return fmt.Errorf("fetch corp token failed: %w", err)
	}

	cursor, err := cm.getCursor(event.OpenKFID)
	needCursor := false
	nextCursor := ""
	if err != nil {
		log.GetInstance().Sugar.Warn("Failed to get cursor from redis: ", err)
		ReportError(appID, errorStageCursorReload)
		needCursor = true
	} else if cursor == "" {
		log.GetInstance().Sugar.Info("No cursor in redis for weixin kefu: ", event.OpenKFID)
		needCursor = true
	} else {
		log.GetInstance().Sugar.Info("Found cursor in redis for weixin kefu: ", event.OpenKFID)
		nextCursor = cursor
	}

	if needCursor {
		_, newCursor, err := kefu.FetchExternalUserID(event.OpenKFID, event.Token, accessToken)
		if err != nil {
			ReportError(appID, errorStageFetchExternalID)
			return fmt.Errorf("fetch external user id failed: %w", err)
		}
		nextCursor = newCursor
	}
	log.GetInstance().Sugar.Info("Got NextCursor: ", nextCursor)

	req := &kefu.KFSyncMsgRequest{
		Cursor:   nextCursor,
		OpenKFID: event.OpenKFID,
		Limit:    100,
	}

	pullStart := time.Now()
	resp, err := kefu.SyncKFMessages(req, accessToken)
	if err != nil {
		ReportError(appID, errorStageSyncMessages)
		return fmt.Errorf("sync messages failed: %w", err)
	}
	fetcher.ReportPullBatch(s.shadowAppID, time.Since(pullStart), len(resp.MsgList))

	log.GetInstance().Sugar.Info("Synced ", len(resp.MsgList))

	for _, msg := range resp.MsgList {
		msgCopy := msg
		msgType := messageTypeLabel(&msg)
		source := messageSourceLabel(&msg)
		msgStart := time.Now()
		if err := s.kafkaProducer.ProduceKFMessage(&msgCopy); err != nil {
			log.GetInstance().Sugar.Errorf("Failed to produce message to Kafka: %v", err)
			ReportError(appID, errorStageProduceKafka)
		}
		ReportMessage(appID, msgType, source)
		ReportHandleLatency(appID, msgType, time.Since(msgStart))
	}
	fetcher.ReportFanout(s.shadowAppID, len(resp.MsgList))

	if resp.NextCursor != "" {
		if err := cm.setCursor(event.OpenKFID, resp.NextCursor); err != nil {
			log.GetInstance().Sugar.Error("Failed to save cursor: ", err)
			ReportError(appID, errorStageCursorSave)
		}
	}

	return nil
}

// handleWithStress 压测模式下的消息处理
// 不触碰 Cursor 逻辑，直接生成模拟消息并发送到 Kafka
func (s *KFService) handleWithStress(ctx context.Context, event *kefu.KFCallbackMessage, epoch int64) error {
	s.stressMu.Lock()
	defer s.stressMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	log.GetInstance().Sugar.Infof("Processing KF event with stress mode: Token=%s, OpenKFID=%s, epoch=%d", event.Token, event.OpenKFID, epoch)

	appID := s.appID()

	// 模拟消息拉取
	pullStart := time.Now()
	resp := stress.SimulateKFMessages()
	fetcher.ReportPullBatch(s.shadowAppID, time.Since(pullStart), len(resp.MsgList))

	log.GetInstance().Sugar.Infof("Simulated %d messages for OpenKFID=%s", len(resp.MsgList), event.OpenKFID)

	// 逐条发送到 Kafka
	for _, msg := range resp.MsgList {
		msgCopy := msg
		msgType := messageTypeLabel(&msg)
		source := messageSourceLabel(&msg)
		msgStart := time.Now()

		if err := s.kafkaProducer.ProduceKFMessage(&msgCopy); err != nil {
			log.GetInstance().Sugar.Errorf("Failed to produce message to Kafka: %v", err)
			ReportError(appID, errorStageProduceKafka)
		}

		ReportMessage(appID, msgType, source)
		ReportHandleLatency(appID, msgType, time.Since(msgStart))
	}

	fetcher.ReportFanout(s.shadowAppID, len(resp.MsgList))
	return nil
}

func (s *KFService) handleWithCursorStore(ctx context.Context, event *kefu.KFCallbackMessage, epoch int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log.GetInstance().Sugar.Infof("Processing KF event with fetcher: Token=%s, OpenKFID=%s, epoch=%d", event.Token, event.OpenKFID, epoch)
	appID := s.appID()

	state := s.cursorState
	if state == nil {
		return fmt.Errorf("cursor runtime not initialized")
	}
	if state.epoch != epoch {
		log.GetInstance().Sugar.Warnf("epoch mismatch: runtime=%d, got=%d", state.epoch, epoch)
	}

	needCursor := state.cursor == ""
	nextCursor := state.cursor

	cfg := config.GetInstance()
	platform := common.Platform(cfg.Platform)
	accessToken, err := tokenstore.Instance().FetchCorpToken(
		platform,
		cfg.SuiteConfig.SuiteId,
		cfg.SuiteConfig.SuiteId,
		"",
		cfg.SuiteConfig.Secret,
	)
	if err != nil {
		ReportError(appID, errorStageFetchToken)
		return fmt.Errorf("fetch corp token failed: %w", err)
	}

	if needCursor {
		_, newCursor, err := kefu.FetchExternalUserID(event.OpenKFID, event.Token, accessToken)
		if err != nil {
			ReportError(appID, errorStageFetchExternalID)
			return fmt.Errorf("fetch external user id failed: %w", err)
		}
		nextCursor = newCursor
	}

	req := &kefu.KFSyncMsgRequest{
		Cursor:   nextCursor,
		OpenKFID: event.OpenKFID,
		Limit:    100,
	}

	pullStart := time.Now()
	resp, err := kefu.SyncKFMessages(req, accessToken)
	if err != nil {
		ReportError(appID, errorStageSyncMessages)
		return fmt.Errorf("sync messages failed: %w", err)
	}
	fetcher.ReportPullBatch(s.shadowAppID, time.Since(pullStart), len(resp.MsgList))

	log.GetInstance().Sugar.Infof("Synced %d messages for OpenKFID=%s", len(resp.MsgList), event.OpenKFID)

	for _, msg := range resp.MsgList {
		msgCopy := msg
		msgType := messageTypeLabel(&msg)
		source := messageSourceLabel(&msg)
		msgStart := time.Now()
		if err := s.kafkaProducer.ProduceKFMessage(&msgCopy); err != nil {
			log.GetInstance().Sugar.Errorf("Failed to produce message to Kafka: %v", err)
			ReportError(appID, errorStageProduceKafka)
		}
		ReportMessage(appID, msgType, source)
		ReportHandleLatency(appID, msgType, time.Since(msgStart))
	}
	fetcher.ReportFanout(s.shadowAppID, len(resp.MsgList))

	newCursorValue := resp.NextCursor
	if newCursorValue == "" {
		newCursorValue = nextCursor
	}
	if newCursorValue == "" {
		return nil
	}

	if err := s.updateCursorState(ctx, state, newCursorValue); err != nil {
		if errors.Is(err, fetcher.ErrNotLeader) {
			log.GetInstance().Sugar.Warn("Update cursor failed: not leader anymore")
		}
		if !errors.Is(err, fetcher.ErrNotLeader) {
			ReportError(appID, errorStageCursorUpdate)
		}
		return err
	}

	return nil
}

// getCursor 从Redis获取cursor
func (cm *cursorManager) getCursor(kfID string) (string, error) {
	key := cm.getKey(kfID)
	cursor, err := cache.GetInstance().FetchString(key)
	if err != nil {
		if err == cache.ErrKeyNotFound {
			// 键不存在是正常情况，返回空cursor
			return "", nil
		}
		return "", err
	}
	return cursor, nil
}

// setCursor 保存cursor到Redis
func (cm *cursorManager) setCursor(externalUserID, cursor string) error {
	key := cm.getKey(externalUserID)
	// cursor 永久有效（TTL=0），由业务逻辑控制过期
	// 若需要自动过期，可修改为：7*24*time.Hour
	return cache.GetInstance().StoreString(key, cursor, 0)
}

// getKey 生成Redis key
func (cm *cursorManager) getKey(externalUserID string) string {
	return fmt.Sprintf("%s%s", cm.prefix, externalUserID)
}
