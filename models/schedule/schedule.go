package schedule

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/message"
	"qywx/models/msgqueue"
	"qywx/models/thirdpart/gaode"
	"qywx/models/thirdpart/minimax"
)

// 位置键（城市+区域）
type LocationKey struct {
	City     string
	District string
}

// 待确认的位置（含板块）
type PendingLoc struct {
	City     string
	District string
	Plate    string
}

// 单例实例
var (
	instance *Scheduler
	once     sync.Once
)

// FurtherProcessTask 后续处理任务
type FurtherProcessTask struct {
	UserID              string                 // 用户ID (external_user_id)
	OpenKFID            string                 // 客服账号ID
	ConversationHistory []minimax.Message      // 对话历史副本
	RawConverHistory    []minimax.RawMsg       // 纯粹的对话历史，用于构建Ipang API的Qas参数
	Keywords            map[string]interface{} // 累积的关键词（双引擎合并结果）
	Anchor              map[string]interface{} // 当前锚点信息（若存在）
}

// Scheduler 消息调度器
type Scheduler struct {
	ctx              context.Context
	cancel           context.CancelFunc
	messageChan      <-chan *kefu.KFRecvMessage // 从KF模块接收消息的channel
	furtherProcess   chan *FurtherProcessTask   // 后续处理channel（Ipang API调用等）
	processors       map[string]*Processor      // external_user_id -> Processor映射
	processorsMu     sync.RWMutex               // 保护processors map
	wg               sync.WaitGroup             // 等待所有协程退出
	lifetime         time.Duration              // Processor生命周期
	started          bool                       // 是否已启动
	mu               sync.Mutex                 // 保护started状态
	mqRuntime        *msgqueue.KafkaRuntime
	recorderProducer RecorderProducer // Recorder Kafka Producer
	recorderTopic    string           // Recorder Topic名称
}

// Processor 用户消息处理器
type Processor struct {
	userID      string                   // external_user_id
	messageChan chan *message.InboundMsg // 该用户的消息channel
	lifetime    time.Duration            // 生命周期
	resetTimer  chan struct{}            // 重置计时器信号
	ctx         context.Context          // processor的context
	cancel      context.CancelFunc       // 取消函数
	scheduler   *Scheduler               // 父调度器引用

	// MiniMax集成相关字段
	minimaxClient       *minimax.Client   // MiniMax API客户端
	conversationHistory []minimax.Message // 对话历史
	rawConverHistory    []minimax.RawMsg  // 纯粹的对话历史，需要返给IPang API
	historyMu           sync.RWMutex      // 保护对话历史的读写锁

	// 关键字提取服务
	keywordExtractor *KeywordExtractorService // 关键字提取服务

	// 客服相关信息
	currentOpenKFID string // 当前对话的客服账号ID

	// 压测相关
	seqChecker       *SequenceChecker // 消息顺序检测器（压测模式）
	receivedMsgIDs   []string         // 接收到的消息ID列表（用于压测观察）
	receivedMsgIDsMu sync.Mutex       // 保护receivedMsgIDs的锁

	// 并发关键词提取架构
	keywordsChan         chan string            // 用户消息队列（缓冲100）
	keywordsMap          map[string]interface{} // 累积的关键词（10维度）
	keywordsMu           sync.RWMutex           // 保护keywordsMap
	keywordsWorkerCtx    context.Context        // worker上下文
	keywordsWorkerCancel context.CancelFunc     // worker取消函数

	// 多位置分片结构
	keywordsByLoc     map[string]map[string]map[string]interface{} // City -> District -> Record
	finalLoc          LocationKey                                  // 当前活跃位置（City 统一为“上海市”）
	currentKeywords   map[string]interface{}                       // 指向finalLoc下的记录（别名到keywordsMap）
	pendingLoc        *PendingLoc                                  // 待确认的位置
	stagedNonLocation map[string]interface{}                       // 待确认阶段暂存的非location维度
	// 待确认阶段是否重置非location维度（仅当用户明确要求时为true）
	pendingResetNonLocation bool

	// “思考中”占位与轮询控制
	thinkWaiterMu      sync.Mutex
	thinkWaiterRunning bool
	lastPlaceholderAt  time.Time
	lastPlaceholderSeq uint64
	thinkWaiterCtx     context.Context
	thinkWaiterCancel  context.CancelFunc
	turnSeq            uint64

	// 对于同一次pending，仅拦截一次end
	pendingEndIntercepted bool

	// 微地标兜底拦截只触发一次
	microLocAskIntercepted bool

	// 购房意向：新房/二手房/All（默认All）
	purchaseIntent   PurchaseIntent
	purchaseIntentMu sync.RWMutex

	// 服务端自动结束的“一次性”原子位（每条用户文本开头重置）
	autoEndOnce int32
}

