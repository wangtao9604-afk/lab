package kmq

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	qconfig "qywx/infrastructures/config"
	qlog "qywx/infrastructures/log"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// TopicPartitionKey 用于唯一标识主题与分区组合，防止多主题消费时分区号冲突。
type TopicPartitionKey struct {
	Topic     string
	Partition int32
}

// LifecycleHooks 定义分区级生命周期回调。
// - OnAssigned: 当分区被分配给当前消费者时触发；startOffset 为该分区的“起始消费 offset”（通常是已提交 offset，即“下一条要读”的 offset）。
// - OnRevoked : 当分区将从当前消费者撤销时触发（在 CommitContiguous 冲刷之后调用）。
type LifecycleHooks struct {
	OnAssigned func(topic string, partition int32, startOffset int64)
	OnRevoked  func(topic string, partition int32)
}

// Consumer 封装 confluent Kafka consumer，开启 cooperative-sticky 分配、手动偏移控制与分区提交闸门。
type Consumer struct {
	c     *kafka.Consumer
	gates map[TopicPartitionKey]*PartitionCommitGate

	gatesMu sync.RWMutex

	batchCommitManager *BatchCommitManager

	opts ConsumerOptions

	globalInflight chan struct{}

	partitionInflight   map[TopicPartitionKey]chan struct{}
	partitionWaitGroup  map[TopicPartitionKey]*sync.WaitGroup
	partitionInflightMu sync.Mutex

	groupID string
}

// ConsumerOptions 控制消费并发及批量提交参数。
type ConsumerOptions struct {
	MaxInflightPerPartition int
	MaxInflightGlobal       int
	BatchCommitInterval     time.Duration
	BatchCommitMaxPending   int
	PartitionDrainTimeout   time.Duration
	hooks                   LifecycleHooks
}

// ConsumerOption 用于配置 Consumer 行为。
type ConsumerOption func(*ConsumerOptions)

// WithMaxInflightPerPartition 限制单分区最大并发数量（<=0 表示不限制）。
func WithMaxInflightPerPartition(limit int) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.MaxInflightPerPartition = limit
	}
}

// WithMaxInflightGlobal 限制全局最大并发数量（<=0 表示不限制）。
func WithMaxInflightGlobal(limit int) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.MaxInflightGlobal = limit
	}
}

// WithBatchCommit 覆盖批量提交参数。
func WithBatchCommit(interval time.Duration, maxPending int) ConsumerOption {
	return func(opts *ConsumerOptions) {
		if interval > 0 {
			opts.BatchCommitInterval = interval
		}
		if maxPending > 0 {
			opts.BatchCommitMaxPending = maxPending
		}
	}
}

// WithPartitionDrainTimeout 设置撤销分区时等待 in-flight 消息完成的最长时间；<=0 表示无限等待。
func WithPartitionDrainTimeout(timeout time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.PartitionDrainTimeout = timeout
	}
}

// WithLifecycleHooks 通过可选项方式把回调注入 Consumer。
// 需要配合 consumer.Subscribe(...) 内部在 rebalance 回调中显式调用。
func WithLifecycleHooks(h LifecycleHooks) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.hooks = h
	}
}

const (
	defaultBatchCommitInterval   = 5 * time.Second
	defaultBatchCommitMaxPending = 100
	defaultPartitionDrainTimeout = 30 * time.Second
)

func defaultConsumerOptions() ConsumerOptions {
	return ConsumerOptions{
		BatchCommitInterval:   defaultBatchCommitInterval,
		BatchCommitMaxPending: defaultBatchCommitMaxPending,
		PartitionDrainTimeout: defaultPartitionDrainTimeout,
	}
}

// BatchCommitManager 负责批量调用 StoreOffsets/Commit，降低对 broker 的压力。
type BatchCommitManager struct {
	consumer        *Consumer
	commitInterval  time.Duration
	maxPendingCount int

	pendingCount  int
	pendingMu     sync.Mutex
	pendingTopics map[string]int

	stopCh    chan struct{}
	triggerCh chan struct{}
}

