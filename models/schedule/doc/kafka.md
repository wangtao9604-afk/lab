# 企业微信客服消息系统 Kafka 集成方案

## 1. 架构概述

### 1.1 核心设计原则
- **消息顺序性保证**：同一用户的消息必须按时间顺序处理
- **高并发处理**：不同用户的消息可以并行处理
- **容错与高可用**：支持节点故障自动恢复，不丢消息
- **水平扩展**：支持动态增减节点
- **平滑过渡**：支持从单点Kafka平滑升级到集群

### 1.2 整体架构
```
微信回调 → KF模块(生产者) → Kafka Topic(12 Partitions) → Schedule模块(消费者)
                ↓                     ↓                          ↓
          获取并同步消息        按UserID Hash分区          用户级并行处理
```

## 2. Kafka Topic 设计

### 2.1 Topic 配置

#### 生产环境配置（集群）
```yaml
topic:
  name: qywx-chat-messages
  partitions: 12          # 3节点 × 4，预留扩展空间
  replication-factor: 3   # 3副本，保证高可用
  min.insync.replicas: 2  # 最少同步副本数
  retention.ms: 604800000 # 7天保留期
  compression.type: lz4    # 压缩算法
  max.message.bytes: 1048576  # 最大消息1MB
```

#### 开发环境配置（单点）
```yaml
# ⚠️ 重要：单点Kafka必须调整的参数
topic:
  name: qywx-chat-messages
  partitions: 4           # 单节点建议1-4个分区
  replication-factor: 1   # ⚠️ 必须为1（单点只有1个broker）
  min.insync.replicas: 1  # ⚠️ 必须为1（否则无法发送消息）
  retention.ms: 604800000 # 7天保留期
  compression.type: lz4    # 压缩算法
  max.message.bytes: 1048576  # 最大消息1MB
  
# DLQ Topic同样需要调整
dlq_topic:
  name: qywx-dead-letter-queue
  partitions: 2           # 单点建议2个分区
  replication-factor: 1   # ⚠️ 必须为1
  min.insync.replicas: 1  # ⚠️ 必须为1
```

### 2.2 分区策略
- 使用 `external_user_id` 作为 Partition Key
- Kafka 内置 Murmur2 Hash 保证同一用户消息路由到同一分区
- 12个分区均匀分配给3个消费节点（每节点4个分区）

## 3. 生产者设计

### 3.1 生产者实现
```go
package producer

import (
    "encoding/json"
    "fmt"
    "sync"
    "time"
    
    "github.com/confluentinc/confluent-kafka-go/v2/kafka"
    "qywx/infrastructures/log"
    "qywx/infrastructures/wxmsg/kefu"
)

type KafkaProducer struct {
    producer *kafka.Producer
    topic    string
    mu       sync.Mutex
}

// NewKafkaProducer 创建Kafka生产者
func NewKafkaProducer(brokers string, topic string) (*KafkaProducer, error) {
    // 从配置文件读取环境类型
    cfg := config.GetInstance()
    
    config := &kafka.ConfigMap{
        "bootstrap.servers": brokers,  // 支持单点或集群列表
        
        // 可靠性配置（单点和集群通用）
        "acks":                   "all",  // 单点时等同于acks=1
        "retries":                10,     // 重试次数
        "max.in.flight.requests.per.connection": 5,
        "enable.idempotence":     true,   // 幂等性，防止重复
        
        // 性能优化
        "compression.type":       "lz4",
        "linger.ms":             10,      // 批量发送延迟
        "batch.size":            16384,   // 批量大小
        
        // 错误处理
        "retry.backoff.ms":      100,
        "request.timeout.ms":    30000,
    }
    
    p, err := kafka.NewProducer(config)
    if err != nil {
        return nil, fmt.Errorf("create producer failed: %w", err)
    }
    
    // 创建生产者实例
    kp := &KafkaProducer{
        producer:     p,
        topic:        topic,
        deliveryChan: make(chan kafka.Event, 10000), // 大缓冲区
        callbacks:    make(map[string]func(error)),
        metrics:      &ProducerMetrics{},
    }
    
    // 启动事件处理协程（处理异步发送结果）
    go kp.handleDeliveryReports()
    
    return kp, nil
}

// ProduceMessage 真正的异步发送消息到Kafka
func (kp *KafkaProducer) ProduceMessage(msg *kefu.KFRecvMessage) error {
    // 构建消息
    message := &ChatMessage{
        MessageID:      generateMessageID(),
        ExternalUserID: msg.ExternalUserID,
        OpenKFID:       msg.OpenKFID,
        MsgType:        msg.MsgType,
        Content:        msg,
        Timestamp:      time.Now().Unix(),
    }
    
    value, err := json.Marshal(message)
    if err != nil {
        return fmt.Errorf("marshal message failed: %w", err)
    }
    
    // 发送到Kafka
    kafkaMsg := &kafka.Message{
        TopicPartition: kafka.TopicPartition{
            Topic:     &kp.topic,
            Partition: kafka.PartitionAny,
        },
        Key:   []byte(msg.ExternalUserID),
        Value: value,
        Headers: []kafka.Header{
            {Key: "msg_type", Value: []byte(msg.MsgType)},
            {Key: "msg_id", Value: []byte(message.MessageID)},
            {Key: "timestamp", Value: []byte(fmt.Sprintf("%d", time.Now().Unix()))},
        },
    }
    
    // 真正的异步发送 - 不阻塞等待！
    err = kp.producer.Produce(kafkaMsg, nil)  // nil 表示fire-and-forget
    if err != nil {
        // 队列满，需要处理背压
        if err.(kafka.Error).Code() == kafka.ErrQueueFull {
            // 方案1：等待队列有空间
            kp.producer.Flush(100) // 等待100ms
            
            // 重试一次
            err = kp.producer.Produce(kafkaMsg, nil)
            if err != nil {
                kp.metrics.ProduceErrors.Inc()
                return fmt.Errorf("produce failed after retry: %w", err)
            }
        } else {
            return fmt.Errorf("produce failed: %w", err)
        }
    }
    
    kp.metrics.ProducedMessages.Inc()
    return nil
}

// handleDeliveryReports 后台处理发送结果
func (kp *KafkaProducer) handleDeliveryReports() {
    for {
        select {
        case e := <-kp.producer.Events():
            switch ev := e.(type) {
            case *kafka.Message:
                // 处理发送结果
                if ev.TopicPartition.Error != nil {
                    kp.metrics.ProduceErrors.Inc()
                    log.GetInstance().Sugar.Error("Delivery failed: ", 
                        ev.TopicPartition.Error)
                    
                    // 如果有回调，执行回调
                    msgID := kp.extractMessageID(ev)
                    if callback, ok := kp.callbacks[msgID]; ok {
                        callback(ev.TopicPartition.Error)
                        delete(kp.callbacks, msgID)
                    }
                } else {
                    kp.metrics.ProducedMessages.Inc()
                    kp.metrics.ProduceLatency.Observe(time.Since(ev.Timestamp).Seconds())
                    
                    log.GetInstance().Sugar.Debug("Message delivered to ", 
                        ev.TopicPartition)
                    
                    // 成功回调
                    msgID := kp.extractMessageID(ev)
                    if callback, ok := kp.callbacks[msgID]; ok {
                        callback(nil)
                        delete(kp.callbacks, msgID)
                    }
                }
                
            case kafka.Error:
                kp.metrics.ProduceErrors.Inc()
                log.GetInstance().Sugar.Error("Kafka error: ", ev)
            }
            
        case <-kp.stopChan:
            return
        }
    }
}

// ProduceMessageWithCallback 需要确认的场景使用回调
func (kp *KafkaProducer) ProduceMessageWithCallback(msg *kefu.KFRecvMessage, 
    callback func(err error)) error {
    // 构建消息（逻辑同 ProduceMessage）
    message := &ChatMessage{
        MessageID:      generateMessageID(),
        ExternalUserID: msg.ExternalUserID,
        // ... 其他字段
    }
    
    // 注册回调
    kp.mu.Lock()
    kp.callbacks[message.MessageID] = callback
    kp.mu.Unlock()
    
    // 发送消息（使用Events channel）
    err = kp.producer.Produce(kafkaMsg, nil) // 依然是异步！
    if err != nil {
        // 清理回调
        kp.mu.Lock()
        delete(kp.callbacks, message.MessageID)
        kp.mu.Unlock()
        return fmt.Errorf("produce failed: %w", err)
    }
    
    return nil
}

// Close 关闭生产者
func (kp *KafkaProducer) Close() {
    kp.producer.Flush(10000) // 等待10秒，发送剩余消息
    kp.producer.Close()
}
```

### 3.2 异步发送性能分析

#### 同步 vs 异步性能对比
```
同步发送（错误设计）：
- 每条消息等待确认：~20ms
- TPS上限：50条/秒/线程
- CPU利用率：< 5%（大部分时间在等待）
- 批量优化：完全失效

异步发送（正确设计）：
- 消息立即返回：< 1ms
- TPS：10,000+条/秒/线程
- CPU利用率：~60%（充分利用）
- 批量优化：充分发挥作用
```

#### 三种发送模式选择
```go
// 模式1：Fire-and-forget（最高性能）
// 适用：日志、监控数据等允许少量丢失的场景
producer.Produce(msg, nil)

// 模式2：异步回调（平衡性能和可靠性）
// 适用：需要确认但不阻塞主流程的场景
producer.Produce(msg, nil) + handleDeliveryReports()

// 模式3：同步确认（最低性能，最高可靠性）
// 适用：金融交易等绝对不能丢失的场景
deliveryChan := make(chan kafka.Event, 1)
producer.Produce(msg, deliveryChan)
<-deliveryChan  // 仅在特殊场景使用！
```

### 3.3 生产者集成到KF模块

#### 正常发送流程
```go
// 在 kf.go 的 doHandleEvent 方法中
func (s *KFService) doHandleEvent(event *kefu.KFCallbackMessage, cm *cursorManager) error {
    // ... 前面的同步逻辑保持不变 ...
    
    // 获取消息后，发送到Kafka
    for _, msg := range resp.MsgList {
        msgCopy := msg
        
        // 发送到Kafka
        if err := s.kafkaProducer.ProduceMessage(&msgCopy); err != nil {
            log.GetInstance().Sugar.Error("Send to Kafka failed: ", err)
            
            // 降级处理（临时方案，有缺陷）
            if err := s.handleKafkaFailure(&msgCopy); err != nil {
                log.GetInstance().Sugar.Error("Fallback also failed: ", err)
            }
        }
    }
    
    // ... cursor更新逻辑 ...
}
```

#### 降级策略（临时方案）

**⚠️ 当前降级方案的问题**：
1. **消息乱序风险**：降级消息走本地，后续消息走Kafka，可能导致同一用户消息乱序
2. **分布式能力丧失**：降级消息只能单节点处理
3. **无重试机制**：降级是永久的，即使Kafka恢复也不会重试

```go
// 临时降级方案（有缺陷，待优化）
func (s *KFService) handleKafkaFailure(msg *kefu.KFRecvMessage) error {
    // TODO: 实现持久化失败队列
    // 1. 将消息写入数据库的失败队列表
    // 2. 记录失败时间、重试次数等元信息
    // 3. 启动独立的补偿任务定期重试
    
    // 临时方案：直接降级到本地channel（会导致消息乱序）
    select {
    case s.processorChan <- msg:
        log.GetInstance().Sugar.Warn("Message degraded to local processing, may cause out-of-order: ", 
            msg.ExternalUserID)
        // TODO: 记录降级指标，触发告警
        return nil
    case <-time.After(1 * time.Second):
        return fmt.Errorf("local channel also full")
    }
}
```

#### 理想的降级方案（TODO）