// GetInstance 获取调度器单例
func GetInstance() *Scheduler {
	once.Do(func() {
		instance = newScheduler()
	})
	return instance
}

// newScheduler 创建调度器（私有）
func newScheduler() *Scheduler {
	cfg := config.GetInstance()

	// 从配置读取processor生命周期，默认30分钟
	lifetime := time.Duration(cfg.ScheduleConfig.ProcessorLifetimeMinutes) * time.Minute
	if lifetime == 0 {
		lifetime = 30 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		ctx:            ctx,
		cancel:         cancel,
		processors:     make(map[string]*Processor),
		furtherProcess: make(chan *FurtherProcessTask, 100), // 缓冲channel，避免阻塞
		lifetime:       lifetime,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("scheduler already started")
	}

	// 启动单个后续处理worker（保证顺序处理）
	s.wg.Add(1)
	go s.furtherTaskWorker()

	s.started = true
	log.GetInstance().Sugar.Info("Scheduler started with processor lifetime: ", s.lifetime)

	return nil
}

// StartKafkaRuntime 根据配置启动 Kafka runtime，消息通过 DispatchInbound 分发。
func (s *Scheduler) StartKafkaRuntime() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return fmt.Errorf("scheduler not started")
	}
	if s.mqRuntime != nil {
		s.mu.Unlock()
		return fmt.Errorf("kafka runtime already started")
	}
	s.mu.Unlock()

	cfg := config.GetInstance()
	topic := strings.TrimSpace(cfg.Kafka.Topics.Chat.Name)
	if topic == "" {
		return fmt.Errorf("schedule kafka topic not configured")
	}
	groupID := cfg.Kafka.Consumer.Chat.GroupID
	if groupID == "" {
		return fmt.Errorf("kafka consumer group id not configured")
	}

	// 初始化 Recorder Producer
	recorderProducer, err := msgqueue.NewKafkaProducer(msgqueue.KafkaProducerOptions{
		Brokers:  cfg.Kafka.Brokers,
		ClientID: "qywx-producer-recorder",
		Topic:    cfg.Kafka.Topics.Recorder.Name,
	})
	if err != nil {
		return fmt.Errorf("create recorder producer: %w", err)
	}
	s.recorderProducer = recorderProducer
	s.recorderTopic = cfg.Kafka.Topics.Recorder.Name

	runtime, err := msgqueue.NewKafkaRuntime(
		cfg.Kafka.Brokers,
		groupID,
		cfg.Kafka.ChatDLQ.Topic,
		s.DispatchInbound,
	)
	if err != nil {
		return fmt.Errorf("start kafka runtime: %w", err)
	}

	topics := splitTopics(topic)
	if err := runtime.Start(s.ctx, topics); err != nil {
		runtime.Close()
		return fmt.Errorf("subscribe kafka topics: %w", err)
	}

	s.mu.Lock()
	s.mqRuntime = runtime
	s.mu.Unlock()

	log.GetInstance().Sugar.Infof("Kafka runtime started, topics=%v", topics)
	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return fmt.Errorf("scheduler not started")
	}
	s.started = false
	s.mu.Unlock()

	// 发送取消信号 (必须在 stopKafkaRuntime 之前，让 PollAndDispatchWithAck 能检测到 context 取消)
	s.cancel()

	// 停止 Kafka runtime (Close 内部会等待 poll loop 退出)
	s.stopKafkaRuntime()

	// 等待所有协程退出（最多10秒）
	done := make(chan bool)
	go func() {
		s.wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		log.GetInstance().Sugar.Info("Scheduler stopped gracefully")
		return nil
	case <-time.After(10 * time.Second):
		log.GetInstance().Sugar.Warn("Scheduler stop timeout")
		return fmt.Errorf("stop timeout")
	}
}