// NewBatchCommitManager 按给定时间间隔与阈值创建批量提交管理器。
func NewBatchCommitManager(consumer *Consumer, commitInterval time.Duration, maxPendingCount int) *BatchCommitManager {
	bcm := &BatchCommitManager{
		consumer:        consumer,
		commitInterval:  commitInterval,
		maxPendingCount: maxPendingCount,
		stopCh:          make(chan struct{}),
		triggerCh:       make(chan struct{}, 1),
		pendingTopics:   make(map[string]int),
	}

	go bcm.commitLoop()
	return bcm
}

// commitLoop 定时或在达到阈值时刷新已存储的偏移。
func (bcm *BatchCommitManager) commitLoop() {
	ticker := time.NewTicker(bcm.commitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bcm.flushCommits()
		case <-bcm.triggerCh:
			bcm.flushCommits()
			// 清理重复触发信号，避免多次提交。
			select {
			case <-bcm.triggerCh:
			default:
			}
		case <-bcm.stopCh:
			bcm.flushCommits()
			return
		}
	}
}

// Store 通过闸门存储偏移，并在达到阈值时触发提交。
func (bcm *BatchCommitManager) Store(gate *PartitionCommitGate) error {
	storedOffset, err := gate.StoreContiguous(bcm.consumer.c)
	if err != nil {
		return err
	}

	if storedOffset >= 0 {
		bcm.pendingMu.Lock()
		bcm.pendingCount++
		bcm.pendingTopics[gate.Topic()]++
		shouldCommit := bcm.maxPendingCount > 0 && bcm.pendingCount >= bcm.maxPendingCount
		bcm.pendingMu.Unlock()

		if shouldCommit {
			select {
			case bcm.triggerCh <- struct{}{}:
			default:
			}
		}
	}

	return nil
}

// flushCommits 调用底层 consumer 的 Commit 将已存储偏移提交给 broker。
func (bcm *BatchCommitManager) flushCommits() {
	bcm.pendingMu.Lock()
	if bcm.pendingCount == 0 {
		bcm.pendingMu.Unlock()
		return
	}
	pendingToCommit := bcm.pendingCount
	pendingTopicsSnapshot := make(map[string]int, len(bcm.pendingTopics))
	for topic, count := range bcm.pendingTopics {
		pendingTopicsSnapshot[topic] = count
	}
	bcm.pendingMu.Unlock()

	committed, err := bcm.consumer.c.Commit()
	if err != nil {
		qlog.GetInstance().Sugar.Warnf("Batch commit failed: %v", err)
		reported := make(map[string]struct{}, len(pendingTopicsSnapshot))
		for topic := range pendingTopicsSnapshot {
			if _, ok := reported[topic]; ok {
				continue
			}
			reported[topic] = struct{}{}
			ReportBatchCommit(topic, false)
		}
		return
	}

	reported := make(map[string]struct{}, len(committed))
	for _, tp := range committed {
		if tp.Topic == nil {
			continue
		}
		topic := *tp.Topic
		if _, ok := reported[topic]; ok {
			continue
		}
		reported[topic] = struct{}{}
		ReportBatchCommit(topic, true)
	}

	// 这里的 len(reported) == 0 出现在 Commit() 已返回成功（err==nil） 之后，只是 Kafka 没有在返回列表里携带任何分区信息。
	// 而我们为这批积累的 topic 做了快照 pendingTopicsSnapshot，这个兜底分支就是在“提交成功但 committed 为空”时，仍将其视为成功事件上报，否则指标会漏记。
	if len(reported) == 0 {
		for topic := range pendingTopicsSnapshot {
			ReportBatchCommit(topic, true)
			reported[topic] = struct{}{}
		}
	}

	bcm.pendingMu.Lock()
	bcm.pendingCount -= pendingToCommit
	if bcm.pendingCount < 0 {
		bcm.pendingCount = 0
	}
	for topic, count := range pendingTopicsSnapshot {
		remaining := bcm.pendingTopics[topic] - count
		if remaining <= 0 {
			delete(bcm.pendingTopics, topic)
		} else {
			bcm.pendingTopics[topic] = remaining
		}
	}
	bcm.pendingMu.Unlock()
}