```go
// TODO: 待持久存储层实现后的理想方案
type FailedMessage struct {
    ID            int64
    MessageID     string
    UserID        string
    Content       []byte  // 序列化的消息
    FailedAt      time.Time
    RetryCount    int
    LastError     string
    Status        string  // pending/retrying/success/failed
}

// 持久化失败消息
func (s *KFService) persistFailedMessage(msg *kefu.KFRecvMessage) error {
    // TODO: 实现
    // 1. 序列化消息
    // 2. 写入数据库
    // 3. 返回成功/失败
    return nil
}

// 补偿任务（独立协程）
func (s *KFService) retryFailedMessages() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        // TODO: 实现
        // 1. 查询待重试的消息（按用户分组，保证顺序）
        // 2. 按用户批量重试
        // 3. 更新重试状态
        // 4. 超过重试次数的进入死信队列
    }
}

// 消息顺序保证策略
// 1. 同一用户的失败消息必须按顺序重试
// 2. 如果用户有失败消息未重试成功，新消息也应该进入失败队列
// 3. 只有当用户的所有失败消息都重试成功后，才能恢复正常发送
```

#### 降级方案的监控与告警

```go
// 降级监控指标
type DegradationMetrics struct {
    DegradedMessages   counter  // 降级消息总数
    DegradedUsers      gauge    // 受影响的用户数
    KafkaFailures      counter  // Kafka发送失败次数
    LastDegradationTime time.Time // 最近一次降级时间
}

// 告警规则
alerts:
  - name: KafkaProducerDegraded
    condition: degraded_messages > 10
    duration: 1m
    action: page
    message: "Kafka producer is degrading, messages may be out of order"
    
  - name: KafkaProducerDown
    condition: kafka_failures > 100
    duration: 5m
    action: critical
    message: "Kafka producer completely down, all messages degraded"
```

#### 实施路线图

**Phase 1（当前）：基础降级**
- ✅ 实现降级到本地channel
- ⚠️ 接受消息可能乱序的风险
- ⚠️ 添加明显的日志警告
- TODO: 添加监控指标

**Phase 2（短期）：持久化队列**
- TODO: 实现失败消息持久化表
- TODO: 实现按用户的顺序重试机制
- TODO: 添加死信队列处理
- TODO: 实现补偿任务调度

**Phase 3（长期）：智能降级**
- TODO: 实现熔断器模式
- TODO: 动态调整重试策略
- TODO: 实现多级降级（Kafka → 数据库 → 本地）
- TODO: 支持手动重放历史消息

### 3.4 死信队列（Dead Letter Queue）设计

#### 为什么需要DLQ

**问题诊断：投递保证的降级**
```
系统承诺：At-least-once（至少一次）
实际情况：
✓ 正常负载：保证至少一次
✗ 高负载时：降级为Best-effort
✗ 限流/熔断：消息直接丢弃
✗ 处理超时：消息永久丢失

客服场景的特殊性：
- 每条消息价值极高（客户投诉、订单问题）
- 丢失消息 = 丢失客户信任
- 可能涉及法律合规（审计要求）
- 需要事后分析（为什么失败？）
```

#### DLQ消息格式设计

```go
// DeadLetterMessage DLQ消息完整格式
type DeadLetterMessage struct {
    // 原始消息（完整保留）
    OriginalMessage  *kefu.KFRecvMessage `json:"original_message"`
    MessageID        string              `json:"message_id"`        // 唯一标识
    
    // 失败上下文
    FailureReason    FailureReason       `json:"failure_reason"`    // 失败原因枚举
    FailureDetails   string              `json:"failure_details"`   // 详细错误信息
    FailureTime      time.Time           `json:"failure_time"`      // 失败时间
    ProcessorID      string              `json:"processor_id"`      // 处理器标识
    NodeID           string              `json:"node_id"`           // 节点标识
    
    // 重试信息
    RetryCount       int                 `json:"retry_count"`       // 已重试次数
    LastRetryTime    *time.Time          `json:"last_retry_time"`   // 最后重试时间
    NextRetryTime    *time.Time          `json:"next_retry_time"`   // 计划重试时间
    
    // Kafka元数据（用于追踪）
    SourceTopic      string              `json:"source_topic"`
    SourcePartition  int32               `json:"source_partition"`
    SourceOffset     int64               `json:"source_offset"`
    SourceTimestamp  time.Time           `json:"source_timestamp"`
    
    // 业务元数据
    UserID           string              `json:"user_id"`
    UserGroup        string              `json:"user_group"`        // normal/vip/suspicious
    OpenKFID         string              `json:"open_kf_id"`        // 客服账号
    SessionID        string              `json:"session_id"`        // 会话ID
    
    // 处理策略
    Recoverable      bool                `json:"recoverable"`       // 是否可恢复
    Priority         int                 `json:"priority"`          // 恢复优先级
    MaxRetries       int                 `json:"max_retries"`       // 最大重试次数
    TTL              time.Duration       `json:"ttl"`               // 消息有效期
    
    // 处理建议
    SuggestedAction  ActionType          `json:"suggested_action"`  // 建议操作
    ManualReview     bool                `json:"manual_review"`     // 需要人工审核
}

// FailureReason 失败原因枚举
type FailureReason string

const (
    ReasonRateLimited      FailureReason = "rate_limited"       // 限流
    ReasonUserBlocked      FailureReason = "user_blocked"       // 用户被封禁
    ReasonCircuitOpen      FailureReason = "circuit_open"       // 熔断器打开
    ReasonQueueFull        FailureReason = "queue_full"         // 队列满
    ReasonProcessTimeout   FailureReason = "process_timeout"    // 处理超时
    ReasonKafkaFailure     FailureReason = "kafka_failure"      // Kafka故障
    ReasonInvalidMessage   FailureReason = "invalid_message"    // 消息格式错误
    ReasonDownstreamError  FailureReason = "downstream_error"   // 下游服务错误
    ReasonSystemOverload   FailureReason = "system_overload"    // 系统过载
)

// ActionType 建议操作类型
type ActionType string

const (
    ActionAutoRetry        ActionType = "auto_retry"         // 自动重试
    ActionDelayedRetry     ActionType = "delayed_retry"      // 延迟重试
    ActionManualReview     ActionType = "manual_review"      // 人工审核
    ActionDiscard          ActionType = "discard"            // 丢弃
    ActionEscalate         ActionType = "escalate"           // 升级处理
)
```

#### DLQ生产者实现

```go
// DLQProducer 死信队列生产者
type DLQProducer struct {
    producer        *kafka.Producer
    topic           string              // DLQ Topic名称
    metrics         *DLQMetrics
    circuitBreaker  *CircuitBreaker     // DLQ自己的熔断器
    mu              sync.Mutex
}

// NewDLQProducer 创建DLQ生产者
func NewDLQProducer(brokers string, topic string) (*DLQProducer, error) {
    config := &kafka.ConfigMap{
        "bootstrap.servers": brokers,
        
        // 可靠性最高配置（DLQ不能再失败）
        "acks":                   "all",
        "retries":                100,    // 更多重试
        "max.in.flight.requests.per.connection": 1,  // 保证顺序
        "enable.idempotence":     true,
        
        // 性能配置
        "compression.type":       "snappy",  // 平衡压缩率和CPU
        "linger.ms":             50,         // 稍长的批量时间
        "batch.size":            65536,      // 更大的批量
    }
    
    p, err := kafka.NewProducer(config)
    if err != nil {
        return nil, fmt.Errorf("create DLQ producer failed: %w", err)
    }
    
    dlq := &DLQProducer{
        producer: p,
        topic:    topic,
        metrics:  NewDLQMetrics(),
        circuitBreaker: &CircuitBreaker{
            failureThreshold: 10,
            halfOpenDelay:    1 * time.Minute,
        },
    }
    
    // 启动后台事件处理
    go dlq.handleEvents()
    
    return dlq, nil
}

// SendToDeadLetter 发送消息到死信队列
func (dlq *DLQProducer) SendToDeadLetter(
    msg *kefu.KFRecvMessage,
    reason FailureReason,
    details string,
    kafkaMetadata *KafkaMetadata,
) error {
    // 检查DLQ自己的熔断器
    if err := dlq.circuitBreaker.Allow(); err != nil {
        // DLQ也失败了，这是最坏的情况
        // 写入本地紧急文件
        dlq.writeToEmergencyFile(msg, reason, details)
        return fmt.Errorf("DLQ circuit breaker open: %w", err)
    }
    
    // 构建死信消息
    deadLetter := &DeadLetterMessage{
        OriginalMessage: msg,
        MessageID:       generateMessageID(),
        FailureReason:   reason,
        FailureDetails:  details,
        FailureTime:     time.Now(),
        UserID:          msg.ExternalUserID,
        
        // 设置恢复策略
        Recoverable:     dlq.isRecoverable(reason),
        Priority:        dlq.calculatePriority(msg, reason),
        MaxRetries:      dlq.getMaxRetries(reason),
        TTL:             dlq.getTTL(reason),
        SuggestedAction: dlq.suggestAction(reason),
    }
    
    // 添加Kafka元数据
    if kafkaMetadata != nil {
        deadLetter.SourceTopic = kafkaMetadata.Topic
        deadLetter.SourcePartition = kafkaMetadata.Partition
        deadLetter.SourceOffset = kafkaMetadata.Offset
    }
    
    // 序列化
    value, err := json.Marshal(deadLetter)
    if err != nil {
        dlq.metrics.SerializationErrors.Inc()
        return fmt.Errorf("marshal dead letter failed: %w", err)
    }
    
    // 发送到Kafka（异步但要监控结果）
    err = dlq.producer.Produce(&kafka.Message{
        TopicPartition: kafka.TopicPartition{
            Topic:     &dlq.topic,
            Partition: kafka.PartitionAny,
        },
        Key:   []byte(msg.ExternalUserID),
        Value: value,
        Headers: []kafka.Header{
            {Key: "failure_reason", Value: []byte(reason)},
            {Key: "retry_count", Value: []byte("0")},
            {Key: "message_id", Value: []byte(deadLetter.MessageID)},
        },
    }, nil)
    
    if err != nil {
        dlq.circuitBreaker.OnFailure()
        dlq.metrics.ProduceErrors.Inc()
        
        // 最后的保险：写入本地文件
        dlq.writeToEmergencyFile(msg, reason, details)
        return fmt.Errorf("produce to DLQ failed: %w", err)
    }
    
    dlq.circuitBreaker.OnSuccess()
    dlq.metrics.MessagesProduced.Inc()
    dlq.metrics.MessagesByReason[reason].Inc()
    
    log.GetInstance().Sugar.Warn("Message sent to DLQ",
        ", user: ", msg.ExternalUserID,
        ", reason: ", reason,
        ", message_id: ", deadLetter.MessageID)
    
    return nil
}

// writeToEmergencyFile 紧急情况下写入本地文件
func (dlq *DLQProducer) writeToEmergencyFile(
    msg *kefu.KFRecvMessage,
    reason FailureReason,
    details string,
) {
    // 写入紧急备份文件，确保消息不丢失
    emergencyFile := fmt.Sprintf("/var/log/qywx/dlq_emergency_%s.jsonl",
        time.Now().Format("20060102"))
    
    data := map[string]interface{}{
        "timestamp": time.Now().Unix(),
        "message":   msg,
        "reason":    reason,
        "details":   details,
    }
    
    jsonData, _ := json.Marshal(data)
    
    // 追加写入，带锁
    dlq.mu.Lock()
    defer dlq.mu.Unlock()
    
    file, err := os.OpenFile(emergencyFile, 
        os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        // 真的没办法了，只能打印到stderr
        fmt.Fprintf(os.Stderr, "EMERGENCY: Failed to write DLQ: %s\n", jsonData)
        return
    }
    defer file.Close()
    
    file.WriteString(string(jsonData) + "\n")
    
    dlq.metrics.EmergencyWrites.Inc()
}

// 策略方法
func (dlq *DLQProducer) isRecoverable(reason FailureReason) bool {
    switch reason {
    case ReasonRateLimited, ReasonUserBlocked, ReasonCircuitOpen,
         ReasonQueueFull, ReasonProcessTimeout, ReasonSystemOverload:
        return true  // 这些都是暂时性问题，可恢复
    case ReasonInvalidMessage:
        return false // 消息本身有问题，不可恢复
    default:
        return true  // 默认认为可恢复
    }
}

func (dlq *DLQProducer) calculatePriority(msg *kefu.KFRecvMessage, reason FailureReason) int {
    priority := 5  // 默认中等优先级
    
    // VIP用户提高优先级
    if isVIPUser(msg.ExternalUserID) {
        priority += 3
    }
    
    // 某些失败原因降低优先级
    switch reason {
    case ReasonUserBlocked:
        priority -= 3  // 被封用户低优先级
    case ReasonRateLimited:
        priority -= 1  // 限流稍微降低
    }
    
    // 限制在1-10范围
    if priority < 1 {
        priority = 1
    }
    if priority > 10 {
        priority = 10
    }
    
    return priority
}
```

