# Kafka Topic 配置指南 - qywx-recorder

## 概述

为支持对话记录持久化，需要创建/配置 Kafka Topic `qywx-recorder`，并将单条消息容量提升到 **6 MiB**。

**关键要求**：必须同步调整 4 处阈值（Topic/Broker/Producer/Consumer），最终上限取四者最小值。

---

## 1. Topic 配置（二选一）

### 1.1 创建新 Topic

如果 Topic 不存在，使用以下命令创建：

```bash
kafka-topics \
  --bootstrap-server <BOOTSTRAP_SERVER> \
  --create \
  --topic qywx-recorder \
  --partitions 24 \
  --replication-factor 3 \
  --config cleanup.policy=delete \
  --config retention.ms=604800000 \
  --config max.message.bytes=6291456
```

**参数说明**：
- `partitions=24`：支持高并发消费
- `replication-factor=3`：高可用
- `retention.ms=604800000`：保留 7 天（604800000ms = 7 days）
- `max.message.bytes=6291456`：允许单条消息最大 6 MiB（6,291,456 字节）

---

### 1.2 修改已存在的 Topic

#### 1.2.1 添加/覆盖 Topic 级配置

```bash
kafka-configs \
  --bootstrap-server <BOOTSTRAP_SERVER> \
  --alter \
  --entity-type topics \
  --entity-name qywx-recorder \
  --add-config cleanup.policy=delete,retention.ms=604800000,max.message.bytes=6291456
```

#### 1.2.2 扩分区到 24（只能增加，不能减少）

```bash
kafka-topics \
  --bootstrap-server <BOOTSTRAP_SERVER> \
  --alter \
  --topic qywx-recorder \
  --partitions 24
```

---

## 2. 验证配置

### 2.1 查看 Topic 元信息

```bash
kafka-topics \
  --bootstrap-server <BOOTSTRAP_SERVER> \
  --describe \
  --topic qywx-recorder
```

**预期输出**：
- PartitionCount: 24
- ReplicationFactor: 3
- Leader/Replicas 分布均匀

---

### 2.2 查看生效中的 Topic 配置

```bash
kafka-configs \
  --bootstrap-server <BOOTSTRAP_SERVER> \
  --describe \
  --entity-type topics \
  --entity-name qywx-recorder | egrep 'cleanup.policy|max.message.bytes|retention.ms'
```

**预期输出**：
```
cleanup.policy=delete
max.message.bytes=6291456
retention.ms=604800000
```

---

## 3. 客户端配置（应用层）

### 3.1 Producer 配置（librdkafka / Confluent 客户端）

在 `config.toml` 中添加：

```toml
[kafka.producer.recorder]
clientId = "qywx-producer-recorder"
acks = "all"
enableIdempotence = true
compressionType = "lz4"
lingerMs = 10
messageSendMaxRetries = 10
messageTimeoutMs = 30000

# 关键：Producer 发送阈值
messageMaxBytes = 6291456
```

**注意**：librdkafka 与 Java 客户端在参数命名上有历史差异，`messageMaxBytes` 已覆盖。

---

### 3.2 Consumer 配置（librdkafka / Confluent 客户端）

在 `config.toml` 中添加：

```toml
[kafka.consumer.recorder]
groupId = "qywx-recorder-group"
partitionAssignmentStrategy = "cooperative-sticky"
enableAutoCommit = false
enableAutoOffsetStore = false
autoOffsetReset = "earliest"
sessionTimeoutMs = 30000
heartbeatIntervalMs = 3000
maxPollIntervalMs = 600000
fetchWaitMaxMs = 50

# 关键：Consumer 拉取阈值
maxPartitionFetchBytes = 6291456   # 单分区一次最多拉取
fetchMaxBytes = 67108864           # 单次请求总量（64 MiB）
```

---

## 4. 全链路压测建议

配置完成后，建议进行一次全链路压测：

1. **生产大消息**：发送接近 6 MiB 的 JSON 消息
2. **观察 Broker**：
   - 检查副本复制是否成功（`kafka-topics --describe`）
   - 监控内存占用和 GC
3. **验证消费**：Consumer 能否正常拉取并处理大消息
4. **监控指标**：
   - Producer 发送延迟
   - Consumer 消费延迟
   - Broker 副本 Lag

---

## 5. 常见问题排查

### 问题 1：Producer 发送失败 "Message size too large"

**原因**：Producer 的 `messageMaxBytes` < Topic 的 `max.message.bytes`

**解决**：调大 Producer 配置中的 `messageMaxBytes`

---

### 问题 2：Consumer 拉取失败

**原因**：Consumer 的 `maxPartitionFetchBytes` < 单条消息大小

**解决**：调大 Consumer 配置中的 `maxPartitionFetchBytes`

---

### 问题 3：副本复制失败（罕见）

**原因**：Broker 的 `replica.fetch.max.bytes` < Topic 的 `max.message.bytes`

**解决**：联系运维调大 Broker 配置中的 `replica.fetch.max.bytes` 并滚动重启

---

## 6. 附录：6 MiB 计算说明

- **业务需求**：支持单条消息约 5 MB
- **安全余量**：5 MB × 120% = 6 MB
- **字节换算**：6 MB = 6,291,456 字节 = 6 MiB（二进制）

---

## 7. 执行清单

- [ ] 执行 1.1 或 1.2：创建/修改 Topic `qywx-recorder`
- [ ] 执行 2.1 和 2.2：验证 Topic 配置生效
- [ ] 应用层添加 3.1 和 3.2：Producer/Consumer 配置
- [ ] 执行 4：全链路压测验证

---

**文档版本**：1.0
**最后更新**：2025-10-04