// Stop 停止后台提交协程。
func (bcm *BatchCommitManager) Stop() {
	close(bcm.stopCh)
}

// NewConsumer 创建开启手动偏移管理与 cooperative-sticky 分配策略的 consumer。

func NewConsumer(brokers, groupID string, opts ...ConsumerOption) (*Consumer, error) {
	cm, effectiveGroupID, err := buildConsumerConfig(brokers, groupID)
	if err != nil {
		return nil, err
	}

	c, err := kafka.NewConsumer(cm)
	if err != nil {
		return nil, fmt.Errorf("create consumer failed: %w", err)
	}

	options := defaultConsumerOptions()
	for _, opt := range opts {
		opt(&options)
	}

	wc := &Consumer{
		c:       c,
		gates:   make(map[TopicPartitionKey]*PartitionCommitGate),
		opts:    options,
		groupID: effectiveGroupID,
	}

	if options.MaxInflightGlobal > 0 {
		wc.globalInflight = make(chan struct{}, options.MaxInflightGlobal)
	}

	if options.MaxInflightPerPartition > 0 {
		wc.partitionInflight = make(map[TopicPartitionKey]chan struct{})
	}
	wc.partitionWaitGroup = make(map[TopicPartitionKey]*sync.WaitGroup)

	wc.batchCommitManager = NewBatchCommitManager(wc, options.BatchCommitInterval, options.BatchCommitMaxPending)
	return wc, nil
}

func buildConsumerConfig(brokers, groupID string) (*kafka.ConfigMap, string, error) {
	cfg := qconfig.GetInstance().Kafka

	effectiveBrokers := brokers
	if effectiveBrokers == "" {
		effectiveBrokers = cfg.Brokers
	}
	if effectiveBrokers == "" {
		return nil, "", fmt.Errorf("kafka consumer: bootstrap servers not configured")
	}

	// 根据 groupID 选择使用 Chat 或 Recorder 配置
	var consumerCfg qconfig.KafkaConsumerConfig
	if groupID != "" && (strings.Contains(groupID, "recorder") || strings.Contains(groupID, "Recorder")) {
		consumerCfg = cfg.Consumer.Recorder
	} else {
		consumerCfg = cfg.Consumer.Chat
	}

	effectiveGroupID := groupID
	if effectiveGroupID == "" {
		effectiveGroupID = consumerCfg.GroupID
	}
	if effectiveGroupID == "" {
		return nil, "", fmt.Errorf("kafka consumer: group id not configured")
	}

	configMap := kafka.ConfigMap{
		"bootstrap.servers": effectiveBrokers,
		"group.id":          effectiveGroupID,
	}

	if consumerCfg.PartitionAssignmentStrategy != "" {
		configMap["partition.assignment.strategy"] = consumerCfg.PartitionAssignmentStrategy
	}
	if consumerCfg.EnableAutoCommit != nil {
		configMap["enable.auto.commit"] = *consumerCfg.EnableAutoCommit
	}
	if consumerCfg.EnableAutoOffsetStore != nil {
		configMap["enable.auto.offset.store"] = *consumerCfg.EnableAutoOffsetStore
	}
	if consumerCfg.AutoOffsetReset != "" {
		configMap["auto.offset.reset"] = consumerCfg.AutoOffsetReset
	}
	if consumerCfg.SessionTimeoutMs > 0 {
		configMap["session.timeout.ms"] = consumerCfg.SessionTimeoutMs
	}
	if consumerCfg.HeartbeatIntervalMs > 0 {
		configMap["heartbeat.interval.ms"] = consumerCfg.HeartbeatIntervalMs
	}
	if consumerCfg.MaxPollIntervalMs > 0 {
		configMap["max.poll.interval.ms"] = consumerCfg.MaxPollIntervalMs
	}
	if consumerCfg.FetchMinBytes > 0 {
		configMap["fetch.min.bytes"] = consumerCfg.FetchMinBytes
	}
	if consumerCfg.Debug != "" {
		configMap["debug"] = consumerCfg.Debug
	}

	if consumerCfg.FetchWaitMaxMs > 0 {
		configMap["fetch.wait.max.ms"] = consumerCfg.FetchWaitMaxMs
	}
	if consumerCfg.StatisticsIntervalMs > 0 {
		configMap["statistics.interval.ms"] = consumerCfg.StatisticsIntervalMs
	}
	if consumerCfg.SocketKeepaliveEnable != nil {
		configMap["socket.keepalive.enable"] = *consumerCfg.SocketKeepaliveEnable
	}
	if consumerCfg.MaxPartitionFetchBytes > 0 {
		configMap["max.partition.fetch.bytes"] = consumerCfg.MaxPartitionFetchBytes
	}
	if consumerCfg.FetchMaxBytes > 0 {
		configMap["fetch.max.bytes"] = consumerCfg.FetchMaxBytes
	}

	return &configMap, effectiveGroupID, nil
}