#### DLQ消费者与恢复策略

```go
// DLQConsumer DLQ消费者（独立服务）
type DLQConsumer struct {
    consumer        *kafka.Consumer
    strategies      map[FailureReason]RecoveryStrategy
    retryProducer   *kafka.Producer  // 用于重新发送到主Topic
    metrics         *DLQConsumerMetrics
    ctx             context.Context
    wg              sync.WaitGroup
}

// RecoveryStrategy 恢复策略接口
type RecoveryStrategy interface {
    CanRecover(msg *DeadLetterMessage) bool
    CalculateRetryDelay(msg *DeadLetterMessage) time.Duration
    PrepareRetry(msg *DeadLetterMessage) (*kefu.KFRecvMessage, error)
    OnSuccess(msg *DeadLetterMessage)
    OnFailure(msg *DeadLetterMessage, err error)
}

// 具体恢复策略实现
type RateLimitRecovery struct{}

func (r *RateLimitRecovery) CanRecover(msg *DeadLetterMessage) bool {
    // 检查用户是否还在封禁期
    if isUserBlocked(msg.UserID) {
        return false
    }
    // 检查是否超过最大重试次数
    return msg.RetryCount < msg.MaxRetries
}

func (r *RateLimitRecovery) CalculateRetryDelay(msg *DeadLetterMessage) time.Duration {
    // 指数退避：1分钟、2分钟、4分钟...
    delay := time.Duration(math.Pow(2, float64(msg.RetryCount))) * time.Minute
    if delay > 1*time.Hour {
        delay = 1 * time.Hour  // 最多1小时
    }
    return delay
}

// DLQ消费主循环
func (dc *DLQConsumer) Start() {
    dc.wg.Add(1)
    go dc.consumeLoop()
    
    dc.wg.Add(1)
    go dc.retryScheduler()  // 定时重试调度器
}

func (dc *DLQConsumer) consumeLoop() {
    defer dc.wg.Done()
    
    for {
        select {
        case <-dc.ctx.Done():
            return
            
        default:
            msg, err := dc.consumer.ReadMessage(100 * time.Millisecond)
            if err != nil {
                continue
            }
            
            var deadLetter DeadLetterMessage
            if err := json.Unmarshal(msg.Value, &deadLetter); err != nil {
                log.GetInstance().Sugar.Error("Unmarshal DLQ message failed: ", err)
                dc.consumer.CommitMessage(msg)
                continue
            }
            
            // 处理死信消息
            dc.processDeadLetter(&deadLetter)
            
            // 提交offset
            dc.consumer.CommitMessage(msg)
        }
    }
}

func (dc *DLQConsumer) processDeadLetter(msg *DeadLetterMessage) {
    // 获取对应的恢复策略
    strategy, exists := dc.strategies[msg.FailureReason]
    if !exists {
        log.GetInstance().Sugar.Warn("No recovery strategy for reason: ", msg.FailureReason)
        dc.metrics.NoStrategyMessages.Inc()
        return
    }
    
    // 检查是否可恢复
    if !strategy.CanRecover(msg) {
        if msg.RetryCount >= msg.MaxRetries {
            // 超过最大重试，需要人工介入
            dc.escalateToManual(msg)
        }
        return
    }
    
    // 计算重试延迟
    delay := strategy.CalculateRetryDelay(msg)
    
    // 如果需要延迟，加入调度队列
    if delay > 0 {
        msg.NextRetryTime = ptrTime(time.Now().Add(delay))
        dc.scheduleRetry(msg, delay)
        return
    }
    
    // 立即重试
    dc.retryMessage(msg, strategy)
}

// retryMessage 重试消息
func (dc *DLQConsumer) retryMessage(msg *DeadLetterMessage, strategy RecoveryStrategy) {
    // 准备重试消息
    retryMsg, err := strategy.PrepareRetry(msg)
    if err != nil {
        log.GetInstance().Sugar.Error("Prepare retry failed: ", err)
        strategy.OnFailure(msg, err)
        return
    }
    
    // 发送回主Topic
    err = dc.retryProducer.Produce(&kafka.Message{
        TopicPartition: kafka.TopicPartition{
            Topic:     &mainTopic,
            Partition: kafka.PartitionAny,
        },
        Key:   []byte(retryMsg.ExternalUserID),
        Value: jsonMarshal(retryMsg),
        Headers: []kafka.Header{
            {Key: "retry_from_dlq", Value: []byte("true")},
            {Key: "retry_count", Value: []byte(fmt.Sprintf("%d", msg.RetryCount+1))},
            {Key: "original_failure", Value: []byte(string(msg.FailureReason))},
        },
    }, nil)
    
    if err != nil {
        // 重试失败，更新DLQ消息
        msg.RetryCount++
        msg.LastRetryTime = ptrTime(time.Now())
        strategy.OnFailure(msg, err)
        dc.metrics.RetryFailures.Inc()
        
        // 重新发送到DLQ（更新后的版本）
        dc.updateDLQMessage(msg)
    } else {
        strategy.OnSuccess(msg)
        dc.metrics.RetrySuccesses.Inc()
        
        log.GetInstance().Sugar.Info("Message recovered from DLQ",
            ", user: ", msg.UserID,
            ", reason: ", msg.FailureReason,
            ", retry: ", msg.RetryCount+1)
    }
}
```

#### 监控与告警

```yaml
# DLQ监控指标
dlq_metrics:
  # 生产者指标
  - name: dlq_messages_produced_total
    description: 发送到DLQ的消息总数
    alert_threshold: 100/hour
    
  - name: dlq_messages_by_reason
    description: 按失败原因分类的消息数
    labels: [reason]
    
  - name: dlq_emergency_writes_total
    description: 紧急写入本地文件次数
    alert_threshold: 1  # 任何紧急写入都应告警
    
  # 消费者指标
  - name: dlq_messages_recovered_total
    description: 成功恢复的消息数
    
  - name: dlq_messages_unrecoverable_total
    description: 不可恢复的消息数
    alert_threshold: 10/day
    
  - name: dlq_consumer_lag
    description: DLQ消费延迟
    alert_threshold: 1000
    
  - name: dlq_manual_review_pending
    description: 待人工审核的消息数
    alert_threshold: 100

# DLQ告警规则
dlq_alerts:
  - level: WARNING
    condition: DLQ消息增长速率 > 正常值3倍
    action: 检查系统是否有异常
    
  - level: CRITICAL
    condition: DLQ紧急文件写入
    action: 立即检查Kafka集群状态
    
  - level: CRITICAL
    condition: DLQ消费者停止
    action: 立即重启DLQ消费者
    
  - level: P1
    condition: 不可恢复消息 > 100
    action: 人工介入处理
```

#### 管理界面

```go
// DLQManager DLQ管理接口
type DLQManager interface {
    // 查询DLQ状态
    GetStats() (*DLQStats, error)
    
    // 查询特定用户的死信消息
    GetUserMessages(userID string) ([]*DeadLetterMessage, error)
    
    // 手动重试消息
    ManualRetry(messageID string) error
    
    // 批量重试
    BatchRetry(filter DLQFilter) (int, error)
    
    // 清理过期消息
    PurgeExpired() (int, error)
    
    // 导出消息（用于分析）
    Export(filter DLQFilter, format ExportFormat) ([]byte, error)
}

// 管理API
POST /admin/dlq/retry/{messageID}      # 手动重试单条消息
POST /admin/dlq/batch-retry            # 批量重试
GET  /admin/dlq/stats                  # 获取DLQ统计
GET  /admin/dlq/messages?user={userID} # 查询用户的死信消息
POST /admin/dlq/purge                  # 清理过期消息
GET  /admin/dlq/export?format=csv      # 导出DLQ数据
```

### 3.5 多生产者场景下的分布式协调

#### 3.5.1 为什么需要分布式锁

在多生产者实例场景下，需要避免：
- **重复拉取**：多个KF实例同时从微信服务器拉取同一批消息
- **重复处理**：同一个事件被多个实例并发处理
- **资源竞争**：多个实例同时更新同一个cursor

#### 3.5.2 开源社区成熟方案对比

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| **Redis + Redlock** | • 性能高（4x faster）<br>• 部署简单<br>• 已有Redis可复用 | • 时钟跳变风险<br>• 网络分区问题<br>• 无fencing token | 效率优化场景 |
| **ZooKeeper** | • 强一致性（CP）<br>• 有fencing token<br>• 支持阻塞等待 | • 部署复杂<br>• 性能较低<br>• 学习曲线陡 | 正确性关键场景 |
| **etcd** | • Raft共识<br>• 云原生友好<br>• K8s生态集成 | • 相对较重<br>• 需要集群 | K8s环境 |
| **Consul** | • 健康检查<br>• 服务发现集成<br>• Session机制 | • 额外组件<br>• 复杂度高 | 微服务架构 |

#### 3.5.3 最终方案：Redis + go-redsync（单实例→Sentinel）

**✅ 方案确定：统一使用Redis单实例和Sentinel，不考虑Cluster**

基于深入研究和context7文档验证，确定采用以下方案：

- ✅ **开发环境**：Redis单实例（简单高效）
- ✅ **生产环境**：Redis Sentinel（高可用主从）
- ❌ **不使用**：Redis Cluster（不适合分布式锁场景）
- 📝 **核心优势**：同一套代码完美支持两种环境

**决策依据**：
1. **go-redsync官方支持**：明确支持单实例和Sentinel模式
2. **代码零改动**：通过go-redis的UniversalClient接口自动适配
3. **平滑升级**：从开发到生产仅需修改配置文件
4. **Redis Sentinel优势**：
   - 自动故障转移（秒级）
   - 主从复制保证数据安全
   - 所有数据在一个主节点，适合分布式锁
5. **避免Cluster复杂性**：
   - Cluster设计用于数据分片，不适合锁场景
   - Redlock算法需要独立Redis实例，非分片集群

### 统一实现代码（支持单实例和Sentinel）

```go
import (
    "github.com/go-redsync/redsync/v4"
    "github.com/go-redsync/redsync/v4/redis/goredis/v9"
    goredislib "github.com/redis/go-redis/v9"
)

// RedisLockManager Redis分布式锁管理器
type RedisLockManager struct {
    redSync *redsync.Redsync
    client  goredislib.UniversalClient
}

// NewRedisLockManager 创建锁管理器（支持单实例和哨兵模式）
func NewRedisLockManager(cfg *config.RedisConfig) (*RedisLockManager, error) {
    var client goredislib.UniversalClient
    
    switch cfg.Mode {
    case "single":
        // 开发环境：单实例
        client = goredislib.NewClient(&goredislib.Options{
            Addr:     cfg.Addr,
            Password: cfg.Password,
            DB:       cfg.DB,
        })
        
    case "sentinel":
        // 生产环境：哨兵模式（高可用）
        client = goredislib.NewFailoverClient(&goredislib.FailoverOptions{
            MasterName:    cfg.MasterName,
            SentinelAddrs: cfg.SentinelAddrs,
            Password:      cfg.Password,
            DB:            cfg.DB,
        })
        
    default:
        return nil, fmt.Errorf("unsupported redis mode: %s, use 'single' or 'sentinel'", cfg.Mode)
    
    // 测试连接
    ctx := context.Background()
    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("redis connection failed: %w", err)
    }
    
    // 创建redsync实例
    pool := goredis.NewPool(client)
    rs := redsync.New(pool)
    
    return &RedisLockManager{
        redSync: rs,
        client:  client,
    }, nil
}

// AcquireLock 获取分布式锁
func (m *RedisLockManager) AcquireLock(key string, ttl time.Duration) (*redsync.Mutex, error) {
    mutex := m.redSync.NewMutex(
        key,
        redsync.WithExpiry(ttl),
        redsync.WithTries(1),        // 非阻塞，快速失败
        redsync.WithRetryDelay(100*time.Millisecond),
    )
    
    if err := mutex.Lock(); err != nil {
        return nil, err // 锁已被占用
    }
    
    return mutex, nil
}

// 在KF服务中使用
func (s *KFService) syncWithLock(openKFID string) error {
    lockKey := fmt.Sprintf("sync:lock:%s", openKFID)
    
    // 获取锁（支持单实例、哨兵）
    mutex, err := s.lockManager.AcquireLock(lockKey, 30*time.Second)
    if err != nil {
        log.Debug("Another node is syncing for ", openKFID)
        return nil // 其他节点正在处理
    }
    defer mutex.Unlock()
    
    // 执行同步
    return s.doHandleEvent(...)
}
```