func (s *Scheduler) stopKafkaRuntime() {
	s.mu.Lock()
	runtime := s.mqRuntime
	s.mqRuntime = nil
	s.mu.Unlock()

	if runtime != nil {
		runtime.Close()
		log.GetInstance().Sugar.Info("Kafka runtime stopped")
	}
}

func splitTopics(spec string) []string {
	parts := strings.Split(spec, ",")
	topics := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			topics = append(topics, trimmed)
		}
	}
	return topics
}

// dispatcher 消息分发协程
func (s *Scheduler) dispatcher() {
	defer s.wg.Done()

	log.GetInstance().Sugar.Info("Dispatcher started")
	defer log.GetInstance().Sugar.Info("Dispatcher stopped")

	for {
		select {
		case <-s.ctx.Done():
			// 停止所有processor
			s.stopAllProcessors()
			return

		case msg, ok := <-s.messageChan:
			if !ok {
				log.GetInstance().Sugar.Info("Message channel closed")
				s.stopAllProcessors()
				return
			}

			// 通过 DispatchInbound 统一处理逻辑
			s.DispatchInbound(&message.InboundMsg{Msg: msg})
		}
	}
}

// DispatchInbound 将消息交给用户级 processor，供 Kafka runtime 复用。
func (s *Scheduler) DispatchInbound(in *message.InboundMsg) {
	if in == nil {
		return
	}

	msg := in.Msg
	if msg == nil {
		if in.Ack != nil {
			in.Ack(true)
		}
		return
	}

	if msg.MsgType == "event" {
		s.handleEvent(msg)
		if in.Ack != nil {
			in.Ack(true)
		}
		return
	}

	if msg.MsgType != "text" {
		log.GetInstance().Sugar.Warn("Skip non-text/event message type: ", msg.MsgType)
		if in.Ack != nil {
			in.Ack(true)
		}
		return
	}

	if msg.ExternalUserID == "" {
		log.GetInstance().Sugar.Warn("Message without external_user_id, type: ", msg.MsgType)
		if in.Ack != nil {
			in.Ack(true)
		}
		return
	}

	processor := s.getOrCreateProcessor(msg.ExternalUserID)

	select {
	case processor.messageChan <- in:
		select {
		case processor.resetTimer <- struct{}{}:
		default:
		}
		// 成功入队列即可调用ACK，不让单个用户的处理时延拖累整个Kafka的推送性能
		if in.Ack != nil {
			in.Ack(true)
		}
	case <-time.After(3 * time.Second):
		log.GetInstance().Sugar.Warnf("消息分发超时: user=%s", msg.ExternalUserID)
		if in.Ack != nil {
			// 正常消费掉这条消息
			in.Ack(true)
		}
	}
}

func (s *Scheduler) getProcessor(userID string) *Processor {
	s.processorsMu.RLock()
	defer s.processorsMu.RUnlock()
	if processor, exists := s.processors[userID]; exists {
		return processor
	} else {
		return nil
	}
}