// Subscribe 注册 cooperative-sticky 的再平衡回调，并为分配到的分区初始化提交闸门。
func (cc *Consumer) Subscribe(topics []string) error {
	return cc.c.SubscribeTopics(topics, func(c *kafka.Consumer, e kafka.Event) error {
		switch ev := e.(type) {
		case kafka.AssignedPartitions:
			ReportRebalance(cc.groupID, "assigned")
			committed, err := c.Committed(ev.Partitions, 5000)
			committedMap := make(map[TopicPartitionKey]int64)
			if err == nil {
				for _, ctp := range committed {
					if ctp.Offset >= 0 && ctp.Topic != nil {
						committedMap[TopicPartitionKey{Topic: *ctp.Topic, Partition: ctp.Partition}] = int64(ctp.Offset)
					}
				}
			} else {
				qlog.GetInstance().Sugar.Warnf("Failed to get committed offsets: %v", err)
			}

			cc.gatesMu.Lock()
			for _, tp := range ev.Partitions {
				key := TopicPartitionKey{Topic: *tp.Topic, Partition: tp.Partition}

				var start int64
				if committedOffset, ok := committedMap[key]; ok {
					start = committedOffset
				} else {
					low, _, watermarkErr := c.QueryWatermarkOffsets(*tp.Topic, tp.Partition, 5000)
					if watermarkErr != nil {
						qlog.GetInstance().Sugar.Warnf("查询 %s:%d 水位失败，尝试使用缓存值: %v",
							*tp.Topic, tp.Partition, watermarkErr)
						low2, _, getErr := c.GetWatermarkOffsets(*tp.Topic, tp.Partition)
						if getErr == nil && low2 >= 0 {
							start = low2
							qlog.GetInstance().Sugar.Infof("使用缓存的低水位 %d 初始化 %s:%d", low2, *tp.Topic, tp.Partition)
						} else {
							start = 0
							qlog.GetInstance().Sugar.Errorf("严重警告：%s:%d 的水位查询全部失败，退化为 0。QueryErr=%v GetErr=%v",
								*tp.Topic, tp.Partition, watermarkErr, getErr)
						}
					} else {
						start = low
						qlog.GetInstance().Sugar.Debugf("使用实时低水位 %d 初始化 %s:%d", low, *tp.Topic, tp.Partition)
					}
				}

				cc.gates[key] = NewPartitionCommitGate(*tp.Topic, tp.Partition, start)
				qlog.GetInstance().Sugar.Debugf("为 %s:%d 初始化提交闸门，起始偏移 %d", *tp.Topic, tp.Partition, start)
				ReportGateBacklog(*tp.Topic, tp.Partition, 0)

				// 通知上层（例如：在 Scheduler 内为该分区创建 partitionSequencer）
				if cc.opts.hooks.OnAssigned != nil {
					cc.opts.hooks.OnAssigned(*tp.Topic, tp.Partition, start)
				}
			}
			cc.gatesMu.Unlock()
			return c.IncrementalAssign(ev.Partitions)

		case kafka.RevokedPartitions:
			ReportRebalance(cc.groupID, "revoked")
			cc.gatesMu.Lock()
			for _, tp := range ev.Partitions {
				key := TopicPartitionKey{Topic: *tp.Topic, Partition: tp.Partition}

				drained := cc.waitPartitionDrain(key)
				if !drained {
					qlog.GetInstance().Sugar.Warnf("Partition drain timeout before revoke: %s:%d (timeout=%v)", key.Topic, key.Partition, cc.opts.PartitionDrainTimeout)
				}
				if g, ok := cc.gates[key]; ok {
					if err := g.CommitContiguous(c); err != nil {
						qlog.GetInstance().Sugar.Warnf("Commit contiguous on revoke failed for %s:%d: %v", key.Topic, key.Partition, err)
					}
					delete(cc.gates, key)
					ReportGateBacklog(key.Topic, key.Partition, 0)
				}
				cc.cleanupPartitionState(key)

				if cc.opts.hooks.OnRevoked != nil {
					topic := ""
					if tp.Topic != nil {
						topic = *tp.Topic
					}
					cc.opts.hooks.OnRevoked(topic, tp.Partition)
				}
			}
			cc.gatesMu.Unlock()
			return c.IncrementalUnassign(ev.Partitions)

		}
		return nil
	})
}

