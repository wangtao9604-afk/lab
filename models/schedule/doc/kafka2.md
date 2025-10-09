# 企业微信客服消息系统 Kafka 集成方案（Jenny架构重构版）

## 变更摘要（Jenny架构重构）

**🎯 核心修正**：消除原设计中的**致命错误**和**过度复杂性**，回归Kafka使用的本质。

### 🔴 修复的致命问题
1. **分区内连续提交问题**：引入`PartitionCommitGate`，解决"高offset先提交跳过低offset"的丢消息问题
2. **配置键错误**：使用`librdkafka`正确的配置键（`message.send.max.retries`而非`retries`）
3. **分区器不一致**：统一使用`murmur2_random`保证跨语言分区一致性

### 🟡 简化的架构复杂性
1. **消除特殊情况处理**：废除复杂的降级逻辑、多层熔断器、UserProcessor状态机
2. **DLQ极简化**：从复杂的恢复策略系统简化为标准的死信队列
3. **配置统一**：基于`confluent-kafka-go`仓库的标准实践

### 🟢 采用的最佳实践
1. **数据结构优先**：`PartitionCommitGate`作为核心数据结构解决所有提交问题
2. **实用主义设计**：直接使用`kmq`包装层，避免过度抽象
3. **零破坏性迁移**：保持API兼容，只修改内部实现
4. **三级降级保护**：Kafka → DLQ → MySQL，确保消息永不丢失（MySQL部分TODO）

---

## 1. 架构概述

### 1.1 核心设计原则（Jenny式修正）
- **消息顺序性保证**：同一用户的消息必须按时间顺序处理（分区内连续提交保证）
- **分区级并行处理**：不同用户并行，同分区内安全并发+连续提交
- **容错与高可用**：支持节点故障自动恢复，**真正的**不丢消息（非理论上的）
- **水平扩展**：支持动态增减节点，`cooperative-sticky`分区分配
- **平滑过渡**：基于标准`confluent-kafka-go`实现，配置兼容单点/集群

### 1.2 Jenny式简化架构
```
微信回调 → kmq.Producer → Kafka Topic(12 Partitions) → kmq.Consumer + PartitionCommitGate
             ↓                        ↓                              ↓
      统一murmur2分区        按UserID Hash分区           分区内连续提交闸门
             ↓                        ↓                              ↓
        DLQ兜底处理              消息不丢失                  业务处理器并发
```

**架构简化要点**：
- **移除**：UserProcessor状态管理、多层熔断器、复杂降级逻辑
- **保留**：核心业务价值 + Kafka可靠性保证
- **核心**：`PartitionCommitGate`解决连续提交，其他一切从简

---

## 2. Kafka Topic 设计

### 2.1 Topic 配置（配置修正版）

#### 生产环境配置（集群）
```yaml
topic:
  name: qywx-chat-messages
  partitions: 12          # 3节点 × 4，支持水平扩展
  replication-factor: 3   # 3副本，真正的高可用
  min.insync.replicas: 2  # 最少同步副本数，acks=all时生效
  retention.ms: 604800000 # 7天保留期
  compression.type: lz4   # 平衡压缩率和CPU开销
  max.message.bytes: 1048576  # 最大消息1MB
```

#### 开发环境配置（单点）
```yaml
# ✅ 单点Kafka的正确配置
topic:
  name: qywx-chat-messages
  partitions: 4           # 单节点建议1-4个分区，便于开发调试
  replication-factor: 1   # ⚠️ 必须为1（单点只有1个broker）
  min.insync.replicas: 1  # ⚠️ 必须为1（否则acks=all无法工作）
  retention.ms: 604800000 # 7天保留期
  compression.type: lz4   # 压缩算法
  max.message.bytes: 1048576  # 最大消息1MB

# DLQ Topic（使用相同配置保证一致性）
dlq_topic:
  name: qywx-dead-letter-queue
  partitions: 4           # 与主Topic保持一致，便于回流
  replication-factor: 1   # ⚠️ 必须为1
  min.insync.replicas: 1  # ⚠️ 必须为1
```

### 2.2 分区策略（统一修正）
- **分区键**：使用 `external_user_id` 作为 Partition Key
- **分区器**：统一使用 `murmur2_random`（与Java客户端对齐）
- **分区分配**：12个分区均匀分配给消费节点，支持`cooperative-sticky`动态rebalance
- **DLQ一致性**：DLQ使用相同的分区器和key，保证回流消息命中原分区

---

## 3. 生产者设计（基于kmq包装）

### 3.1 Jenny式简化生产者

**设计思路**：直接使用项目中的`kmq.Producer`，避免重复造轮子。