// getOrCreateProcessor 获取或创建processor
func (s *Scheduler) getOrCreateProcessor(userID string) *Processor {
	// 先尝试读锁获取
	s.processorsMu.RLock()
	if processor, exists := s.processors[userID]; exists {
		s.processorsMu.RUnlock()
		return processor
	}
	s.processorsMu.RUnlock()

	// 需要创建新的processor，使用写锁
	s.processorsMu.Lock()
	defer s.processorsMu.Unlock()

	// 双重检查，避免并发创建
	if processor, exists := s.processors[userID]; exists {
		return processor
	}

	// 创建新的processor
	ctx, cancel := context.WithCancel(s.ctx)

	// 创建MiniMax客户端
	minimaxClient, err := minimax.NewClient()
	if err != nil {
		log.GetInstance().Sugar.Warn("Failed to create MiniMax client for user ", userID, ": ", err)
		// 即使MiniMax初始化失败，仍然创建processor（降级为echo模式）
		minimaxClient = nil
	} else {
		// 启用系统提示
		minimaxClient.EnableSystemPrompt(true)
	}

	processor := &Processor{
		userID:              userID,
		messageChan:         make(chan *message.InboundMsg, 100),
		lifetime:            s.lifetime,
		resetTimer:          make(chan struct{}, 1),
		ctx:                 ctx,
		cancel:              cancel,
		scheduler:           s,
		minimaxClient:       minimaxClient,
		conversationHistory: make([]minimax.Message, 0),
		rawConverHistory:    make([]minimax.RawMsg, 0),
		keywordExtractor:    NewKeywordExtractorService(s.recorderProducer, s.recorderTopic),
		seqChecker:          NewSequenceChecker(), // 初始化顺序检测器
		receivedMsgIDs:      make([]string, 0),    // 初始化消息ID列表
		// 并发关键词提取初始化
		keywordsChan: make(chan string, 100), // 缓冲channel
		keywordsMap:  make(map[string]interface{}),

		// 多位置分片初始化
		keywordsByLoc:     make(map[string]map[string]map[string]interface{}),
		finalLoc:          LocationKey{City: "上海市", District: ""},
		stagedNonLocation: make(map[string]interface{}),
	}

	// 建立当前记录视图：指向 finalLoc 对应记录（初始为默认空区）
	processor.currentKeywords = processor.ensureRecord(processor.finalLoc)
	// 兼容旧逻辑：keywordsMap 始终别名为 currentKeywords
	processor.keywordsMap = processor.currentKeywords

	// 初始化关键词提取worker
	processor.initKeywordsExtraction()

	// 保存到map
	s.processors[userID] = processor

	// 启动processor协程
	s.wg.Add(1)
	go processor.run()

	log.GetInstance().Sugar.Info("Created new processor for user: ", userID)

	return processor
}

// stopAllProcessors 停止所有processor
func (s *Scheduler) stopAllProcessors() {
	s.processorsMu.Lock()
	defer s.processorsMu.Unlock()

	for userID, processor := range s.processors {
		processor.cancel()
		log.GetInstance().Sugar.Info("Stopping processor for user: ", userID)
	}

	// 清空map
	s.processors = make(map[string]*Processor)

	// 不关闭furtherProcess channel，避免send on closed channel panic
	// 通过context.Done()让furtherTaskWorker自然退出
}

// furtherTaskWorker 后续任务处理worker（单个，保证顺序）
func (s *Scheduler) furtherTaskWorker() {
	defer s.wg.Done()
	log.GetInstance().Sugar.Info("Further-task worker started")
	defer log.GetInstance().Sugar.Info("Further-task worker stopped")

	for {
		select {
		case <-s.ctx.Done():
			return
		case task, ok := <-s.furtherProcess:
			if !ok {
				return
			}
			// 顺序处理每个任务
			s.handleFurtherTask(task)
		}
	}
}