func (cc *Consumer) acquireInflight(key TopicPartitionKey) func() {
	releases := make([]func(), 0, 3)

	sem, wg := cc.ensurePartitionControl(key)
	if wg != nil {
		wg.Add(1)
		releases = append(releases, func() { wg.Done() })
	}

	if cc.globalInflight != nil {
		cc.globalInflight <- struct{}{}
		releases = append(releases, func() { <-cc.globalInflight })
	}

	if sem != nil {
		sem <- struct{}{}
		releases = append(releases, func() { <-sem })
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			// 获取资源时按顺序压栈，释放时反向弹栈
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		})
	}
}

func (cc *Consumer) ensurePartitionControl(key TopicPartitionKey) (chan struct{}, *sync.WaitGroup) {
	cc.partitionInflightMu.Lock()
	defer cc.partitionInflightMu.Unlock()

	var sem chan struct{}
	if cc.partitionInflight != nil && cc.opts.MaxInflightPerPartition > 0 {
		sem = cc.partitionInflight[key]
		if sem == nil {
			sem = make(chan struct{}, cc.opts.MaxInflightPerPartition)
			cc.partitionInflight[key] = sem
		}
	}

	if cc.partitionWaitGroup == nil {
		cc.partitionWaitGroup = make(map[TopicPartitionKey]*sync.WaitGroup)
	}

	wg := cc.partitionWaitGroup[key]
	if wg == nil {
		wg = &sync.WaitGroup{}
		cc.partitionWaitGroup[key] = wg
	}

	return sem, wg
}

func (cc *Consumer) getPartitionWaitGroup(key TopicPartitionKey) *sync.WaitGroup {
	cc.partitionInflightMu.Lock()
	defer cc.partitionInflightMu.Unlock()
	if cc.partitionWaitGroup == nil {
		return nil
	}
	return cc.partitionWaitGroup[key]
}

func (cc *Consumer) cleanupPartitionState(key TopicPartitionKey) {
	cc.partitionInflightMu.Lock()
	defer cc.partitionInflightMu.Unlock()
	if cc.partitionInflight != nil {
		delete(cc.partitionInflight, key)
	}
	if cc.partitionWaitGroup != nil {
		delete(cc.partitionWaitGroup, key)
	}
}