```go
package producer

import (
    "encoding/json"
    "time"
    "qywx/infrastructures/mq/kmq"
    "qywx/infrastructures/wxmsg/kefu"
)

// MessageProducer 基于kmq的消息生产者
type MessageProducer struct {
    producer *kmq.Producer
    dlq      *kmq.DLQ
    topic    string
}

// NewMessageProducer 创建生产者实例
func NewMessageProducer(brokers, topic string) (*MessageProducer, error) {
    // 使用kmq.Producer，配置已经优化（librdkafka正确配置键）
    producer, err := kmq.NewProducer(brokers, "qywx-message-producer")
    if err != nil {
        return nil, fmt.Errorf("create producer failed: %w", err)
    }

    // 创建DLQ（使用相同分区器）
    dlq, err := kmq.NewDLQ(brokers, "qywx-message-dlq", "qywx-dead-letter-queue")
    if err != nil {
        producer.Close()
        return nil, fmt.Errorf("create DLQ failed: %w", err)
    }

    return &MessageProducer{
        producer: producer,
        dlq:      dlq,
        topic:    topic,
    }, nil
}

// ProduceMessage 发送消息（Jenny式简化版）
func (mp *MessageProducer) ProduceMessage(msg *kefu.KFRecvMessage) error {
    // 构建标准消息格式
    chatMsg := &ChatMessage{
        MessageID:      generateMessageID(),
        ExternalUserID: msg.ExternalUserID,
        OpenKFID:       msg.OpenKFID,
        MsgType:        msg.MsgType,
        Content:        msg,
        Timestamp:      time.Now().Unix(),
    }

    value, err := json.Marshal(chatMsg)
    if err != nil {
        return fmt.Errorf("marshal message failed: %w", err)
    }

    headers := []kafka.Header{
        {Key: "msg_type", Value: []byte(msg.MsgType)},
        {Key: "msg_id", Value: []byte(chatMsg.MessageID)},
        {Key: "user_id", Value: []byte(msg.ExternalUserID)},
    }

    // 发送到Kafka（异步）
    key := []byte(msg.ExternalUserID)  // 分区键
    err = mp.producer.Produce(mp.topic, key, value, headers)
    if err != nil {
        // 发送失败，尝试DLQ兜底
        if dlqErr := mp.dlq.Send(key, chatMsg, headers); dlqErr != nil {
            // DLQ也失败，尝试MySQL兜底（最后的保险）
            // TODO: 实现MySQL降级存储（当数据库引入后实现）
            // if mysqlErr := mp.persistToMySQL(chatMsg); mysqlErr != nil {
            //     return fmt.Errorf("all failed: produce=%w, dlq=%w, mysql=%w", err, dlqErr, mysqlErr)
            // }
            // log.Warn("Message persisted to MySQL fallback", "user_id", msg.ExternalUserID)
            // return nil  // MySQL成功，返回成功（隐藏Kafka故障）

            return fmt.Errorf("both produce and DLQ failed: produce=%w, dlq=%w", err, dlqErr)
        }
        return fmt.Errorf("produce failed, sent to DLQ: %w", err)
    }

    return nil
}

// Close 优雅关闭
func (mp *MessageProducer) Close() {
    mp.producer.Flush(10000)  // 等待10秒发送完成
    mp.producer.Close()
    mp.dlq.Close()            // DLQ内部已经实现了Flush逻辑
}
```


### 3.2 配置详解（librdkafka正确键名）

**重要修正**：原设计使用Java客户端配置键，在`librdkafka`下无效！

```go
// kmq/producer.go 中的正确配置（已实现）
cfg := &kafka.ConfigMap{
    "bootstrap.servers":          brokers,
    "client.id":                  clientID,

    // 可靠性配置（使用librdkafka正确键名）
    "acks":                       "all",
    "enable.idempotence":         true,
    "message.send.max.retries":   10,     // ✅ 正确，不是"retries"
    "message.timeout.ms":         30000,  // ✅ 正确，不是"request.timeout.ms"

    // 批处理配置（librdkafka键名）
    "linger.ms":                  10,
    "batch.num.messages":         1000,   // ✅ 正确，不是"batch.size"
    "queue.buffering.max.ms":     50,
    "queue.buffering.max.kbytes": 102400,

    // 压缩和连接
    "compression.type":           "lz4",
    "socket.keepalive.enable":    true,
    "connections.max.idle.ms":    300000,

    // 主题级配置（关键修正）
    "default.topic.config": kafka.ConfigMap{
        "partitioner": "murmur2_random",  // ✅ 与Java对齐的分区器
    },
}
```

**配置对比表**：
| 功能 | ❌ 原设计（Java键名） | ✅ 修正版（librdkafka键名） |
|------|---------------------|--------------------------|
| 重试次数 | `retries: 10` | `message.send.max.retries: 10` |
| 批量大小 | `batch.size: 16384` | `batch.num.messages: 1000` |
| 缓冲时间 | `linger.ms: 10` | `linger.ms: 10` ✓ |
| 超时设置 | `request.timeout.ms` | `message.timeout.ms: 30000` |

### 3.3 集成到KF模块（零破坏性修改）

```go
// 在 kf.go 中的集成（保持原有API）
func (s *KFService) doHandleEvent(event *kefu.KFCallbackMessage, cm *cursorManager) error {
    // ... 前面的同步逻辑保持不变 ...

    // 获取消息后，发送到Kafka（修改后的简洁实现）
    for _, msg := range resp.MsgList {
        msgCopy := msg

        // Jenny式简化：直接发送，失败自动DLQ
        if err := s.messageProducer.ProduceMessage(&msgCopy); err != nil {
            log.GetInstance().Sugar.Error("Message produce failed: ", err)
            // 不需要复杂的降级逻辑，kmq已经处理了DLQ兜底
        }
    }

    // ... cursor更新逻辑 ...
}
```

**简化要点**：
- **移除**：复杂的降级到本地channel逻辑
- **移除**：多重熔断器和状态管理
- **保留**：核心业务逻辑不变
- **增强**：可靠性通过正确的Kafka配置保证

### 3.4 MySQL兜底方案设计（TODO：待数据库引入后实现）

**🛡️ Jenny式兜底设计：三级降级保护**

#### 为什么需要MySQL兜底？

**现实场景分析**：
```
正常路径：Producer → Kafka → Consumer ✅
降级路径1：Producer → DLQ → 人工处理 ⚠️
降级路径2：Producer → MySQL → 补偿任务 → Kafka 🛡️（最后的保险）
```

**核心价值**：
1. **消息永不丢失**：即使Kafka集群完全故障，消息仍然保存在MySQL
2. **自动恢复**：Kafka恢复后，补偿任务自动回放MySQL中的消息
3. **顺序性保证**：按用户分组，保证同用户消息顺序

