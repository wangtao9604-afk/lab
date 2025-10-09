# 压测系统使用指南

## 概述

qywx 压测系统用于验证 Kafka 消息队列的**顺序性**和**系统吞吐能力**。

**核心特性：**
- 模拟 1000 个用户，每用户消息 ID 严格递增
- 端到端链路：Producer → Kafka → Consumer
- 自动检测消息顺序违规
- Prometheus 监控指标

---

## 快速开始

### 0. 准备 UserID 数据

压测系统使用预生成的固定 UserID 集合以确保测试的可重复性。`userids.json` 文件已经预生成并包含在代码仓库中。

```bash
# 验证 UserID 文件存在
ls -lh app/stress/userids.json

# 如需重新生成（可选）
go run app/stress/generate_userids.go
```

**📖 详细说明**：参见 [USERIDS.md](./USERIDS.md)

---

### 1. 配置压测模式

编辑 `/etc/qywx/config.toml`（或项目根目录的 `config_fixed.toml`）：

```toml
# 全局配置
environment = "dev"
platform = 2
stress = true  # ✓ 启用压测模式
```

**重要提示：**
- `stress = true` 时：
  - Producer 会注册 `/stress` HTTP 端点
  - Consumer 会进入压测处理模式（3秒 sleep + 顺序检测）
  - 所有业务逻辑被跳过
- `stress = false` 时（默认）：
  - 正常业务模式
  - `/stress` 端点不会注册

---

### 2. 启动服务

```bash
# 编译服务
make build_dynamic  # macOS/开发环境
# 或
make build_amd64_static  # Linux/生产环境

# 启动 Producer（监听 11112 端口）
./bin/producer &

# 启动 Consumer
./bin/consumer &
```

**验证服务启动：**
```bash
# 检查 Producer 健康
curl http://localhost:11112/health

# 检查 Consumer 健康
curl http://localhost:11113/health

# 查看 Prometheus 指标
curl http://localhost:11112/metrics | grep stress
curl http://localhost:11113/metrics | grep stress
```

---

### 3. 运行压测

⚠️ **重要提示**：压测脚本会自动重置 MsgID 计数器，确保每次压测从 MsgID=1 开始。

```bash
cd app/stress

# 基础压测（默认 10000 请求，10 并发）
./run_stress.sh

# 自定义参数
./run_stress.sh 5000 20  # 5000 请求，20 并发

# 环境变量方式
PRODUCER_URL=http://localhost:11112/stress \
TOTAL_REQUESTS=10000 \
CONCURRENT=10 \
./run_stress.sh
```

**工作流程**：
```
1. POST /stress/reset  → 重置所有用户的 MsgID 计数器
2. POST /stress         → 发送第 1 批消息（MsgID=1）
3. POST /stress         → 发送第 2 批消息（MsgID=2）
...
```

**脚本输出示例：**
```
======================================
    qywx Stress Test
======================================
Target URL:     http://localhost:11112/stress
Total Requests: 10000
Concurrency:    10
======================================

Progress: [====================] 100% (10000/10000)

======================================
    Test Results
======================================
Total Requests:  10000
Successful:      10000
Failed:          0
Duration:        120s
Requests/sec:    83
======================================

✓ All requests completed successfully!
```

---

### 4. 查看监控指标

```bash
# 查询 Prometheus 指标汇总
./check_metrics.sh

# 指定 Prometheus 地址
./check_metrics.sh http://prometheus.example.com:9090
```

**输出示例：**
```
======================================
    Stress Test Metrics
======================================
Prometheus: http://localhost:9090
======================================

✓ Connected to Prometheus

==> Sequence Violations
  Total violations: 0

==> Messages Processed
  Total processed:  10000000
  Successful (ok):  10000000
  Errors:           0

==> Processing Duration (p50, p95, p99)
  p50: 3.01s
  p95: 3.05s
  p99: 3.12s

======================================
    Summary
======================================
✓ No sequence violations detected!
Success rate: 100.00%
======================================
```

---

## 监控指标说明

### Prometheus 指标

| 指标名称 | 类型 | 说明 | Labels |
|---------|------|------|--------|
| `qywx_stress_sequence_violations_total` | Counter | 消息顺序违规总数 | 无 |
| `qywx_stress_messages_processed_total` | Counter | 处理的消息总数 | `result`=(ok\|sequence_error\|invalid_format) |
| `qywx_stress_processing_duration_seconds` | Histogram | 消息处理延迟（含3s sleep） | 无 |