func (cc *Consumer) waitPartitionDrain(key TopicPartitionKey) bool {
	wg := cc.getPartitionWaitGroup(key)
	if wg == nil {
		return true
	}

	timeout := cc.opts.PartitionDrainTimeout
	if timeout <= 0 {
		wg.Wait()
		return true
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// MessageHandler 处理消息并返回是否已经完成（成功或写入 DLQ），只有返回 true 才会推进提交闸门。
type MessageHandler func(m *kafka.Message) (handled bool, err error)

// PollAndDispatch 持续轮询并分发消息，只有 handler 返回 true 才会推进提交；处理逻辑在分区级协程内执行。
// 当 ctx 被取消时，函数会优雅退出（不再接收新消息，但已启动的 handler 会继续执行）。
func (cc *Consumer) PollAndDispatch(ctx context.Context, handler MessageHandler, pollMs int) {
	for {
		select {
		case <-ctx.Done():
			qlog.GetInstance().Sugar.Info("PollAndDispatch: context cancelled, exiting poll loop")
			return
		default:
		}

		ev := cc.c.Poll(pollMs)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			part := e.TopicPartition.Partition
			topic := *e.TopicPartition.Topic
			off := int64(e.TopicPartition.Offset)

			key := TopicPartitionKey{Topic: topic, Partition: part}

			cc.gatesMu.RLock()
			gate := cc.gates[key]
			cc.gatesMu.RUnlock()
			if gate == nil {
				qlog.GetInstance().Sugar.Warnf("未找到 %s:%d 对应的提交闸门", topic, part)
				continue
			}

			gate.EnsureInit(off)

			release := cc.acquireInflight(key)
			go func(m *kafka.Message, g *PartitionCommitGate, offset int64, done func(), partitionKey TopicPartitionKey) {
				defer done()
				handled, err := handler(m)
				if err != nil {
					qlog.GetInstance().Sugar.Warnf("业务处理返回错误: %v", err)
				}
				if handled {
					g.MarkDone(offset)
					if err := cc.batchCommitManager.Store(g); err != nil {
						qlog.GetInstance().Sugar.Warnf("批量存储偏移失败: %v", err)
					}
				}
			}(e, gate, off, release, key)

		case kafka.Error:
			qlog.GetInstance().Sugar.Warnf("Kafka 错误事件: %v", e)
		default:
			// 忽略其余事件
		}
	}
}

// AsyncMessageHandler 与 MessageHandler 类似，但提供 ack 回调由下游决定何时推进提交。
type AsyncMessageHandler func(m *kafka.Message, ack func(success bool))

// PollAndDispatchWithAck 轮询消息并交给 handler，业务完成后需调用 ack(true) 才会推进提交。
// 当 ctx 被取消时，函数会优雅退出（不再接收新消息，但已启动的 handler 会继续执行）。
func (cc *Consumer) PollAndDispatchWithAck(ctx context.Context, handler AsyncMessageHandler, pollMs int) {
	for {
		select {
		case <-ctx.Done():
			qlog.GetInstance().Sugar.Info("PollAndDispatchWithAck: context cancelled, exiting poll loop")
			return
		default:
		}

		ev := cc.c.Poll(pollMs)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			part := e.TopicPartition.Partition
			topic := *e.TopicPartition.Topic
			off := int64(e.TopicPartition.Offset)

			key := TopicPartitionKey{Topic: topic, Partition: part}

			cc.gatesMu.RLock()
			gate := cc.gates[key]
			cc.gatesMu.RUnlock()
			if gate == nil {
				qlog.GetInstance().Sugar.Warnf("未找到 %s:%d 对应的提交闸门", topic, part)
				continue
			}

			gate.EnsureInit(off)

			release := cc.acquireInflight(key)
			var onceGuard sync.Once
			ack := func(success bool) {
				onceGuard.Do(func() {
					if success {
						gate.MarkDone(off)
						if err := cc.batchCommitManager.Store(gate); err != nil {
							qlog.GetInstance().Sugar.Warnf("batch store failed: %v", err)
						}
					}
					release()
				})
			}

			go handler(e, ack)

		case kafka.Error:
			qlog.GetInstance().Sugar.Warnf("Kafka 错误事件: %v", e)
		default:
			// 忽略其余事件
		}
	}
}

// Close 停止批量提交管理器并关闭底层 consumer。
func (cc *Consumer) Close() {
	if cc.batchCommitManager != nil {
		cc.batchCommitManager.Stop()
	}
	cc.c.Close()
}