#### 数据库表结构设计（TODO）

```sql
-- 消息降级存储表
CREATE TABLE `kafka_fallback_messages` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(64) NOT NULL COMMENT '消息唯一ID',
    `user_id` VARCHAR(128) NOT NULL COMMENT '用户ID（分区键）',
    `open_kf_id` VARCHAR(128) NOT NULL COMMENT '客服账号',
    `msg_type` VARCHAR(32) NOT NULL COMMENT '消息类型',
    `content` JSON NOT NULL COMMENT '消息内容（JSON格式）',
    `status` ENUM('pending', 'retrying', 'success', 'failed') DEFAULT 'pending',
    `retry_count` INT DEFAULT 0 COMMENT '重试次数',
    `max_retries` INT DEFAULT 10 COMMENT '最大重试次数',
    `error_message` TEXT COMMENT '最后错误信息',
    `created_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    `updated_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `next_retry_at` TIMESTAMP NULL COMMENT '下次重试时间',

    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_message_id` (`message_id`),
    KEY `idx_user_id_status` (`user_id`, `status`, `created_at`),
    KEY `idx_next_retry` (`status`, `next_retry_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Kafka降级消息存储表';

-- 分区优化（按月分区，便于归档）
ALTER TABLE `kafka_fallback_messages`
PARTITION BY RANGE (UNIX_TIMESTAMP(`created_at`)) (
    PARTITION p202501 VALUES LESS THAN (UNIX_TIMESTAMP('2025-02-01')),
    PARTITION p202502 VALUES LESS THAN (UNIX_TIMESTAMP('2025-03-01')),
    -- ... 更多分区
);
```

#### 实现策略（TODO）

```go
// persistToMySQL 将消息持久化到MySQL（最后的兜底）
func (mp *MessageProducer) persistToMySQL(msg *ChatMessage) error {
    // TODO: 实现MySQL持久化逻辑
    // 1. 获取数据库连接（使用连接池）
    // 2. 插入消息到kafka_fallback_messages表
    // 3. 记录监控指标
    // 4. 返回成功/失败

    // 伪代码示例：
    /*
    query := `
        INSERT INTO kafka_fallback_messages
        (message_id, user_id, open_kf_id, msg_type, content, status, next_retry_at)
        VALUES (?, ?, ?, ?, ?, 'pending', DATE_ADD(NOW(), INTERVAL 1 MINUTE))
    `

    _, err := db.Exec(query,
        msg.MessageID,
        msg.ExternalUserID,
        msg.OpenKFID,
        msg.MsgType,
        jsonContent,
    )

    if err != nil {
        return fmt.Errorf("persist to MySQL failed: %w", err)
    }

    mp.metrics.MySQLFallbacks.Inc()
    return nil
    */

    return fmt.Errorf("MySQL persistence not yet implemented")
}
```

#### 补偿任务设计（TODO）

```go
// MessageCompensator 消息补偿器（独立服务/协程）
type MessageCompensator struct {
    db       *sql.DB
    producer *kmq.Producer
    interval time.Duration
}

// Start 启动补偿任务
func (mc *MessageCompensator) Start(ctx context.Context) {
    ticker := time.NewTicker(mc.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            mc.compensatePendingMessages()
        }
    }
}

// compensatePendingMessages 补偿待发送消息
func (mc *MessageCompensator) compensatePendingMessages() {
    // TODO: 实现补偿逻辑
    // 1. 查询status='pending'或'retrying'且next_retry_at <= NOW()的消息
    // 2. 按user_id分组，保证同用户消息顺序
    // 3. 尝试发送到Kafka
    // 4. 成功：更新status='success'
    // 5. 失败：更新retry_count和next_retry_at（指数退避）
    // 6. 超过max_retries：标记为'failed'，人工介入
}
```

#### 监控指标（TODO）

```go
type MySQLFallbackMetrics struct {
    MessagesStored     counter   // 存储到MySQL的消息数
    CompensateSuccess  counter   // 补偿成功数
    CompensateFailed   counter   // 补偿失败数
    PendingMessages    gauge     // 待补偿消息数
    OldestPendingAge   gauge     // 最老待补偿消息年龄（秒）
}

// 告警规则
alerts:
  - name: MySQLFallbackActive
    condition: mysql_fallback_rate > 0
    duration: 1m
    severity: warning
    message: "Kafka故障，消息降级到MySQL"

  - name: CompensationBacklog
    condition: pending_messages > 1000
    duration: 5m
    severity: critical
    message: "MySQL补偿积压过多，检查Kafka状态"
```

#### Jenny架构思维体现

**数据结构优先**：
- 设计合理的表结构，支持高效查询和补偿
- 按用户分组保证顺序性

**消除特殊情况**：
- 统一的降级链：Kafka → DLQ → MySQL
- 每一级都是标准的处理流程，没有特殊分支

**实用主义**：
- 当前标记为TODO，不过度设计
- 等数据库真正引入后再实现
- 保持架构的扩展性

**零破坏性**：
- MySQL降级对上层透明
- 补偿任务自动恢复，不需要人工干预
- 保持消息的最终一致性

---

## 4. 消费者设计（PartitionCommitGate核心）

### 4.1 Jenny式消费者架构

**设计核心**：使用`kmq.Consumer`的`PartitionCommitGate`机制解决原设计的致命问题。

```go
package consumer

import (
    "context"
    "encoding/json"
    "time"
    "qywx/infrastructures/mq/kmq"
    "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// MessageConsumer 基于kmq的消息消费者
type MessageConsumer struct {
    consumer  *kmq.Consumer
    dlq       *kmq.DLQ
    processor MessageProcessor
    ctx       context.Context
    cancel    context.CancelFunc
}

// MessageProcessor 业务处理接口
type MessageProcessor interface {
    ProcessMessage(msg *ChatMessage) error
    GetUserID() string  // 用于路由和监控
}

// NewMessageConsumer 创建消费者
func NewMessageConsumer(brokers, groupID string, processor MessageProcessor) (*MessageConsumer, error) {
    // 使用kmq.Consumer（已配置cooperative-sticky + 手动提交）
    consumer, err := kmq.NewConsumer(brokers, groupID)
    if err != nil {
        return nil, fmt.Errorf("create consumer failed: %w", err)
    }

    // DLQ（统一分区器）
    dlq, err := kmq.NewDLQ(brokers, "qywx-consumer-dlq", "qywx-dead-letter-queue")
    if err != nil {
        consumer.Close()
        return nil, fmt.Errorf("create DLQ failed: %w", err)
    }

    ctx, cancel := context.WithCancel(context.Background())

    return &MessageConsumer{
        consumer:  consumer,
        dlq:       dlq,
        processor: processor,
        ctx:       ctx,
        cancel:    cancel,
    }, nil
}

// Start 启动消费（实现kmq.MessageHandler接口）
func (mc *MessageConsumer) Start(topics []string) error {
    if err := mc.consumer.Subscribe(topics); err != nil {
        return fmt.Errorf("subscribe failed: %w", err)
    }

    // 核心：实现kmq.MessageHandler接口
    handler := func(m *kafka.Message) (handled bool, err error) {
        return mc.handleMessage(m)
    }

    // 使用kmq的PollAndDispatch（包含PartitionCommitGate逻辑）
    go mc.consumer.PollAndDispatch(handler, 100)
    return nil
}

// handleMessage 实现业务处理逻辑
func (mc *MessageConsumer) handleMessage(m *kafka.Message) (handled bool, err error) {
    // 解析消息
    var chatMsg ChatMessage
    if err := json.Unmarshal(m.Value, &chatMsg); err != nil {
        // 解析失败，发送到DLQ
        if dlqErr := mc.dlq.Send(m.Key, m.Value, m.Headers); dlqErr != nil {
            return false, fmt.Errorf("both unmarshal and DLQ failed: %w", dlqErr)
        }
        return true, nil  // DLQ成功 = handled
    }

    // 业务处理
    if err := mc.processor.ProcessMessage(&chatMsg); err != nil {
        // 业务失败，发送到DLQ
        if dlqErr := mc.dlq.Send(m.Key, chatMsg, m.Headers); dlqErr != nil {
            return false, fmt.Errorf("both process and DLQ failed: %w", dlqErr)
        }
        return true, nil  // DLQ成功 = handled
    }

    return true, nil  // 处理成功 = handled
}

// Stop 优雅停机
func (mc *MessageConsumer) Stop() {
    mc.cancel()
    mc.consumer.Close()
    mc.dlq.Close()
}
```

### 4.2 PartitionCommitGate原理（解决致命问题）

**原设计的致命错误**：
```go
// ❌ 错误的提交方式（会丢消息）
consumer.CommitMessage(msg)  // 每条消息成功后立即提交
```

**问题分析**：同一分区内，如果offset=10的消息先完成并提交，offset=8的消息还在处理中，那么重启后会从offset=11开始消费，offset=8和9永远丢失！

**PartitionCommitGate解决方案**：
```go
// kmq/commit_gate.go 的核心逻辑
type PartitionCommitGate struct {
    nextCommit int64                // 下一个要提交的连续offset
    done       map[int64]struct{}  // 已完成的offset（成功或DLQ）
}

// 只有连续完成才推进提交
func (g *PartitionCommitGate) tryAdvance() int64 {
    advanced := int64(-1)
    for {
        if _, ok := g.done[g.nextCommit]; ok {  // 检查连续性
            delete(g.done, g.nextCommit)
            advanced = g.nextCommit
            g.nextCommit++  // 推进连续指针
            continue
        }
        break  // 不连续，停止推进
    }
    return advanced
}
```

**举例说明**：
```
分区消息: [8, 9, 10, 11, 12]
处理完成顺序: 10✅ → 8✅ → 12✅ → 9✅ → 11✅

PartitionCommitGate行为:
1. 10完成 → done={10}, nextCommit=8, 不推进（8未完成）
2. 8完成  → done={8,10}, nextCommit=8, 检查连续性:
   - 8完成✅ → nextCommit=9, advanced=8
   - 9未完成❌ → 停止，提交到offset=9
3. 12完成 → done={9,10,12}, 不推进（9未完成）
4. 9完成  → done={9,10,12}, nextCommit=9, 检查连续性:
   - 9完成✅ → nextCommit=10, advanced=9
   - 10完成✅ → nextCommit=11, advanced=10
   - 11未完成❌ → 停止，提交到offset=11
5. 11完成 → 最终提交到offset=13

结果：没有消息丢失，保证连续性！
```

### 4.3 消费者配置（cooperative-sticky）

```go
// kmq/consumer.go 中的配置（已实现）
cfg := &kafka.ConfigMap{
    "bootstrap.servers":               brokers,
    "group.id":                        groupID,

    // 分区分配策略（支持增量rebalance）
    "partition.assignment.strategy":    "cooperative-sticky",

    // 手动提交配置（配合PartitionCommitGate）
    "enable.auto.commit":               false,
    "enable.auto.offset.store":         false,

    // 会话管理
    "session.timeout.ms":               30000,
    "heartbeat.interval.ms":            3000,
    "max.poll.interval.ms":             300000,

    // 拉取配置
    "fetch.min.bytes":                  1024,
    "fetch.wait.max.ms":                500,

    // 移除无意义的配置
    // "isolation.level": "read_committed",  // ❌ 未启用事务时无效
}
```

### 4.4 Rebalance安全处理

```go
// kmq/consumer.go 中的rebalance回调（已实现）
err := c.SubscribeTopics(topics, func(c *kafka.Consumer, e kafka.Event) error {
    switch ev := e.(type) {
    case kafka.AssignedPartitions:
        // 增量分配：为新分区创建PartitionCommitGate
        cc.gatesM.Lock()
        for _, tp := range ev.Partitions {
            start := int64(tp.Offset)
            if start < 0 { start = 0 }
            cc.gates[tp.Partition] = NewPartitionCommitGate(*tp.Topic, tp.Partition, start)
        }
        cc.gatesM.Unlock()
        return c.IncrementalAssign(ev.Partitions)

    case kafka.RevokedPartitions:
        // 增量撤销：安全冲刷分区提交
        cc.gatesM.Lock()
        for _, tp := range ev.Partitions {
            if g, ok := cc.gates[tp.Partition]; ok {
                _ = g.CommitContiguous(c)  // 尽力推进最终提交
                delete(cc.gates, tp.Partition)
            }
        }
        cc.gatesM.Unlock()
        return c.IncrementalUnassign(ev.Partitions)
    }
    return nil
})
```

**Rebalance安全要点**：
- **cooperative-sticky**：减少分区移动，支持增量分配
- **撤销前冲刷**：确保分区内所有可提交的offset都被提交
- **状态清理**：清理撤销分区的PartitionCommitGate，避免内存泄漏

---

## 5. DLQ设计（极简化版本）

### 5.1 Jenny式DLQ简化

**原设计问题**：DLQ设计过于复杂，包含恢复策略、优先级、TTL等理论概念，实际运维中难以执行。

**Jenny式简化原则**：DLQ就是死信队列，保存失败消息供后续分析即可，不要过度设计！

```go
// 使用kmq.DLQ（已实现极简版本）
type DLQ struct {
    p     *Producer  // 使用相同的分区器配置
    topic string
}

// Send 发送到死信队列（统一接口）
func (d *DLQ) Send(key []byte, v interface{}, headers []kafka.Header) error {
    // 构建简单的DLQ消息
    dlqMsg := map[string]interface{}{
        "original_message": v,
        "failed_at":        time.Now().Unix(),
        "error_context":    "processing_failed",
    }

    // 添加DLQ特有header
    dlqHeaders := append(headers,
        kafka.Header{Key: "dlq_timestamp", Value: []byte(fmt.Sprintf("%d", time.Now().Unix()))},
        kafka.Header{Key: "dlq_source", Value: []byte("qywx-consumer")},
    )

    b, err := json.Marshal(dlqMsg)
    if err != nil {
        return fmt.Errorf("marshal DLQ message: %w", err)
    }

    // 使用相同分区器发送（保证一致性）
    return d.p.Produce(d.topic, key, b, dlqHeaders)
}
```

### 5.2 DLQ Topic配置

```yaml
# DLQ Topic配置（简化版）
dlq_topic:
  name: qywx-dead-letter-queue
  partitions: 12          # 与主Topic相同，便于运维
  replication-factor: 3   # 高可用（生产环境）
  min.insync.replicas: 2
  retention.ms: 2592000000  # 30天保留（更长时间用于分析）
  compression.type: snappy  # 更高压缩比（DLQ消息通常较少）
```

### 5.3 DLQ监控与分析

```go
// DLQ简单监控（实用主义方案）
type DLQMetrics struct {
    MessagesProduced counter    // DLQ消息数量
    MessagesByReason map[string]counter  // 按失败原因分类
    LastDLQTime      time.Time  // 最后一次DLQ时间
}

// 监控建议（运维视角）
alerts:
  - name: DLQMessageRate
    condition: dlq_messages_5min > 10
    action: warn
    message: "High DLQ rate, check processing logic"

  - name: DLQUserConcentration
    condition: single_user_dlq_ratio > 0.5
    action: warn
    message: "Single user generating most DLQ messages"
```

**DLQ处理策略（实用版本）**：
1. **实时监控**：DLQ消息率、用户分布、失败原因
2. **定期分析**：周报分析DLQ消息模式，改进处理逻辑
3. **手动回放**：重要消息可以手动从DLQ回放到主Topic
4. **归档清理**：超过30天的DLQ消息归档到对象存储

**不做的事情**：
- ❌ 自动重试机制（增加复杂度，效果有限）
- ❌ 复杂的恢复策略（理论完美，实践复杂）
- ❌ 优先级系统（YAGNI - You Ain't Gonna Need It）

---

## 6. 集成与部署

### 6.1 代码集成（零破坏性）

```go
// 新的Kafka服务初始化
type KafkaService struct {
    producer *MessageProducer
    consumer *MessageConsumer
    dlq      *kmq.DLQ
}

func NewKafkaService(brokers string) (*KafkaService, error) {
    // 生产者
    producer, err := NewMessageProducer(brokers, "qywx-chat-messages")
    if err != nil {
        return nil, fmt.Errorf("create producer: %w", err)
    }

    // 消费者（注入业务处理器）
    processor := &ScheduleMessageProcessor{
        scheduleService: scheduleService,  // 注入现有业务逻辑
    }
    consumer, err := NewMessageConsumer(brokers, "qywx-consumers", processor)
    if err != nil {
        producer.Close()
        return nil, fmt.Errorf("create consumer: %w", err)
    }

    return &KafkaService{
        producer: producer,
        consumer: consumer,
    }, nil
}

// 业务处理器实现（适配现有代码）
type ScheduleMessageProcessor struct {
    scheduleService *ScheduleService
}

func (smp *ScheduleMessageProcessor) ProcessMessage(msg *ChatMessage) error {
    // 转换为现有的数据结构
    kfMsg := &kefu.KFRecvMessage{
        ExternalUserID: msg.ExternalUserID,
        OpenKFID:       msg.OpenKFID,
        MsgType:        msg.MsgType,
        // ... 其他字段映射
    }

    // 调用现有的业务逻辑（无需修改）
    return smp.scheduleService.ProcessKFMessage(kfMsg)
}
```

### 6.2 配置管理（环境统一）

```yaml
# config/kafka.yaml（统一配置）
kafka:
  brokers: "${KAFKA_BROKERS:localhost:9092}"

  topics:
    main: "qywx-chat-messages"
    dlq: "qywx-dead-letter-queue"

  producer:
    client_id: "qywx-producer-${HOSTNAME}"
    # kmq已包含所有必需配置，无需重复

  consumer:
    group_id: "qywx-consumers"
    # kmq已包含所有必需配置，无需重复

  # 环境特定配置
  development:
    partitions: 4
    replication_factor: 1
    min_insync_replicas: 1

  production:
    partitions: 12
    replication_factor: 3
    min_insync_replicas: 2
```

### 6.3 迁移策略（平滑升级）

**Phase 1：双写验证（0风险）**
```go
// 在现有代码中添加双写逻辑
func (s *ScheduleService) ProcessMessage(msg *kefu.KFRecvMessage) error {
    // 1. 保持现有处理逻辑不变
    err := s.processMessageOriginal(msg)

    // 2. 同步发送到Kafka（验证）
    go func() {
        if kafkaErr := s.kafkaProducer.ProduceMessage(msg); kafkaErr != nil {
            log.Warn("Kafka双写失败（不影响主流程）:", kafkaErr)
        }
    }()

    return err
}
```

**Phase 2：消费验证（低风险）**
```go
// 启动Kafka消费者，但只做日志记录，不执行业务逻辑
func (consumer *MessageConsumer) handleMessage(m *kafka.Message) (handled bool, err error) {
    var chatMsg ChatMessage
    json.Unmarshal(m.Value, &chatMsg)

    // 只记录日志，验证消费正常
    log.Info("Kafka消息验证:",
        "user_id", chatMsg.ExternalUserID,
        "msg_type", chatMsg.MsgType,
        "offset", m.TopicPartition.Offset)

    return true, nil  // 直接标记为handled
}
```

**Phase 3：完全切换（生产环境）**
```go
// 关闭原有处理，完全切换到Kafka消费
func (s *ScheduleService) switchToKafkaMode() {
    s.stopOriginalConsumer()    // 停止原有消费
    s.startKafkaConsumer()      // 启动Kafka消费
    s.enableKafkaProducer()     // 启用Kafka生产
}
```

---

## 7. 监控与运维

### 7.1 关键指标

**生产者监控**：
```go
type ProducerMetrics struct {
    MessagesProduced   counter   // 生产消息数量
    ProduceLatency     histogram // 生产延迟
    ProduceErrors      counter   // 生产错误数
    DLQFallbacks      counter   // DLQ兜底次数
}
```

**消费者监控（核心）**：
```go
type ConsumerMetrics struct {
    MessagesConsumed   counter   // 消费消息数量
    ProcessLatency     histogram // 处理延迟
    ProcessErrors      counter   // 处理错误数
    CommitGateBacklog  gauge    // 闸门积压（关键指标）
    RebalanceCount     counter   // Rebalance次数
}

// CommitGateBacklog计算
func (g *PartitionCommitGate) GetBacklog() int64 {
    g.mu.Lock()
    defer g.mu.Unlock()
    return int64(len(g.done))  // 已完成但未提交的消息数
}
```

**告警规则**：
```yaml
alerts:
  # 关键告警：提交闸门积压
  - name: CommitGateBacklog
    condition: max(commit_gate_backlog) > 1000
    duration: 5m
    severity: critical
    message: "PartitionCommitGate积压过多，可能有消息处理卡住"

  # 关键告警：DLQ兜底率过高
  - name: DLQFallbackRate
    condition: rate(dlq_fallbacks_5m) > 0.1
    duration: 2m
    severity: warning
    message: "DLQ兜底率过高，Kafka可能有问题"

  # Rebalance频率异常
  - name: RebalanceFrequency
    condition: rate(rebalance_count_1h) > 10
    duration: 1h
    severity: warning
    message: "Rebalance过于频繁，检查网络和配置"
```

### 7.2 运维工具

```bash
# Kafka健康检查脚本
#!/bin/bash
# kafka-health-check.sh

echo "=== Kafka集群状态 ==="
kafka-topics --bootstrap-server $BROKERS --list

echo "=== Topic分区状态 ==="
kafka-topics --bootstrap-server $BROKERS --describe --topic qywx-chat-messages

echo "=== 消费组状态 ==="
kafka-consumer-groups --bootstrap-server $BROKERS --describe --group qywx-consumers

echo "=== 积压情况 ==="
kafka-consumer-groups --bootstrap-server $BROKERS --describe --group qywx-consumers | awk '{print $1, $5, $6}' | column -t
```

### 7.3 故障处理手册

**场景1：PartitionCommitGate积压**
```bash
# 1. 查看积压分区
curl -s localhost:9090/metrics | grep commit_gate_backlog

# 2. 查看卡住的消息
kafka-console-consumer --bootstrap-server $BROKERS \
  --topic qywx-chat-messages \
  --partition $PARTITION \
  --offset $STUCK_OFFSET \
  --max-messages 10

# 3. 手动跳过（紧急情况）
# 注意：这会丢失卡住的消息，仅紧急时使用
kafka-consumer-groups --bootstrap-server $BROKERS \
  --group qywx-consumers \
  --reset-offsets \
  --to-offset $NEW_OFFSET \
  --topic qywx-chat-messages:$PARTITION \
  --execute
```

**场景2：DLQ消息回放**
```bash
# 1. 查看DLQ消息
kafka-console-consumer --bootstrap-server $BROKERS \
  --topic qywx-dead-letter-queue \
  --from-beginning \
  --max-messages 100

# 2. 选择性回放到主Topic
# （需要开发专用工具，过滤和转换DLQ消息）
./dlq-replay-tool --user-id "specific_user" --time-range "2024-01-01,2024-01-02"
```

---

## 8. 性能测试与验证

### 8.1 性能基准

**生产者性能**：
```bash
# 使用kafka-producer-perf-test测试
kafka-producer-perf-test \
  --topic qywx-chat-messages \
  --num-records 100000 \
  --record-size 1024 \
  --throughput 5000 \
  --producer-props bootstrap.servers=$BROKERS \
                    compression.type=lz4 \
                    batch.num.messages=1000

# 期望结果：
# - 吞吐量：5000+ msgs/sec
# - 延迟P99：< 50ms
# - CPU使用率：< 60%
```

**消费者性能**：
```bash
# 使用kafka-consumer-perf-test测试
kafka-consumer-perf-test \
  --topic qywx-chat-messages \
  --messages 100000 \
  --group perf-test-group \
  --bootstrap-server $BROKERS

# 期望结果：
# - 吞吐量：8000+ msgs/sec
# - PartitionCommitGate积压：< 100
# - 处理延迟P99：< 100ms
```

### 8.2 压力测试场景

**场景1：大用户量并发**
```go
// 模拟1000个用户同时发送消息
func TestHighConcurrency(t *testing.T) {
    producer, _ := NewMessageProducer(brokers, topic)
    defer producer.Close()

    var wg sync.WaitGroup
    for userID := 0; userID < 1000; userID++ {
        wg.Add(1)
        go func(uid int) {
            defer wg.Done()
            for msgID := 0; msgID < 100; msgID++ {
                msg := generateTestMessage(fmt.Sprintf("user_%d", uid))
                producer.ProduceMessage(msg)
            }
        }(userID)
    }
    wg.Wait()

    // 验证：所有消息按用户顺序处理
    // 验证：无消息丢失（通过offset连续性检查）
}
```

**场景2：Rebalance压力测试**
```bash
# 动态扩缩容测试
for i in {1..5}; do
    echo "启动消费者实例 $i"
    go run consumer_test.go --group qywx-test &
    sleep 30

    echo "检查rebalance状态"
    kafka-consumer-groups --bootstrap-server $BROKERS --describe --group qywx-test

    echo "停止消费者实例 $((i-1))"
    kill $prev_pid
    sleep 30
done

# 验证：无消息重复/丢失
# 验证：PartitionCommitGate正确冲刷
```

---

## 9. 总结

**🔴 致命问题修复**：
1. **PartitionCommitGate**彻底解决分区内连续提交问题，消除丢消息风险
2. **配置键修正**使用librdkafka正确配置，确保参数实际生效
3. **统一分区器**保证跨语言分区一致性，DLQ回流路由正确
4. **MySQL兜底机制**（TODO）三级降级保护，确保消息永不丢失

**🟡 复杂度大幅简化**：
1. **移除特殊情况**：废除复杂降级逻辑、多层熔断器、UserProcessor状态机
2. **DLQ极简化**：从理论完美的恢复系统简化为实用的死信队列
3. **统一配置**：基于kmq标准实现，开发/生产环境一致

**🟢 架构质量提升**：
1. **数据结构优先**：PartitionCommitGate作为核心，其他逻辑围绕它简化
2. **实用主义设计**：直接使用confluent-kafka-go最佳实践，避免重复造轮子
3. **零破坏性迁移**：保持业务API不变，内部实现平滑升级

---

## 10. 设计深化与实施计划（严格有序 + Kafka 接入落地）

### 10.1 结论与目标
- 同一用户（external_user_id）严格有序是刚性要求；保持“每用户一个 Processor goroutine 串行处理”的既有架构不变。
- 用 Kafka 替代内存通道，核心是“处理完成→再推进分区 offset 提交”，形成可靠闭环；生产端消费 DR 事件，所有异步投递失败写入 DLQ。

### 10.2 数据流与控制点（端到端）
- 生产：微信回调→Kafka Producer（Key=external_user_id，partitioner=murmur2_random）→Topic
- 消费：Kafka Consumer（cooperative-sticky, 手动 store/commit）→ 将消息包装为 Envelope 投递到 Scheduler.dispatchMessage（驱动每用户 Processor 串行处理）→ 处理完成触发 ack → PartitionCommitGate.MarkDone + 批量 StoreOffsets/CommitOffsets
- DLQ：
  - 生产端：DR 失败→DLQ（Key 与 Header 透传，便于回放）
  - 消费端：业务不可恢复→DLQ（handled=true，推进 gate）

### 10.3 kmq 增强（已更新）
- 生产者（/Users/peter/projs/libs/mq/kmq/producer.go）
  - 顶层配置 `partitioner="murmur2_random"`，弃用 `default.topic.config`。
  - 开启 `statistics.interval.ms=60000`，`go.delivery.report.fields="key,value,headers"`。
  - 新增 `AttachDLQ(*DLQ)`：DR 失败自动写入 DLQ（使用同 Key、Headers），避免静默丢失。
- 消费者（/Users/peter/projs/libs/mq/kmq/consumer.go）
  - 新增 `PollAndDispatchWithAck(handler func(m *kafka.Message, ack func(success bool)), pollMs int)`：
    - handler 将消息投递给每用户 Processor；待 Processor 真实处理完成后调用 `ack(true)`，进而 `gate.MarkDone→Batch.Store→定时/阈值 Commit()`。
    - ack 具备幂等保护（`sync.Once`）。
  - 继续支持现有 `PollAndDispatch`（返回 handled 的老接口），便于渐进迁移。
  - Rebalance：Assigned 用 `Committed()` 作为 gate 起点（低水位为备）；Revoked 先 `CommitContiguous()` 再 `IncrementalUnassign()`（已实现）。

### 10.4 与 Scheduler 对接（严格有序保持不变）
- 现状：`schedule.dispatcher()` 将消息按 userID 路由到 `processors[userID].messageChan`，Processor.run() 串行处理（见 schedule/schedule.go:124, :220；processor.go:17, :43）。
- Kafka 接入适配层：
  - 使用 `kmq.Consumer.PollAndDispatchWithAck`；在 handler 内：
    1) 将 Kafka 消息解包为 KFRecvMessage；
    2) 投递到 `Scheduler.dispatchMessage`（或直达 per-user `processor.messageChan`）；
    3) 在 Processor 处理流的“真实完成点”（成功处理或写入 DLQ）调用 `ack(true)`。
  - 若因业务判定“不可处理且不应推进”，调用 `ack(false)`（通常用不到，建议失败就 DLQ）。