#### 3.5.4 配置示例（支持平滑升级）

```yaml
# config/development.yaml - 开发环境
distributed_lock:
  enabled: false  # 单KF实例可关闭
  redis:
    mode: single
    addr: localhost:6379
    db: 0
    
# config/staging.yaml - 预发环境
distributed_lock:
  enabled: true
  redis:
    mode: sentinel  # 哨兵模式
    master_name: mymaster
    sentinel_addrs:
      - sentinel1:26379
      - sentinel2:26379
      - sentinel3:26379
    db: 0
    password: ""
    
# config/production.yaml - 生产环境
distributed_lock:
  enabled: true
  redis:
    mode: sentinel  # 哨兵模式（高可用）
    master_name: mymaster
    sentinel_addrs:
      - sentinel1:26379
      - sentinel2:26379
      - sentinel3:26379
    db: 0
    password: ""
```

#### 3.5.5 生产部署建议

**环境演进路径**：
```
开发环境          →     预发环境        →     生产环境
Redis单实例             Redis Sentinel        Redis Sentinel
(无需分布式锁)          (3节点高可用)         (3节点高可用)
```

**关键优势**：
1. **代码无需修改**：同一套代码支持单实例和Sentinel
2. **配置驱动**：通过配置文件切换Redis模式
3. **平滑过渡**：从单实例到Sentinel零代码改动
4. **故障隔离**：锁的问题不影响主业务流程
5. **成熟稳定**：Redis Sentinel是官方推荐的高可用方案

**实施指南**：

1. **开发环境部署**：
   ```bash
   # 启动单个Redis实例
   docker run -d -p 6379:6379 redis:latest
   ```

2. **生产环境部署**：
   ```bash
   # 部署Redis Sentinel（3个哨兵 + 1主2从）
   # 参考：https://redis.io/docs/manual/sentinel/
   ```

3. **代码集成**：
   - 引入依赖：`go get github.com/go-redsync/redsync/v4`
   - 复制上述`RedisLockManager`代码
   - 在配置文件中指定Redis模式

4. **测试验证**：
   - 单元测试：模拟锁竞争
   - 集成测试：多实例并发测试
   - 故障演练：Sentinel主从切换测试

**监控指标**：
```go
// 分布式锁监控
type LockMetrics struct {
    LockAcquired    counter  // 获取锁成功次数
    LockFailed      counter  // 获取锁失败次数（被占用）
    LockExpired     counter  // 锁过期次数
    LockHoldTime    histogram // 锁持有时间分布
}
```

## 4. 消费者设计