// handleFurtherTask 处理单个后续任务
func (s *Scheduler) handleFurtherTask(task *FurtherProcessTask) {
	if task == nil {
		return
	}

	log.GetInstance().Sugar.Info("Processing further task for user: ", task.UserID)

	// 创建关键字提取服务
	extractor := NewKeywordExtractorService(s.recorderProducer, s.recorderTopic)

	// 合并对话历史中的非LLM回复，构造给Ipang使用的完整问答对
	mergedRawHistory := mergeRawHistoryForIpang(task.ConversationHistory, task.RawConverHistory)

	if anchor := task.Anchor; anchor != nil {
		anchorName := strings.TrimSpace(fmt.Sprint(anchor["name"]))
		if anchorName != "" {
			city := cityFromKeywords(task.Keywords)
			service, err := gaode.NewService()
			if err != nil {
				log.GetInstance().Sugar.Warnf("Gaode service init failed for user %s anchor %s: %v", task.UserID, anchorName, err)
			} else {
				ctx := s.ctx
				if ctx == nil {
					ctx = context.Background()
				}

				geo, err := service.GeocodeFirst(ctx, anchorName, city)
				if err != nil {
					log.GetInstance().Sugar.Warnf("Gaode geocode failed for user %s anchor %s city %s: %v", task.UserID, anchorName, city, err)
				} else if geo != nil {
					if coord := strings.TrimSpace(geo.Location); coord != "" {
						log.GetInstance().Sugar.Infof("Gaode geocode result for user %s anchor %s city %s: %+v", task.UserID, anchorName, city, geo)
						anchor["location"] = coord
					}
				}
			}

			if radius := anchorRadiiMeters(anchor); radius > 0 {
				anchor["radius_search_m"] = radius
			}

			updateAnchorInKeywords(task.Keywords, anchor)
		} else {
			log.GetInstance().Sugar.Warn("Anchor detected without name for user: ", task.UserID)
		}
	}

	// 4.3 优先使用聚合的关键词，避免重复提取（简单实用原则）
	if task.Keywords != nil && len(task.Keywords) > 0 {
		log.GetInstance().Sugar.Info("Using aggregated keywords for user: ", task.UserID, ", dimensions: ", len(task.Keywords))
		extractor.CallIpangAPIFromKeywords(task.UserID, task.OpenKFID, task.Keywords, mergedRawHistory)
	} else {
		// 降级到历史提取（兼容性保证）
		log.GetInstance().Sugar.Info("No aggregated keywords, fallback to conversation history extraction for user: ", task.UserID)
		extractor.ExtractAndCallIpangAPI(task.UserID, task.OpenKFID, task.ConversationHistory, mergedRawHistory)
	}
}

func mergeRawHistoryForIpang(conv []minimax.Message, raw []minimax.RawMsg) []minimax.RawMsg {
	if len(conv) == 0 {
		return raw
	}

	placeholder := "小胖正在思考中，很快给您回复哦…"
	merged := make([]minimax.RawMsg, 0, len(conv))

	for _, msg := range conv {
		content := strings.TrimSpace(extractMessageText(msg.Content))
		if content == "" {
			continue
		}

		switch msg.Role {
		case "user":
			merged = append(merged, minimax.RawMsg{
				Source:  "用户",
				Content: content,
			})
		case "assistant":
			if content == placeholder {
				continue
			}
			merged = append(merged, minimax.RawMsg{
				Source:  "AI",
				Content: content,
			})
		default:
			continue
		}
	}

	if len(merged) == 0 {
		return raw
	}

	return merged
}

func extractMessageText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []minimax.ContentItem:
		var builder strings.Builder
		for _, item := range v {
			if item.Type != "text" {
				continue
			}
			txt := strings.TrimSpace(item.Text)
			if txt == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(txt)
		}
		return builder.String()
	default:
		return ""
	}
}

func anchorRadiiMeters(anchor map[string]interface{}) int {
	if anchor == nil {
		return 0
	}

	keys := []string{
		"radius_search_m",
		"radius_m",
		"radius_max_m",
		"radius_min_m",
	}

	max := 0
	for _, key := range keys {
		if radius := toPositiveInt(anchor[key]); radius > max {
			max = radius
		}
	}
	return max
}