示意代码（消费端 handler 侧伪码）：
```go
handler := func(m *kafka.Message, ack func(bool)) {
    var chatMsg ChatMessage
    if err := json.Unmarshal(m.Value, &chatMsg); err != nil {
        _ = dlq.Send(m.Key, m.Value, m.Headers)
        ack(true) // 已安全落DLQ，推进gate
        return
    }

    // 将消息投递到 Scheduler（每用户串行）
    delivered := tryDispatchToScheduler(&chatMsg, func onDone(success bool) { ack(success) })
    if !delivered {
        // 无法投递时可降级DLQ，避免阻塞提交推进
        _ = dlq.Send(m.Key, m.Value, m.Headers)
        ack(true)
    }
}

go consumer.PollAndDispatchWithAck(handler, 100)
```

### 10.5 生产者与 DLQ 策略
- Producer：`acks=all`, `enable.idempotence=true`, `message.timeout.ms=30000`，`partitioner=murmur2_random`。
- 事件循环消费 DR：一旦 `msg.TopicPartition.Error != nil` → 写 DLQ，Headers 透传；关闭前 `Flush()` 确保出清。
- DLQ：与主 Topic 同 Key/分区器，回放命中原分区；消息结构建议包含 original_topic/partition/offset/reason/version。

### 10.6 可观测性与告警
- 生产：DR 失败率、Flush 剩余、队列深度、吞吐；导出 librdkafka Stats JSON（`statistics.interval.ms` 已启用）。
- 消费：gate backlog、提交滞后（high watermark - committed）、rebalance 次数、每用户处理时延 P99、DLQ 写入率。
- 告警：
  - gate backlog/提交滞后持续超阈（例如 > 60s）。
  - DR 失败率异常上升、DLQ 爆量。