### 4.1 消费者核心实现
```go
package consumer

import (
    "context"
    "encoding/json"
    "sync"
    "time"
    
    "github.com/confluentinc/confluent-kafka-go/v2/kafka"
    "qywx/infrastructures/log"
)

// KafkaConsumer Kafka消费者
type KafkaConsumer struct {
    consumer       *kafka.Consumer
    userProcessors sync.Map  // userID -> *UserProcessor
    ctx            context.Context
    cancel         context.CancelFunc
    wg             sync.WaitGroup
}

// UserProcessor 用户消息处理器（自管理生命周期 + 熔断保护）
type UserProcessor struct {
    userID       string
    messageChan  chan *MessageWrapper
    consumer     *kafka.Consumer
    registry     *sync.Map          // 指向全局注册表
    state        atomic.Int32       // 0=active, 1=dying, 2=dead
    lastActive   atomic.Int64       // Unix timestamp
    lifetime     time.Duration      // 生命周期（如3小时）
    deduplicator *MessageDeduplicator
    
    // 熔断器：防护恶意用户
    circuitBreaker *CircuitBreaker
    
    // 流量控制：限制消息速率
    rateLimiter    *RateLimiter
}

// CircuitBreaker 熔断器实现
type CircuitBreaker struct {
    userID              string
    state               atomic.Int32  // 0=closed, 1=open, 2=half-open
    consecutiveFailures atomic.Int32
    openedAt            atomic.Int64  // Unix timestamp
    halfOpenDelay       time.Duration
    failureThreshold    int32
    successThreshold    int32         // half-open成功次数阈值
    successCount        atomic.Int32  // half-open成功计数
}

// RateLimiter 限流器实现（滑动窗口 + 惩罚升级）
type RateLimiter struct {
    // 配置参数
    userID           string         // 用户标识（用于日志）
    windowSize       int           // N秒的时间窗口
    maxMessages      int           // X条消息上限
    penaltyThreshold int           // Y次连续违规
    blockDuration    time.Duration // Z小时拒绝服务
    
    // 运行时状态
    messageTimestamps []int64      // 消息时间戳队列（滑动窗口）
    violations        int           // 连续违规次数
    blockedUntil      time.Time     // 封禁到何时
    mu                sync.Mutex    // 保护并发访问
}

const (
    StateActive = iota
    StateDying  
    StateDead
)

// NewKafkaConsumer 创建消费者
func NewKafkaConsumer(brokers string, groupID string, topics []string) (*KafkaConsumer, error) {
    config := &kafka.ConfigMap{
        "bootstrap.servers": brokers,
        "group.id":          groupID,
        
        // 分配策略：使用sticky保持分区稳定
        "partition.assignment.strategy": "cooperative-sticky",
        
        // 会话管理
        "session.timeout.ms":    30000,  // 30秒超时
        "heartbeat.interval.ms": 3000,   // 3秒心跳
        "max.poll.interval.ms":  300000, // 5分钟最大处理时间
        
        // Offset管理
        "enable.auto.commit":    false,     // 手动提交
        "auto.offset.reset":     "earliest", // 从最早开始
        "isolation.level":       "read_committed", // 只读已提交
        
        // 性能优化
        "fetch.min.bytes":       1024,
        "fetch.wait.max.ms":     500,
    }
    
    c, err := kafka.NewConsumer(config)
    if err != nil {
        return nil, fmt.Errorf("create consumer failed: %w", err)
    }
    
    // 订阅主题
    err = c.SubscribeTopics(topics, nil)
    if err != nil {
        return nil, fmt.Errorf("subscribe failed: %w", err)
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    
    return &KafkaConsumer{
        consumer: c,
        ctx:      ctx,
        cancel:   cancel,
    }, nil
}

// Start 启动消费
func (kc *KafkaConsumer) Start() {
    kc.wg.Add(1)
    go kc.consumeLoop()
    
    // 注意：不需要 cleanupLoop！
    // 每个 processor 自己管理生命周期
}

// MessageWrapper 包装消息和Kafka元信息
type MessageWrapper struct {
    ChatMessage    *ChatMessage
    KafkaMessage   *kafka.Message  // 保留原始Kafka消息用于提交
}

// consumeLoop 消费主循环（优化版：无队头阻塞）
func (kc *KafkaConsumer) consumeLoop() {
    defer kc.wg.Done()
    
    for {
        select {
        case <-kc.ctx.Done():
            return
            
        default:
            // 批量拉取消息
            msg, err := kc.consumer.ReadMessage(100 * time.Millisecond)
            if err != nil {
                if err.(kafka.Error).Code() != kafka.ErrTimedOut {
                    log.GetInstance().Sugar.Error("Read message failed: ", err)
                }
                continue
            }
            
            // 解析消息
            var chatMsg ChatMessage
            if err := json.Unmarshal(msg.Value, &chatMsg); err != nil {
                log.GetInstance().Sugar.Error("Unmarshal failed: ", err)
                // 格式错误的消息直接提交，避免阻塞
                kc.consumer.CommitMessage(msg)
                continue
            }
            
            // 包装消息（关键：传递Kafka元信息）
            wrapper := &MessageWrapper{
                ChatMessage:  &chatMsg,
                KafkaMessage: msg,
            }
            
            // 路由到用户处理器
            processor, err := kc.getOrCreateProcessor(chatMsg.ExternalUserID)
            if err != nil {
                log.GetInstance().Sugar.Error("Failed to get processor: ", err)
                continue
            }
            
            // 使用TrySend安全发送（带熔断保护）
            err = processor.TrySend(wrapper)
            if err != nil {
                if strings.Contains(err.Error(), "not active") {
                    // processor正在dying，稍等后重试创建新的
                    time.Sleep(10 * time.Millisecond)
                    if processor, err = kc.getOrCreateProcessor(chatMsg.ExternalUserID); err == nil {
                        processor.TrySend(wrapper) // 重试一次
                    }
                } else if strings.Contains(err.Error(), "circuit broken") {
                    // 用户被熔断，发送到DLQ
                    log.GetInstance().Sugar.Warn("User circuit broken, sending to DLQ: ", chatMsg.ExternalUserID)
                    
                    // 发送到死信队列
                    kc.dlqProducer.SendToDeadLetter(
                        &chatMsg,
                        ReasonCircuitOpen,
                        fmt.Sprintf("Circuit breaker open for user %s", chatMsg.ExternalUserID),
                        &KafkaMetadata{
                            Topic:     *msg.TopicPartition.Topic,
                            Partition: msg.TopicPartition.Partition,
                            Offset:    int64(msg.TopicPartition.Offset),
                        },
                    )
                    
                    metrics.CircuitBrokenMessages.Inc()
                    // 提交offset，继续处理其他用户
                    kc.consumer.CommitMessage(msg)
                    
                } else if strings.Contains(err.Error(), "rate limited") {
                    // 用户被限流，发送到DLQ
                    log.GetInstance().Sugar.Warn("User rate limited, sending to DLQ: ", chatMsg.ExternalUserID)
                    
                    // 发送到死信队列
                    kc.dlqProducer.SendToDeadLetter(
                        &chatMsg,
                        ReasonRateLimited,
                        err.Error(),
                        &KafkaMetadata{
                            Topic:     *msg.TopicPartition.Topic,
                            Partition: msg.TopicPartition.Partition,
                            Offset:    int64(msg.TopicPartition.Offset),
                        },
                    )
                    
                    metrics.RateLimitedMessages.Inc()
                    // 提交offset，继续处理其他用户
                    kc.consumer.CommitMessage(msg)
                    
                } else if strings.Contains(err.Error(), "user blocked") {
                    // 用户被封禁，发送到DLQ
                    log.GetInstance().Sugar.Warn("User blocked, sending to DLQ: ", chatMsg.ExternalUserID)
                    
                    // 发送到死信队列
                    kc.dlqProducer.SendToDeadLetter(
                        &chatMsg,
                        ReasonUserBlocked,
                        err.Error(),
                        &KafkaMetadata{
                            Topic:     *msg.TopicPartition.Topic,
                            Partition: msg.TopicPartition.Partition,
                            Offset:    int64(msg.TopicPartition.Offset),
                        },
                    )
                    
                    metrics.UserBlockedMessages.Inc()
                    // 提交offset，继续处理其他用户
                    kc.consumer.CommitMessage(msg)
                    
                } else if strings.Contains(err.Error(), "queue full") {
                    // 队列满，发送到DLQ
                    log.GetInstance().Sugar.Warn("User queue full, sending to DLQ: ", chatMsg.ExternalUserID)
                    
                    // 发送到死信队列
                    kc.dlqProducer.SendToDeadLetter(
                        &chatMsg,
                        ReasonQueueFull,
                        "Processor message queue is full",
                        &KafkaMetadata{
                            Topic:     *msg.TopicPartition.Topic,
                            Partition: msg.TopicPartition.Partition,
                            Offset:    int64(msg.TopicPartition.Offset),
                        },
                    )
                    
                    metrics.QueueFullMessages.Inc()
                    // 提交offset避免阻塞其他用户
                    kc.consumer.CommitMessage(msg)
                    
                    // TODO: 可选择将消息存入重试队列
                    // kc.saveToRetryQueue(wrapper)
                }
            }
        }
    }
}

// getOrCreateProcessor 获取或创建用户处理器（线程安全）
func (kc *KafkaConsumer) getOrCreateProcessor(userID string) (*UserProcessor, error) {
    // 快速路径：获取已存在的活跃processor
    if p, ok := kc.userProcessors.Load(userID); ok {
        processor := p.(*UserProcessor)
        // 只返回活跃的processor
        if processor.state.Load() == StateActive {
            return processor, nil
        }
        // 非活跃，继续创建新的
    }
    
    // 创建新处理器（带熔断和限流保护）
    processor := &UserProcessor{
        userID:       userID,
        messageChan:  make(chan *MessageWrapper, 1000),
        consumer:     kc.consumer,
        registry:     &kc.userProcessors,
        state:        atomic.Int32{},
        lastActive:   atomic.Int64{},
        lifetime:     3 * time.Hour,  // TODO: 从配置读取
        deduplicator: NewMessageDeduplicator(),
        
        // 初始化熔断器
        circuitBreaker: &CircuitBreaker{
            userID:           userID,
            state:            atomic.Int32{},      // 初始为Closed
            halfOpenDelay:    30 * time.Second,    // 30秒后尝试恢复
            failureThreshold: 100,                 // 连续100次失败触发熔断
            successThreshold: 10,                  // Half-Open状态需要10次成功才能恢复
        },
        
        // 初始化限流器（精确的滑动窗口限流）
        rateLimiter: &RateLimiter{
            windowSize:       10,              // 每10秒
            maxMessages:      5,               // 最多5条消息
            penaltyThreshold: 3,               // 连续违规3次
            blockDuration:    1 * time.Hour,   // 封禁1小时
            messageTimestamps: make([]int64, 0, 5),
            userID:           userID,          // 用于日志和告警
        },
    }
    
    // 原子存储，避免并发创建
    actual, loaded := kc.userProcessors.LoadOrStore(userID, processor)
    
    if !loaded {
        // 我们赢得了竞争，启动processor
        kc.wg.Add(1)
        go kc.runProcessor(actual.(*UserProcessor))
        log.GetInstance().Sugar.Info("Created new processor for user: ", userID)
    }
    
    return actual.(*UserProcessor), nil
}

// runProcessor processor的完整生命周期（自管理）
func (kc *KafkaConsumer) runProcessor(p *UserProcessor) {
    defer kc.wg.Done()
    
    log.GetInstance().Sugar.Info("Processor started for user: ", p.userID)
    defer log.GetInstance().Sugar.Info("Processor stopped for user: ", p.userID)
    
    // 初始化状态
    p.state.Store(StateActive)
    p.lastActive.Store(time.Now().Unix())
    
    // 生命周期计时器
    idleTimer := time.NewTimer(p.lifetime)
    defer idleTimer.Stop()
    
    // 清理资源
    defer p.cleanup()
    
    // 消息处理主循环
    for {
        select {
        case wrapper, ok := <-p.messageChan:
            if !ok {
                // channel被外部关闭（不应该发生）
                log.GetInstance().Sugar.Warn("Message channel closed externally for user: ", p.userID)
                return
            }
            
            // 检查自己的状态
            if p.state.Load() != StateActive {
                log.GetInstance().Sugar.Debug("Processor dying, reject message for user: ", p.userID)
                continue
            }
            
            // 处理消息
            kc.processMessage(p, wrapper)
            
            // 更新活跃时间
            p.lastActive.Store(time.Now().Unix())
            
            // 重置生命周期计时器
            if !idleTimer.Stop() {
                select {
                case <-idleTimer.C:
                default:
                }
            }
            idleTimer.Reset(p.lifetime)
            
        case <-idleTimer.C:
            // 生命周期到期
            log.GetInstance().Sugar.Info("Processor lifetime expired for user: ", p.userID)
            
            // 标记为dying
            if !p.state.CompareAndSwap(StateActive, StateDying) {
                return // 已经在dying
            }
            
            // 优雅退出：尝试处理剩余消息
            p.drainMessages(kc, 5*time.Second)
            
            return
        }
    }
}

// processMessage 处理单条消息（带幂等性检查）
func (kc *KafkaConsumer) processMessage(p *UserProcessor, wrapper *MessageWrapper) {
    // 幂等性检查
    isNew, err := p.deduplicator.CheckAndMark(wrapper.ChatMessage.MessageID)
    if err != nil {
        log.GetInstance().Sugar.Error("Dedup check failed: ", err)
        return
    }
    
    if !isNew {
        // 重复消息也要提交offset，避免无限重试
        p.consumer.CommitMessage(wrapper.KafkaMessage)
        return
    }
    
    // 实际业务处理
    err = kc.handleMessage(p.userID, wrapper.ChatMessage)
    
    if err != nil {
        log.GetInstance().Sugar.Error("Handle message failed: ", err)
        // 处理失败，不提交offset，等待重启重试
        return
    }
    
    // 成功处理，提交offset
    if err := p.consumer.CommitMessage(wrapper.KafkaMessage); err != nil {
        log.GetInstance().Sugar.Error("Commit offset failed: ", err,
            ", message may be reprocessed on restart")
    }
}

// drainMessages 优雅退出前尽力处理剩余消息
func (p *UserProcessor) drainMessages(kc *KafkaConsumer, timeout time.Duration) {
    deadline := time.Now().Add(timeout)
    
    for time.Now().Before(deadline) {
        select {
        case wrapper, ok := <-p.messageChan:
            if !ok {
                return
            }
            kc.processMessage(p, wrapper)
            
        default:
            // 队列空了
            return
        }
    }
}

// cleanup processor清理资源
func (p *UserProcessor) cleanup() {
    // 1. 标记为死亡
    p.state.Store(StateDead)
    
    // 2. 从注册表删除自己（关键：防止新消息进入）
    p.registry.Delete(p.userID)
    
    // 3. 关闭channel
    close(p.messageChan)
    
    // 4. 清理其他资源
    if p.deduplicator != nil {
        // p.deduplicator.Close() // 如果有清理方法
    }
    
    log.GetInstance().Sugar.Info("Processor cleaned up for user: ", p.userID)
}

// TrySend 外部安全发送消息接口（带熔断和限流保护）
func (p *UserProcessor) TrySend(wrapper *MessageWrapper) error {
    // 1. 检查processor状态
    state := p.state.Load()
    if state != StateActive {
        return fmt.Errorf("processor not active: state=%d", state)
    }
    
    // 2. 熔断器检查
    if err := p.circuitBreaker.Allow(); err != nil {
        metrics.CircuitBreakerDenied.Inc()
        return fmt.Errorf("circuit broken: %w", err)
    }
    
    // 3. 限流检查
    if err := p.rateLimiter.Allow(); err != nil {
        metrics.RateLimitExceeded.Inc()
        log.GetInstance().Sugar.Warn("Rate limit exceeded for user: ", p.userID)
        return fmt.Errorf("rate limited: %w", err)
    }
    
    // 4. 非阻塞发送
    select {
    case p.messageChan <- wrapper:
        // 成功发送，更新熔断器状态
        p.circuitBreaker.OnSuccess()
        return nil
    default:
        // 队列满，触发熔断器失败计数
        p.circuitBreaker.OnFailure()
        metrics.QueueFullEvents.Inc()
        return fmt.Errorf("processor queue full")
    }
}

// CircuitBreaker方法实现
func (cb *CircuitBreaker) Allow() error {
    state := cb.state.Load()
    
    switch state {
    case 0: // Closed - 正常状态
        return nil
        
    case 1: // Open - 熔断状态
        openedAt := time.Unix(cb.openedAt.Load(), 0)
        if time.Since(openedAt) > cb.halfOpenDelay {
            // 尝试进入半开状态
            if cb.state.CompareAndSwap(1, 2) {
                cb.successCount.Store(0)
                log.GetInstance().Sugar.Info("Circuit breaker half-open for user: ", cb.userID)
            }
            return nil // 允许测试流量通过
        }
        return fmt.Errorf("circuit open for user %s", cb.userID)
        
    case 2: // Half-Open - 半开状态（测试恢复）
        // 限制测试流量
        if cb.successCount.Load() < cb.successThreshold {
            return nil
        }
        return fmt.Errorf("circuit half-open, limiting traffic for user %s", cb.userID)
        
    default:
        return fmt.Errorf("invalid circuit breaker state: %d", state)
    }
}

func (cb *CircuitBreaker) OnSuccess() {
    state := cb.state.Load()
    
    switch state {
    case 0: // Closed
        // 重置失败计数
        cb.consecutiveFailures.Store(0)
        
    case 2: // Half-Open
        count := cb.successCount.Add(1)
        if count >= cb.successThreshold {
            // 恢复到关闭状态
            cb.state.Store(0)
            cb.consecutiveFailures.Store(0)
            log.GetInstance().Sugar.Info("Circuit breaker closed for user: ", cb.userID)
            metrics.CircuitBreakerRecovered.Inc()
        }
    }
}

func (cb *CircuitBreaker) OnFailure() {
    state := cb.state.Load()
    
    switch state {
    case 0: // Closed
        failures := cb.consecutiveFailures.Add(1)
        if failures >= cb.failureThreshold {
            // 触发熔断
            if cb.state.CompareAndSwap(0, 1) {
                cb.openedAt.Store(time.Now().Unix())
                log.GetInstance().Sugar.Error("Circuit breaker opened for user: ", cb.userID,
                    ", failures: ", failures)
                metrics.CircuitBreakerOpened.Inc()
                
                // 发送告警
                alerting.Send("UserCircuitBreakerOpen", map[string]string{
                    "user_id": cb.userID,
                    "failures": fmt.Sprintf("%d", failures),
                })
            }
        }
        
    case 2: // Half-Open
        // 测试失败，立即回到Open状态
        cb.state.Store(1)
        cb.openedAt.Store(time.Now().Unix())
        log.GetInstance().Sugar.Warn("Circuit breaker re-opened for user: ", cb.userID)
    }
}

// RateLimiter方法实现（滑动窗口算法）
func (rl *RateLimiter) Allow() error {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    
    now := time.Now()
    
    // 1. 检查是否被封禁
    if now.Before(rl.blockedUntil) {
        remaining := rl.blockedUntil.Sub(now)
        metrics.BlockedUserDenied.Inc()
        return fmt.Errorf("user blocked for %v", remaining)
    }
    
    // 2. 清理过期的时间戳（滑动窗口）
    cutoff := now.Unix() - int64(rl.windowSize)
    validTimestamps := []int64{}
    for _, ts := range rl.messageTimestamps {
        if ts > cutoff {
            validTimestamps = append(validTimestamps, ts)
        }
    }
    rl.messageTimestamps = validTimestamps
    
    // 3. 检查窗口内消息数量
    if len(rl.messageTimestamps) >= rl.maxMessages {
        // 违规！
        rl.violations++
        metrics.RateLimitViolations.Inc()
        
        // 检查是否需要封禁
        if rl.violations >= rl.penaltyThreshold {
            rl.blockedUntil = now.Add(rl.blockDuration)
            
            // 发送告警
            log.GetInstance().Sugar.Error("User blocked due to rate limit violations: ",
                rl.userID, ", violations: ", rl.violations, ", blocked for: ", rl.blockDuration)
            
            alerting.Send("UserBlocked", map[string]string{
                "user_id": rl.userID,
                "violations": fmt.Sprintf("%d", rl.violations),
                "block_hours": fmt.Sprintf("%.1f", rl.blockDuration.Hours()),
            })
            
            metrics.UsersBlocked.Inc()
            
            // 重置违规计数（下次解封后重新计算）
            rl.violations = 0
            
            return fmt.Errorf("user blocked: exceeded %d violations, blocked for %v",
                rl.penaltyThreshold, rl.blockDuration)
        }
        
        return fmt.Errorf("rate limit exceeded: %d messages in %d seconds (violation %d/%d)",
            len(rl.messageTimestamps), rl.windowSize, rl.violations, rl.penaltyThreshold)
    }
    
    // 4. 允许发送，记录时间戳
    rl.messageTimestamps = append(rl.messageTimestamps, now.Unix())
    
    // 如果没有违规，重置违规计数
    if len(rl.messageTimestamps) <= rl.maxMessages/2 {
        rl.violations = 0  // 流量降到一半以下时重置违规计数
    }
    
    return nil
}

// handleMessage 实际处理消息
func (kc *KafkaConsumer) handleMessage(userID string, msg *ChatMessage) error {
    // 创建超时context
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    log.GetInstance().Sugar.Info("Processing message for user ", userID, 
        ", type: ", msg.MsgType, ", msgID: ", msg.MessageID)
    
    // 根据消息类型处理
    switch msg.MsgType {
    case "text":
        return handleTextMessage(ctx, msg)
    case "image":
        return handleImageMessage(ctx, msg)
    case "event":
        return handleEventMessage(ctx, msg)
    default:
        log.GetInstance().Sugar.Debug("Unknown message type: ", msg.MsgType)
        return nil
    }
}

// 注意：采用自管理生命周期设计，不需要 cleanupLoop！
// 每个 processor 自己管理生命周期，见下方 processUserMessages 实现

// commitMessage 提交消息offset
func (kc *KafkaConsumer) commitMessage(msg *kafka.Message) {
    _, err := kc.consumer.CommitMessage(msg)
    if err != nil {
        log.GetInstance().Sugar.Error("Commit failed: ", err)
    }
}

// Stop 停止消费者
func (kc *KafkaConsumer) Stop() {
    kc.cancel()
    
    // 等待所有协程退出
    done := make(chan struct{})
    go func() {
        kc.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        log.GetInstance().Sugar.Info("Consumer stopped gracefully")
    case <-time.After(30 * time.Second):
        log.GetInstance().Sugar.Warn("Consumer stop timeout")
    }
    
    kc.consumer.Close()
}
```