func updateAnchorInKeywords(keywords map[string]interface{}, anchor map[string]interface{}) {
	if keywords == nil || anchor == nil {
		return
	}
	loc, _ := keywords["location"].(map[string]interface{})
	if loc == nil {
		loc = make(map[string]interface{})
	}
	loc["anchor"] = anchor
	keywords["location"] = loc
}

func cityFromKeywords(keywords map[string]interface{}) string {
	city := "上海"
	if keywords == nil {
		return city
	}
	loc, ok := keywords["location"].(map[string]interface{})
	if !ok || loc == nil {
		return city
	}
	if province, ok := loc["province"].(string); ok && strings.TrimSpace(province) != "" {
		city = strings.TrimSpace(province)
	}
	return city
}

func toPositiveInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		if val > 0 {
			return val
		}
	case int64:
		if val > 0 {
			return int(val)
		}
	case float64:
		if val > 0 {
			return int(val)
		}
	case float32:
		if val > 0 {
			return int(val)
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && f > 0 {
			return int(f)
		}
	}
	return 0
}

// removeProcessor 移除processor（processor退出时调用）
func (s *Scheduler) removeProcessor(userID string) {
	s.processorsMu.Lock()
	defer s.processorsMu.Unlock()

	if processor, exists := s.processors[userID]; exists {
		// 不要close(processor.messageChan)，避免panic: send on closed channel
		// Go垃圾回收会自动处理不再被引用的processor和channel
		close(processor.resetTimer)
		delete(s.processors, userID)
		log.GetInstance().Sugar.Info("Removed processor for user: ", userID)
	}
}

// handleEvent 处理事件类型的消息
func (s *Scheduler) handleEvent(msg *kefu.KFRecvMessage) {
	if msg.Event == nil {
		log.GetInstance().Sugar.Warn("Event message without event content")
		return
	}

	log.GetInstance().Sugar.Info("Received event: ", msg.Event.EventType, " for user: ", msg.Event.ExternalUserID)

	switch msg.Event.EventType {
	case "enter_session":
		// 用户进入会话，发送欢迎消息
		s.sendWelcomeMessage(msg.Event.ExternalUserID, msg.Event.OpenKFID)
	default:
		log.GetInstance().Sugar.Debug("Unhandled event type: ", msg.Event.EventType)
	}
}

// sendWelcomeMessage 发送欢迎消息
func (s *Scheduler) sendWelcomeMessage(userID, openKFID string) {
	if userID == "" || openKFID == "" {
		log.GetInstance().Sugar.Warn("Cannot send welcome message: missing userID or openKFID")
		return
	}

	cfg := config.GetInstance()
	platform := common.Platform(cfg.Platform)

	// 获取访问令牌
	token, err := tokenstore.Instance().FetchCorpToken(
		platform,
		cfg.SuiteConfig.SuiteId,
		cfg.SuiteConfig.SuiteId,
		"",
		cfg.SuiteConfig.Secret,
	)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to fetch corp token for welcome message: ", err)
		return
	}

	// 创建欢迎消息
	welcomeMsg := &kefu.KFTextMessage{
		KFMessage: kefu.KFMessage{
			ToUser:    userID,
			OpenKFID:  openKFID,
			MsgType:   "text",
			CorpToken: token,
		},
	}
	welcomeMsg.Text.Content = "您好，小胖看房助手很高兴为您服务"

	// 发送欢迎消息
	result, err := kefu.SendKFText(welcomeMsg)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to send welcome message to user ", userID, ": ", err)
		return
	}

	log.GetInstance().Sugar.Info("Sent welcome message to user ", userID, ", msgid: ", result.MsgID)
	reportKefuMessage(welcomeMsg.MsgType)
}