### 10.7 配置基线（librdkafka）
- Producer 关键：`acks=all`, `enable.idempotence=true`, `message.timeout.ms=30000`, `partitioner=murmur2_random`, `go.delivery.report.fields=key,value,headers`。
- Consumer 关键：`enable.auto.commit=false`, `enable.auto.offset.store=false`, `partition.assignment.strategy=cooperative-sticky`, `auto.offset.reset=earliest`, `statistics.interval.ms=60000`。

### 10.8 迁移与演练
- Phase 0（双写）：保留现有通道主路径，同时 Producer 双写 Kafka，验证 DR→DLQ 闭环与吞吐。
- Phase 1（旁路消费）：Consumer 读取 Kafka→仅落日志或执行业务但不提交，用于稳定性验证。
- Phase 2（切换）：启用 `PollAndDispatchWithAck`，以 ack 为准推进提交；保留快速回滚开关。
- 混沌/演练：断网、broker 重启、扩缩容；验证 revoked 冲刷、assigned 连续性、DR 失败→DLQ。

### 10.9 验收标准
- 顺序性：同一 external_user_id 的处理日志与业务副作用严格递增，无乱序；跨 Key 并发不串扰。
- 可靠性：生产端无静默丢失（DR→DLQ）；消费端 gate backlog 可控、提交曲线连续；Rebalance 无丢失/无限重复。
- 性能：满足 100→500 并发用户目标下的 P99 指标（生产<50ms、处理<100ms），并提供扩容准则。

### 10.10 后续可选增强
- 事务/EOS：若出现“Consume→Produce”且要求跨 Topic 原子性，评估 transactional.id + SendOffsetsToTransaction。
- 回放工具：DLQ→主Topic 选择性回放（按 user/time/原因）；写入 `replayed=true` header。

### 10.11 参考示例（可直接运行）
- 路径：`/Users/peter/projs/example/kafka-bridge`
  - `producer/main.go`：演示 acks=all + 幂等 + DR→DLQ。
  - `consumer/main.go`（生产对接入口，推荐）：演示手动 store/commit、cooperative-sticky、ack(true) 才推进提交（使用批量提交管理，避免“完成即提交”）。
  - `adapter/scheduler_adapter.go`：按用户串行适配器，暴露 onResult(err) → 在其中触发 ack(true)。
  - `types/commit_gate.go`：分区提交闸门示例实现。
  - `consumer-mock/main.go` 与 `scheduler/mock.go`（仅演示/联调，禁止用于生产）：使用 mock Scheduler（每用户 goroutine）复现 onDone(err)→ack(true) 的完成点，配合批量提交管理。
  - 运行方式与说明见 `README.md`。