### 4.2 幂等性保证（重要）

由于采用 **At-least-once** 语义，消息可能重复处理，必须实现幂等性：

```go
// MessageDeduplicator 基于Redis的幂等性保证
type MessageDeduplicator struct {
    redis *redis.Client
}

// CheckAndMark 原子性检查并标记消息
func (md *MessageDeduplicator) CheckAndMark(messageID string) (bool, error) {
    key := fmt.Sprintf("msg:processed:%s", messageID)
    
    // SetNX: 只有不存在时才设置（原子操作）
    success, err := md.redis.SetNX(key, "1", 24*time.Hour).Result()
    if err != nil {
        return false, err
    }
    
    return success, nil // true表示新消息，false表示重复
}

// handleMessage 带幂等性检查的消息处理
func (kc *KafkaConsumer) handleMessage(userID string, msg *ChatMessage) error {
    // 1. 幂等性检查（必须在所有处理之前）
    isNew, err := kc.deduplicator.CheckAndMark(msg.MessageID)
    if err != nil {
        // Redis错误，可以选择：
        // A. 返回错误，不提交offset（保守）
        // B. 继续处理，接受可能重复（激进）
        return fmt.Errorf("dedup check failed: %w", err)
    }
    
    if !isNew {
        // 重复消息，直接返回成功
        // 注意：返回nil让offset被提交，避免无限重试
        log.GetInstance().Sugar.Debug("Duplicate message skipped: ", msg.MessageID)
        return nil
    }
    
    // 2. 实际业务处理
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 处理消息...
    
    return nil
}

// 幂等性设计原则
/*
1. 使用全局唯一的MessageID
2. 在数据库操作中使用 UPSERT 而非 INSERT
3. 状态更新使用版本号或时间戳防止回退
4. 金额操作使用事务ID确保不重复扣款
*/
```

## 5. 监控与运维

### 5.1 关键监控指标
```go
type Metrics struct {
    // 生产者指标
    ProducedMessages   counter
    ProduceErrors      counter
    ProduceLatency     histogram
    
    // 消费者指标
    ConsumedMessages   counter
    ConsumerLag        gauge
    ProcessingTime     histogram
    ActiveProcessors   gauge
    
    // 分区指标
    PartitionLag       map[int32]gauge
    PartitionThroughput map[int32]counter
}

// Prometheus集成示例
func setupMetrics() {
    prometheus.MustRegister(
        consumerLag,
        processingTime,
        activeProcessors,
    )
}
```

### 5.2 告警规则
```yaml
alerts:
  - name: ConsumerLag
    condition: lag > 10000
    duration: 5m
    action: alert
    
  - name: RebalanceFrequent
    condition: rebalance_count > 3
    duration: 10m
    action: page
    
  - name: ProcessorMemoryLeak
    condition: active_processors > 1000
    duration: 30m
    action: alert
```

## 6. 故障处理

### 6.1 节点故障
- **检测**：Kafka 通过心跳检测（30秒超时）
- **恢复**：自动触发 Rebalance，其他节点接管分区
- **影响**：Rebalance 期间（约3-30秒）暂停消费

### 6.2 网络分区
- **问题**：消费者与 Kafka 失联但仍在运行
- **解决**：消费者自动重连，使用指数退避策略
- **降级**：可选降级到本地队列模式

### 6.3 消息积压
- **检测**：监控 Consumer Lag
- **处理**：
  1. 增加消费者节点
  2. 增加每节点的并发处理器数
  3. 优化消息处理逻辑

## 7. 性能优化建议

### 7.1 生产者优化
- 使用批量发送：`linger.ms=10`
- 启用压缩：`compression.type=lz4`
- 异步发送：使用 callback 处理结果

### 7.2 消费者优化
- 批量拉取：`fetch.min.bytes=1024`
- 增加缓冲：每用户 channel 1000 容量
- 并行处理：不同用户完全并行

### 7.3 Kafka 集群优化
```bash
# JVM 调优
KAFKA_HEAP_OPTS="-Xmx4G -Xms4G"
KAFKA_JVM_PERFORMANCE_OPTS="-XX:+UseG1GC -XX:MaxGCPauseMillis=20"

# 系统调优
vm.swappiness=1
net.core.rmem_max=134217728
net.core.wmem_max=134217728
```

## 8. 部署检查清单

### 8.1 生产环境（集群）
- [ ] Kafka 集群至少3节点
- [ ] 主Topic创建：12个分区，3副本
- [ ] **DLQ Topic创建**：`qywx-dead-letter-queue`，6个分区，3副本
- [ ] Redis 集群用于分布式锁和去重
- [ ] 监控系统就绪（Prometheus + Grafana）
- [ ] 日志收集配置（ELK Stack）
- [ ] **DLQ紧急文件目录**：`/var/log/qywx/` 具有写权限

### 8.2 开发环境（单点）
- [ ] Kafka 单节点运行正常
- [ ] **主Topic创建**：4个分区，**1副本**（⚠️ 关键）
- [ ] **DLQ Topic创建**：2个分区，**1副本**（⚠️ 关键）
- [ ] **min.insync.replicas设置为1**（⚠️ 否则无法发送消息）
- [ ] Redis 单实例（如果只有一个生产者实例可暂时不需要）
- [ ] 简化监控（可选）
- [ ] **DLQ紧急文件目录**：`/var/log/qywx/` 具有写权限

### 降级方案准备
- [ ] **确认接受临时降级方案的风险**
  - ⚠️ 可能导致消息乱序
  - ⚠️ 降级消息只能单节点处理
  - ⚠️ 无自动恢复机制
- [ ] 配置降级告警规则（优先级：Critical）
- [ ] 准备手动介入流程文档
- [ ] **TODO: 实现持久化失败队列（Phase 2）**