**设计说明：**
- 所有指标**不使用高基数 labels**（如 user_id, seq），避免时序爆炸
  - 原因：1000 个用户 × 多个序列值 = 数千个时序，会导致 Prometheus 性能问题
  - 替代方案：使用低基数的 `result` label（ok/sequence_error/invalid_format）
- 详细违规信息通过**日志**记录，包含 UserID、期望值、实际值
  - 日志适合存储高基数数据（支持全文检索、过滤、聚合）
  - Prometheus 指标提供聚合视图（总违规数、成功率、延迟分布）

### 日志位置

```bash
# Producer 日志
tail -f /var/log/qywx/producer.log

# Consumer 日志（包含顺序违规详情）
tail -f /var/log/qywx/consumer.log | grep "Sequence violation"
```

**顺序违规日志示例：**
```
2025-10-08 21:50:15 ERROR Sequence violation - UserID: wm6Gh8CQAAsXXXXXX, Expected: 42, Got: 44
```

---

## 压测原理

### 消息生成流程

```
HTTP POST /stress
    ↓
handleStressRequest (Producer)
    ↓
handleWithStress (KFService)
    ↓
SimulateKFMessages (生成 1000 条消息)
    ├─ UserID: wm6Gh8CQAAsXXXXXX (1000 个不同用户)
    │   └─ 均匀分布策略：
    │       ├─ 分区 0-7:  每个 63 个用户
    │       └─ 分区 8-15: 每个 62 个用户
    │       (使用 MurmurHash2 算法确保精确映射)
    ├─ MsgID:  "1", "2", "3", ... (每用户严格递增)
    └─ Content: 50 个随机汉字
    ↓
发送到 Kafka (batch=1000, 分布到 16 个分区)
```

### 消息处理流程

```
Kafka Consumer
    ↓
processStressMessage (Consumer)
    ├─ 解析 MsgID (string → int64)
    ├─ 顺序检测 (atomic.Int64 比较)
    ├─ 记录 Prometheus 指标
    ├─ Sleep 3 秒 (模拟业务耗时)
    └─ Ack 消息
```

### Kafka 分区负载均衡

**关键设计**：1000 个 UserID 精确均匀分配到 16 个 Kafka 分区

**实现原理**：
```
1. 使用 MurmurHash2 算法计算 UserID → 分区映射
2. 暴力搜索生成能映射到目标分区的 UserID
3. 确保每个分区负载相同（62-63 个用户）
```

**分配结果**：
```
分区 0-7:   63 个用户 ██████ (1000 / 16 = 62 余 8)
分区 8-15:  62 个用户 ██████
偏差: 1 个用户 (最优分布)
```

**验证工具**：
```bash
go run test/verify_userid_distribution.go
```

**为什么这很重要？**
```
❌ 随机分布（可能不均匀）：
   分区 3:  150 个用户 → 负载过高
   分区 10: 30 个用户  → 资源浪费

✓ 确定性均匀分布：
   每分区 62-63 个用户 → 负载均衡
   → 压测结果更准确，能真实反映系统吞吐能力
```

### 顺序检测逻辑

每个 Processor 维护一个 `atomic.Int64` 跟踪期望的下一个 MsgID：

```go
// 第一条消息：MsgID="1"
expectedSeq = 1  // 初始化
actualSeq = 1    // ✓ 匹配
expectedSeq = 2  // 更新

// 第二条消息：MsgID="2"
expectedSeq = 2  // 当前期望
actualSeq = 2    // ✓ 匹配
expectedSeq = 3  // 更新

// 第三条消息：MsgID="5" (乱序！)
expectedSeq = 3  // 当前期望
actualSeq = 5    // ✗ 违规！
→ 上报指标 + 记录日志
```

---

## 性能基准

### 预期性能（参考）

| 场景 | 配置 | 预期吞吐 | 说明 |
|------|------|----------|------|
| 单次请求 | 1000 消息/请求 | ~1000 msg/s | 受 3s sleep 限制 |
| 并发压测 | 10 并发 | ~10000 msg/s | 10 Processor 并行 |
| 高并发 | 100 并发 | ~100000 msg/s | 需增加 Kafka 分区数 |