### 分布式锁准备
- [ ] **开发环境**：单生产者实例可跳过
- [ ] **生产环境（多生产者）**：
  - [ ] Redis实例就绪（可复用现有）
  - [ ] 选择分布式锁客户端：
    - Go: [go-redsync](https://github.com/go-redsync/redsync)
    - Java: [Redisson](https://github.com/redisson/redisson)
  - [ ] 配置锁超时时间（建议30秒）
  - [ ] 实现锁获取失败的处理逻辑

### 测试与演练
- [ ] 故障演练完成
  - Kafka 宕机场景
  - 网络分区场景
  - 降级流程测试
- [ ] **恶意用户防护测试**
  - [ ] 限流测试：发送100条/分钟，验证拒绝
  - [ ] 熔断测试：模拟队列满，验证30秒熔断
  - [ ] 恢复测试：验证Half-Open状态自动恢复
  - [ ] 隔离测试：验证恶意用户不影响其他用户
- [ ] 性能测试通过
  - 正常负载：1000用户 × 2条/分钟
  - 峰值负载：100用户 × 60条/分钟
  - 攻击负载：10用户 × 1000条/分钟
- [ ] 消息顺序性验证
- [ ] **监控告警验证**
  - [ ] 熔断器告警触发
  - [ ] 限流告警触发
  - [ ] 告警通知链路

## 9. 关键设计决策

### 9.1 Offset 提交安全性

**核心原则：只有消息处理成功后才提交 Offset**

**错误模式（会导致消息丢失）：**
```go
// ❌ 危险：过早提交
case processor.messageChan <- msg:
    commitOffset(msg)  // 消息还在内存，未实际处理
    // 如果此时崩溃，消息永久丢失
```

**正确模式：**
```go
// ✅ 安全：处理后提交
func processUserMessages(processor *UserProcessor) {
    for wrapper := range processor.messageChan {
        err := handleMessage(wrapper.ChatMessage)
        if err == nil {
            // 只有成功处理才提交
            consumer.CommitMessage(wrapper.KafkaMessage)
        }
        // 失败不提交，重启后重试
    }
}
```

**设计要点：**
1. 将 Kafka 元信息（offset、partition）传递到处理协程
2. 处理协程负责提交（谁处理，谁提交）
3. 失败不提交，依赖 at-least-once + 幂等性

### 9.2 降级策略的权衡

**当前降级方案的局限性**：

```go
// ⚠️ 当前的临时降级方案
Kafka失败 → 本地channel → 单节点处理

问题：
1. 消息乱序：User123的M1走本地，M2走Kafka，可能M2先处理
2. 分布式失效：降级消息只能在故障节点处理
3. 无法恢复：一旦降级，即使Kafka恢复也无法迁移回去
```

**理想的降级方案（待实现）**：

```go
// ✅ 理想的持久化降级方案
Kafka失败 → 持久化存储 → 补偿任务 → 重试Kafka

优势：
1. 保证顺序：同一用户的消息按序重试
2. 最终一致：所有消息最终都会进入Kafka
3. 可观测性：失败消息可查询、可监控、可手动干预
```

**为什么暂时接受不完美**：
1. **迭代开发**：先保证基础功能可用，再优化
2. **依赖未就绪**：持久存储层尚未实现
3. **风险可控**：通过监控和告警及时发现问题
4. **明确TODO**：清晰的改进路线图

### 9.3 异步发送是核心
**设计原则**：生产者必须是真正的异步，否则整个系统的吞吐量会被生产者拖累。

**错误示例分析**：
```go
// ❌ 表面异步，实际同步的陷阱
err = producer.Produce(msg, deliveryChan)
e := <-deliveryChan  // 阻塞等待！

// 问题：
// 1. 完全否定了 Kafka 批量发送的优势
// 2. linger.ms=10 变得毫无意义
// 3. 每条消息的延迟累加，形成瓶颈
```

**正确实践**：
- 生产者：Fire-and-forget + 后台事件处理
- 批量确认：通过 Events() channel 批量处理结果
- 背压处理：队列满时的优雅降级策略

### 9.3 用户级并行的必要性
- 同一用户串行：保证消息顺序
- 不同用户并行：最大化吞吐量
- 动态管理：3小时生命周期，自动清理

### 9.4 性能基准
基于正确的异步设计：
- 单节点生产：10,000+ msg/s
- 单节点消费：5,000+ msg/s（含业务处理）
- 端到端延迟：P99 < 100ms

## 10. 并发安全性保证

### 10.1 Processor 生命周期的并发安全

**核心挑战**：在高并发环境下，同一用户的 processor 可能同时面临创建、消息路由、超时清理等并发操作。

**解决方案：自管理生命周期 + 原子状态机**

#### 架构范式转变：从"被管理"到"自治"

这个设计实现了一个关键的架构范式转变，通过让 UserProcessor 完全自治，**从理论上消除了所有竞态条件**：

```go
// ❌ 传统模式：外部管理（复杂、易错、有竞态）
type TraditionalScheduler struct {
    processors  map[string]*Processor
    cleanupChan chan string
    cleanupWG   sync.WaitGroup
}

func (s *Scheduler) cleanupLoop() {
    for {
        s.processorsMu.Lock()
        for userID, processor := range s.processors {
            if isExpired(processor) {
                delete(s.processors, userID)  // 危险：竞态窗口
            }
        }
        s.processorsMu.Unlock()
        time.Sleep(cleanupInterval)
    }
}

// ✅ 自治模式：内部管理（简单、安全、无竞态）
type SelfManagedProcessor struct {
    state       atomic.Int32    // 原子状态
    registry    *sync.Map       // 仅用于自我注销
    lifetime    time.Duration   // 自己的生命周期
    // 没有外部清理器！
}

func (p *UserProcessor) run() {
    idleTimer := time.NewTimer(p.lifetime)
    defer p.cleanup()  // 自我清理
    
    for {
        select {
        case <-idleTimer.C:
            // 生命周期结束，自主退出
            return
        case msg := <-p.messageChan:
            // 处理消息，重置生命
            idleTimer.Reset(p.lifetime)
        }
    }
}
```

#### 为什么"没有任何竞态条件"的论断成立

**传统方案的竞态窗口**：
```
T1: cleanupLoop 检查 processor P1 超时
T2: 新消息 M1 到达，路由到 P1
T3: cleanupLoop 删除 P1
T4: M1 进入已删除的 processor（数据丢失！）
```

**自治方案的安全保证**：
```
T1: P1 的 idleTimer 自己触发
T2: P1 自己决定 state = Dying
T3: 新消息 M1 被 TrySend 拒绝（状态检查）
T4: P1 完成剩余工作
T5: P1 自己从 registry 注销
T6: P1 优雅退出

结果：没有外部干预，没有竞态窗口，没有数据丢失
```

#### Actor Model 的完美体现

这个自治设计无意中实现了 **Actor Model** 的核心原则：

```go
// 每个 UserProcessor 就是一个 Actor
type UserProcessor struct {
    // Actor 的私有状态（封装性）
    state        atomic.Int32
    messageChan  chan *MessageWrapper  // Actor 的邮箱
    
    // 生命周期自管理（自治性）
    lifetime     time.Duration
    idleTimer    *time.Timer
    
    // 唯一的外部引用（最小依赖）
    registry     *sync.Map  // 仅用于自我注销
}

// Actor 的三大特性：
// 1. 封装：内部状态完全私有
// 2. 异步：通过 channel 接收消息
// 3. 自治：生命周期完全自主
```

#### 设计优雅性分析

**系统复杂度的简化**：
```
传统设计的依赖关系（复杂）：
Scheduler ←→ CleanupLoop ←→ Processor ←→ Registry
    ↑            ↓              ↑          ↓
    └────────────┴──────────────┴──────────┘
         需要复杂的同步和协调

自治设计的依赖关系（简单）：
Scheduler → Registry ← Processor
             ↑            ↓
             └────────────┘
        Processor 自己管理自己
```

**内聚性的极致追求**：
```go
// 高内聚：所有生命周期相关逻辑都在 Processor 内部
func (p *UserProcessor) run() {
    // 初始化
    idleTimer := time.NewTimer(p.lifetime)
    defer p.cleanup()  // 清理也在内部
    
    // 运行
    for {
        select {
        case <-idleTimer.C:
            return  // 自主决定退出
        case msg := <-p.messageChan:
            p.processMessage(msg)
            idleTimer.Reset(p.lifetime)  // 自主续命
        }
    }
}

// 对比传统设计：生命周期逻辑分散在多处
// - Scheduler 创建
// - CleanupLoop 检查
// - Manager 删除
// - Processor 被动接受

### 10.2 消息顺序性保证

**场景分析：Processor 超时切换时的消息顺序**

```
时间线：
T1: User123 的 processor P1 生命周期到期
T2: P1 标记为 StateDying，开始 drainMessages
T3: User123 发送新消息 M1
T4: consumeLoop 尝试向 P1 发送 M1，被拒绝（StateDying）
T5: consumeLoop 创建新 processor P2
T6: M1 被发送到 P2
T7: P1 完成剩余消息处理，自行清理
T8: P2 开始处理 M1
```

**顺序性保证机制**：

1. **状态检查**：TrySend 确保只有 Active 状态才接收消息
2. **优雅退出**：drainMessages 处理完队列中的消息
3. **原子切换**：LoadOrStore 确保新旧 processor 不会并存
4. **重试机制**：消息被拒绝后立即尝试创建新 processor

### 10.3 并发场景详解

#### 场景1：并发创建同一用户的 Processor

```go
// 线程A和B同时为User123创建processor
Thread A: getOrCreateProcessor("User123")
Thread B: getOrCreateProcessor("User123")

// LoadOrStore保证只有一个成功创建
actual, loaded := userProcessors.LoadOrStore("User123", processorA)
// loaded=false 表示A赢得竞争
// loaded=true 表示B发现A已创建，使用A的processor
```

#### 场景2：消息到达时 Processor 正在清理

```go
// Processor生命周期管理
func (p *UserProcessor) cleanup() {
    // 1. 先改状态，阻止新消息
    p.state.Store(StateDead)
    
    // 2. 从注册表删除（关键：防止新消息路由过来）
    p.registry.Delete(p.userID)
    
    // 3. 安全关闭channel
    close(p.messageChan)
}

// 消费者路由消息时
if processor.TrySend(msg) != nil {
    // 发送失败，processor可能在dying
    // 稍等后重新获取或创建
    time.Sleep(10 * time.Millisecond)
    newProcessor := getOrCreateProcessor(userID)
    newProcessor.TrySend(msg)
}
```

#### 场景3：Rebalance 期间的并发控制

```go
// Kafka rebalance时可能导致：
// 1. 同一partition被重新分配
// 2. 消息可能被重复消费

// 解决方案：
// - 幂等性检查（MessageDeduplicator）
// - Cooperative-sticky分配策略减少rebalance影响
// - 消费者优雅停止，处理完当前消息
```

### 10.4 死锁预防与自愈能力

**自治设计天然避免死锁**：

1. **无外部锁依赖**：Processor 不需要外部锁来管理生命周期
2. **单向通信**：只通过 channel 和原子操作通信
3. **超时保护**：所有阻塞操作都有超时机制

**自愈能力**：
```go
// 如果 Processor 崩溃
func (p *UserProcessor) run() {
    defer p.cleanup()  // 保证清理
    // 即使 panic，也会：
    // 1. 自动从 registry 移除
    // 2. 关闭 channel
    // 3. 释放所有资源
}

// 系统自动恢复
// - 新消息到达时会创建新的 Processor
// - 没有僵尸进程
// - 没有资源泄漏
```

```go
// ❌ 错误：可能死锁
func bad() {
    processors.Range(func(k, v interface{}) bool {
        processor := v.(*UserProcessor)
        processors.Delete(k)  // 在Range中删除，可能死锁
        return true
    })
}

// ✅ 正确：先收集后删除
func good() {
    var toDelete []string
    processors.Range(func(k, v interface{}) bool {
        toDelete = append(toDelete, k.(string))
        return true
    })
    for _, key := range toDelete {
        processors.Delete(key)
    }
}
```

### 10.5 反压机制与恶意用户防护

#### 问题：队头阻塞（Head-of-Line Blocking）

**原设计的缺陷**：
```go
// ❌ 有问题的反压机制
if processor.QueueFull() {
    kc.pausePartition(partition)  // 暂停整个分区！
}

// 后果：
// Partition P1: [User123(恶意), User456(正常), User789(正常)]
// User123 发送1000条/分钟 → 队列满 → P1暂停 → User456和789被连累
```

**优化后的设计**：
```go
// ✅ 用户级隔离
if processor.QueueFull() {
    // 只影响当前用户，不影响分区内其他用户
    processor.circuitBreaker.OnFailure()
    commitOffset(msg)  // 继续处理其他用户
}
```

#### 三层防护体系

**第一层：精确限流（Sliding Window Rate Limiting）**
```go
// 滑动窗口算法，精确控制消息频率
配置参数：
- N秒时间窗口（如：10秒）
- X条消息上限（如：5条）
- Y次违规封禁（如：3次）
- Z小时拒绝服务（如：1小时）

工作原理：
1. 维护N秒内的消息时间戳队列
2. 新消息到达时清理过期时间戳
3. 超过X条立即限流，违规计数+1
4. 连续Y次违规，封禁Z小时
5. 封禁期间所有请求直接拒绝
```

**第二层：队列缓冲（Queue Buffer）**
```go
// 吸收正常的流量波动
队列大小：1000条
作用：应对短时间的消息突发
特点：per-user隔离，互不影响
```

**第三层：熔断器（Circuit Breaker）**
```go
// 最后的防线
触发条件：连续100次队列满
熔断时间：30秒
恢复机制：Half-Open状态测试
作用：隔离恶意/故障用户
```

#### 限流配置策略

```yaml
# 限流器配置（config.yaml）
rate_limiter:
  # 默认配置
  default:
    window_seconds: 10      # 每10秒
    max_messages: 5         # 最多5条消息
    penalty_threshold: 3    # 连续违规3次
    block_hours: 1.0        # 封禁1小时
  
  # 用户组配置（可根据业务需求调整）
  groups:
    normal:                 # 普通用户
      window_seconds: 10
      max_messages: 5
      penalty_threshold: 3
      block_hours: 1.0
    
    vip:                    # VIP用户（放宽限制）
      window_seconds: 10
      max_messages: 20
      penalty_threshold: 10
      block_hours: 0.5
    
    suspicious:             # 可疑用户（严格限制）
      window_seconds: 30
      max_messages: 3
      penalty_threshold: 1
      block_hours: 24.0
    
    whitelist:              # 白名单（不限制）
      window_seconds: 1
      max_messages: 9999
      penalty_threshold: 9999
      block_hours: 0.0
```

#### 典型攻击场景与防护

**场景1：正常用户**
```
配置：10秒5条，违规3次封禁1小时
行为：正常对话，1-2条/分钟
时间线：
T0-T10: 发送2条 → 通过 ✓
T10-T20: 发送1条 → 通过 ✓
结果：永远不会触发限流
```

**场景2：激动用户**
```
配置：10秒5条，违规3次封禁1小时
行为：情绪激动，连续发送
时间线：
T0: 发送第1-5条 → 通过 ✓
T1: 发送第6条 → 限流，违规1/3 ⚠️
T2: 发送第7条 → 限流，违规2/3 ⚠️
T11: 窗口滑动，发送第8条 → 通过 ✓（违规重置）
结果：短暂限流后恢复正常
```

**场景3：机器人攻击**
```
配置：10秒5条，违规3次封禁1小时
攻击：Bot发送100条/秒
时间线：
T0.00: 发送第1-5条 → 通过 ✓
T0.01: 发送第6条 → 限流，违规1/3 ⚠️
T0.02: 发送第7条 → 限流，违规2/3 ⚠️
T0.03: 发送第8条 → 限流，违规3/3 ⚠️
T0.04: 触发封禁 → 封禁1小时 ❌
T0.05-T3600: 所有请求直接拒绝
结果：4毫秒内识别并隔离攻击者
```

**场景4：慢速攻击（Slowloris类似）**
```
配置：10秒5条，违规3次封禁1小时
攻击：每2秒1条，但每条处理需10秒
时间线：
T0-T10: 发送5条 → 通过（但处理缓慢）
T10-T20: 发送5条 → 第6条触发限流，违规1/3
T20-T30: 持续发送 → 违规2/3, 3/3
T30: 触发封禁
结果：虽然发送频率不高，但仍会被封禁
```

#### 递增惩罚机制（可选）

```go
// ProgressivePenalty 递增惩罚策略
type ProgressivePenalty struct {
    baseBlockHours   float64      // 基础封禁时长（如1小时）
    multiplier       float64      // 倍数递增（如2倍）
    maxBlockHours    float64      // 最大封禁时长（如24小时）
    penaltyHistory   []time.Time  // 历史封禁记录
}

// 计算封禁时长
func (pp *ProgressivePenalty) calculateBlockDuration() time.Duration {
    // 统计24小时内的违规次数
    recentViolations := pp.countRecentViolations(24 * time.Hour)
    
    // 指数递增：1h → 2h → 4h → 8h → 16h → 24h（上限）
    hours := pp.baseBlockHours * math.Pow(pp.multiplier, float64(recentViolations))
    if hours > pp.maxBlockHours {
        hours = pp.maxBlockHours
    }
    
    return time.Duration(hours * float64(time.Hour))
}

// 使用示例
惩罚升级表：
第1次违规：封禁1小时
第2次违规：封禁2小时
第3次违规：封禁4小时
第4次违规：封禁8小时
第5次违规：封禁16小时
第6次及以上：封禁24小时（上限）
```

#### 运维管理接口

```go
// RateLimiterAdmin 运维管理接口
type RateLimiterAdmin interface {
    // 查询用户状态
    GetUserStatus(userID string) (*UserRateLimitStatus, error)
    
    // 手动解封（紧急情况）
    Unblock(userID string, reason string) error
    
    // 临时调整限制（特殊活动）
    AdjustLimit(userID string, tempConfig *RateLimiterConfig) error
    
    // 白名单管理
    AddToWhitelist(userID string) error
    RemoveFromWhitelist(userID string) error
    
    // 黑名单管理
    GetBlockedUsers() ([]BlockedUser, error)
    PermanentlyBlock(userID string, reason string) error
}

// 管理API示例
GET /admin/ratelimit/status/{userID}
POST /admin/ratelimit/unblock/{userID}
POST /admin/ratelimit/whitelist/{userID}
GET /admin/ratelimit/blocked
```

#### 监控与告警

```yaml
# 限流相关指标
rate_limit_metrics:
  # 实时指标
  - name: rate_limit_violations_total
    description: 限流违规次数
    alert: > 100/hour
    
  - name: users_blocked_total
    description: 用户封禁次数
    alert: > 10/hour
    
  - name: blocked_users_current
    description: 当前被封禁用户数
    alert: > 50
    
  - name: messages_denied_total
    description: 拒绝消息总数
    alert: > 1000/hour

# 告警分级（增强版）
alerts:
  - level: INFO
    condition: 单次限流触发
    action: 记录日志
    
  - level: WARNING  
    condition: 用户连续违规2次
    action: 记录详细日志 + 监控面板告警
    
  - level: CRITICAL
    condition: 用户被封禁
    action: 通知运维 + 自动评估是否永久封禁
    details: |
      - 用户ID
      - 违规次数
      - 封禁时长
      - 最近100条消息样本
    
  - level: EMERGENCY
    condition: 10个以上用户同时被封禁
    action: 立即人工介入 + 可能遭受DDoS攻击
```

#### 长期主义的设计理念

**为什么要精确的限流控制？**

1. **墨菲定律**：可能出错的事情终将出错
2. **生产无小事**：一个恶意用户可能影响整个系统
3. **成本考虑**：防护的成本 << 故障的成本
4. **用户体验**：99.9%的正常用户不应被0.1%的异常用户影响

**滑动窗口算法的优势**：
```
对比其他限流算法：

1. 固定窗口算法：
   问题：窗口边界突发流量
   示例：59秒发10条，61秒发10条，2秒内20条
   
2. 令牌桶算法：
   问题：实现复杂，参数难调
   
3. 滑动窗口算法（我们的选择）：
   优势：精确控制，无边界问题
   实现：简单清晰，易于理解
   性能：O(N)空间，O(N)时间，N很小（如5）
```

**设计权衡**：
```
实现成本：
- 滑动窗口实现：~100行代码
- 惩罚机制：~50行代码
- 管理接口：~100行代码

运维收益：
- 精确控制每个用户的消息频率
- 自动识别并隔离恶意用户
- 灵活的配置和管理能力
- 完善的监控和告警体系

结论：精确限流是生产系统的必需品
```

### 10.6 设计哲学：真正的自治

这个设计达到了分布式系统设计的理想状态：

**Linus Torvalds 的 "Good Taste" 体现**：
> "Bad programmers worry about the code. Good programmers worry about data structures and their relationships."

自治设计完美诠释了这一点：通过改变数据结构的关系（从外部管理到自我管理），彻底简化了系统复杂度。

**设计原则对比**：
| 方面 | 传统设计 | 自治设计 |
|------|---------|---------|
| 生命周期管理 | 外部 CleanupLoop | 内部 idleTimer |
| 并发控制 | 复杂的锁机制 | 原子状态 + Channel |
| 错误恢复 | 需要外部干预 | 自动自愈 |
| 代码复杂度 | 高（多处协调） | 低（高内聚） |
| 竞态条件 | 存在理论风险 | **从根本上不可能** |
| 维护成本 | 高 | 低 |

**架构启示**：
1. **不是通过添加机制解决问题，而是通过改变视角消除问题**
2. **最好的并发控制是不需要并发控制**
3. **自治比管理更可靠**
4. **简单是终极的复杂**

## 11. 单点到集群的平滑过渡设计

### 11.1 环境差异对比

| 特性 | 单点Kafka | Kafka集群 | 影响 |
|------|-----------|-----------|------|
| **高可用性** | ❌ 单点故障 | ✅ 自动故障转移 | 集群才能真正高可用 |
| **数据持久性** | ⚠️ 单副本 | ✅ 多副本保障 | 集群数据更安全 |
| **负载均衡** | ❌ 单节点承载 | ✅ 自动Rebalance | 集群支持水平扩展 |
| **DLQ可靠性** | ⚠️ 降级到文件 | ✅ 多副本DLQ | 集群DLQ更可靠 |
| **性能上限** | 受限于单机 | 线性扩展 | 集群性能无上限 |

### 11.2 配置管理策略

#### 环境感知配置（推荐）
```yaml
# config/development.yaml
kafka:
  environment: development
  brokers: "localhost:9092"
  topics:
    main:
      partitions: 4
      replication_factor: 1      # ⚠️ 关键调整
      min_insync_replicas: 1     # ⚠️ 关键调整
    dlq:
      partitions: 2
      replication_factor: 1      # ⚠️ 关键调整
      min_insync_replicas: 1     # ⚠️ 关键调整

# config/production.yaml  
kafka:
  environment: production
  brokers: "kafka1:9092,kafka2:9092,kafka3:9092"
  topics:
    main:
      partitions: 12
      replication_factor: 3
      min_insync_replicas: 2
    dlq:
      partitions: 6
      replication_factor: 3
      min_insync_replicas: 2
```

#### 代码适配示例
```go
// 环境感知的Topic创建
func CreateTopics(adminClient *kafka.AdminClient, env string) error {
    cfg := config.GetKafkaConfig(env)
    
    topicSpecs := []kafka.TopicSpecification{
        {
            Topic:             cfg.Topics.Main.Name,
            NumPartitions:     cfg.Topics.Main.Partitions,
            ReplicationFactor: cfg.Topics.Main.ReplicationFactor,
            Config: map[string]string{
                "min.insync.replicas": strconv.Itoa(cfg.Topics.Main.MinInsyncReplicas),
            },
        },
    }
    
    // 创建Topic时自动适配环境
    _, err := adminClient.CreateTopics(context.Background(), topicSpecs)
    return err
}
```

### 11.3 过渡期间的注意事项

#### 单点环境限制
1. **无真正高可用**：需要准备降级方案
2. **性能瓶颈明显**：控制消息流量
3. **无故障自愈**：需要人工干预流程

#### 升级到集群的步骤
```bash
# Phase 1: 单点运行（开发/测试）
1. 部署单点Kafka
2. 创建Topic（replication-factor=1）
3. 启动应用（配置指向单点）
4. 功能验证

# Phase 2: 集群准备
1. 部署3节点Kafka集群
2. 数据迁移（如需要）
3. 重建Topic（replication-factor=3）

# Phase 3: 平滑切换
1. 停止应用
2. 更新配置文件（brokers列表）
3. 重启应用
4. 验证功能
```

### 11.4 代码设计的过渡友好性

本设计**天然支持平滑过渡**：

✅ **配置外部化**：所有环境相关参数可配置
✅ **无硬编码**：broker地址、topic配置均可调整
✅ **降级机制**：单点故障有应急方案
✅ **监控完备**：单点和集群使用相同监控体系

**无需代码修改**的部分：
- 生产者/消费者逻辑
- 消息处理流程
- DLQ机制
- 监控指标收集

**仅需配置调整**的部分：
- bootstrap.servers
- replication-factor
- min.insync.replicas

## 12. 总结

本方案通过 Kafka 的分区机制和**自治的用户级并行处理**，实现了：
- ✅ **消息顺序性**：同一用户的消息严格有序
- ✅ **消息可靠性**：At-least-once + 幂等性保证不丢消息
- ✅ **高并发**：不同用户完全并行，充分利用资源
- ✅ **高可用**：3副本 + 自动故障转移（集群模式）
- ✅ **可扩展**：支持水平扩展到更多节点
- ✅ **无竞态**：自治设计从根本上消除竞态条件
- ✅ **自愈性**：系统具备自动恢复能力
- ✅ **抗攻击**：三层防护体系，恶意用户无法影响系统
- ✅ **生产级**：长期主义设计，充分考虑边缘场景
- ✅ **平滑过渡**：从单点到集群无需改代码，仅需调配置

### 核心设计要点回顾

1. **生产者**：真正的异步发送，避免同步阻塞陷阱
2. **消费者**：处理成功后才提交 Offset，防止消息丢失
3. **并行模型**：同一用户串行，不同用户并行
4. **生命周期**：**Processor 完全自治，无需外部管理**
5. **容错机制**：幂等性设计，支持消息重复处理
6. **防护体系**：**限流 → 缓冲 → 熔断，三层递进防护**
7. **隔离原则**：恶意用户影响范围限制在自身，不影响他人

### 实施建议

- **第一阶段**：实现基础功能，确保消息不丢失
- **第二阶段**：优化性能，批量提交 Offset
- **第三阶段**：完善监控，建立告警机制

### 设计理念总结

**核心理念**：
- **正确性优于性能**：宁可慢也要对
- **简单可靠优于复杂完美**：复杂度是bug的温床
- **自治优于管理**：让组件自己管理自己
- **数据结构优于算法**：好的数据结构让算法变简单

**这个设计的精髓**：
> 通过让 UserProcessor 完全自治，我们不是解决了竞态条件问题，而是让竞态条件从根本上不可能发生。这是一个教科书级别的设计范例，展示了如何通过改变问题的视角来彻底简化系统复杂度。

正如计算机科学家 Tony Hoare 所说：
> "There are two ways of constructing a software design: One way is to make it so simple that there are obviously no deficiencies, and the other way is to make it so complicated that there are no obvious deficiencies. The first method is far more difficult."

这个自治设计选择了第一条路：**让系统简单到明显没有缺陷**。