**限制因素：**
- Consumer 每条消息 sleep 3 秒（可调整）
- Kafka 分区数（影响并行度）
- Producer/Consumer 实例数

---

## 故障排查

### 问题 1：`/stress` 端点 404

**原因：** 压测模式未启用

**解决：**
```toml
# config.toml
stress = true  # ← 确保设置为 true
```

重启 Producer 后检查日志：
```
WARN Stress test mode enabled, /stress endpoint registered
```

---

### 问题 2：顺序违规数量异常

**可能原因：**
1. Kafka 消费者重平衡导致重复消费
2. Consumer 实例重启（SequenceChecker 重置）
3. 真实的消息丢失或乱序

**排查步骤：**
```bash
# 1. 检查 Kafka Consumer Group 状态
kafka-consumer-groups --bootstrap-server localhost:9092 \
  --group qywx-schedule-consumer --describe

# 2. 查看 Consumer 日志中的违规详情
grep "Sequence violation" /var/log/qywx/consumer.log

# 3. 检查 Kafka 分区分配
# 确保每个分区只被一个 Consumer 实例消费
```

---

### 问题 3：处理延迟过高

**检查项：**
```bash
# 查看 p99 延迟
curl -s http://localhost:9090/api/v1/query \
  --data-urlencode 'query=histogram_quantile(0.99, rate(qywx_stress_processing_duration_seconds_bucket[5m]))'

# 检查 Processor 数量
curl http://localhost:11113/metrics | grep processor_count
```

**优化方向：**
- 减少 sleep 时间（修改 `processor.go:2646`）
- 增加 Kafka 分区数（提高并行度）
- 增加 Consumer 实例数

---

## 配置调优

### Producer 配置

```toml
[kafka.producer.chat]
    clientID = "qywx-producer-stress"
    acks = "all"           # 确保消息可靠性
    compression = "snappy" # 压缩提升吞吐
    batchSize = 1000       # 批量发送
    lingerMs = 10          # 延迟聚合
```

### Consumer 配置

```toml
[kafka.consumer.schedule]
    groupID = "qywx-schedule-consumer"
    autoOffsetReset = "earliest"  # 压测时从头消费
    maxPollRecords = 500          # 批量拉取
    sessionTimeoutMs = 30000
```

---

## 安全注意事项

⚠️ **生产环境警告：**

### 1. 端点保护

**当前实现：** `/stress` 端点仅通过 `stress=true` 配置控制是否注册，**没有身份认证**。

**生产环境建议：**
- **永远不要在生产环境启用 stress=true**
- 如需在生产环境测试，必须添加以下保护措施之一：
  - IP 白名单（通过反向代理如 Nginx）
  - Basic Authentication
  - API Token 验证
  - 网络隔离（仅内网可访问）

**示例 Nginx 配置（IP 白名单）：**
```nginx
location /stress {
    allow 192.168.1.0/24;  # 仅允许内网访问
    deny all;
    proxy_pass http://localhost:11112;
}
```

**示例代码（添加 Token 认证）：**
```go
router.POST("/stress", func(c *gin.Context) {
    token := c.GetHeader("X-Stress-Token")
    if token != cfg.StressToken {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
        return
    }
    handleStressRequest(c, kfService)
})
```

### 2. 配置文件安全

⚠️ **切勿将包含真实凭据的配置文件提交到 Git 仓库**

**config_fixed.toml 包含敏感信息：**
- MiniMax API Key
- MySQL 连接字符串（含密码）
- Redis 密码
- 企业微信密钥

**最佳实践：**
```bash
# .gitignore 中添加
config.toml
config_*.toml
*.env

# 使用环境变量或密钥管理系统
export MINIMAX_API_KEY="your-key"
export MYSQL_PASSWORD="your-password"
```

### 3. Kafka 数据安全

- 压测前备份 Kafka Topic（避免数据污染）
- 使用独立的 Kafka 集群或 Topic
- 压测后清理测试数据：
  ```bash
  kafka-topics --delete --topic wx_inbound_chat \
    --bootstrap-server localhost:9092
  ```

---

## 扩展阅读

- 设计文档：`/Users/peter/projs/qywx/app/stress/plan.txt`
- Makefile 构建：`/Users/peter/projs/qywx/Makefile`
- Kafka 配置：`config.toml` 中的 `[kafka]` 部分
- Prometheus 指标：`observe/prometheus/stress.go`
