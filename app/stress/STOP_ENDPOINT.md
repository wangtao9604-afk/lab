# 压测停止端点使用指南

## 概述

为了方便压测后的数据统计和分析，系统在 Consumer 服务中添加了 `/stop` 端点，用于优雅停止 Scheduler 调度器。

## 设计变更

### 1. 移除 Reset 逻辑

**之前的设计**：
- Producer 提供 `/stress/reset` 端点重置 MsgID 计数器
- 每次压测前调用 reset 确保从 MsgID=1 开始

**变更原因**：
- MsgID 重置会影响顺序检测的准确性
- 不利于跨多轮压测的数据累积分析
- 增加了不必要的复杂性

**新设计**：
- 移除 `/stress/reset` 端点
- MsgID 持续递增，不重置
- 通过停止 Scheduler 来结束压测

### 2. 新增 Stop 端点

**端点信息**：
- URL: `POST http://localhost:11113/stop`
- 仅在 `stress=true` 模式下注册
- 调用 `scheduler.Stop()` 优雅停止调度器

**作用**：
- 停止所有 Processor 的消息处理
- 等待当前正在处理的消息完成
- 输出最终的统计数据（如果有）

## 使用方法

### 启动压测

```bash
# 1. 启动 Producer 和 Consumer（压测模式）
# 确保 config.toml 中 stress=true

./bin/producer &
./bin/consumer &

# 2. 运行压测脚本（不再需要 reset）
cd app/stress
./run_stress.sh 10000 10

# 输出示例：
# ======================================
#     qywx Stress Test
# ======================================
# Target URL:     http://localhost:11112/stress
# Total Requests: 10000
# Concurrency:    10
# ======================================
#
# Progress: [====================] 100% (10000/10000)
#
# ✓ All requests completed successfully!
```

### 停止压测

压测完成后，调用 stop 端点停止 Scheduler：

```bash
# 停止 Scheduler（等待所有消息处理完成）
curl -X POST http://localhost:11113/stop

# 响应示例：
# {
#   "ok": true,
#   "message": "Scheduler stopped successfully"
# }
```

### 查看结果

停止后可以查看各种统计数据：

```bash
# 1. 查看 Prometheus 指标
curl http://localhost:11113/metrics | grep stress

# 输出示例：
# qywx_stress_sequence_violations_total 0
# qywx_stress_messages_processed_total{result="ok"} 10000000
# qywx_stress_processing_duration_seconds_sum 30000.5

# 2. 查看日志中的计数统计
tail -100 /var/log/qywx/consumer.log | grep "消息拉取耗时\|消息分发"

# 输出示例：
# 消息拉取耗时: count=10000, topic=wx_inbound_chat, partition=3, offset=12345, elapsed=523.456µs
# 消息分发成功: count=10000, user=wm6Gh8CQAAs..., type=text, elapsed=234.567µs
```

## API 参考

### POST /stop

**描述**：优雅停止 Scheduler 调度器

**请求**：
```bash
curl -X POST http://localhost:11113/stop
```

**响应**（成功）：
```json
{
  "ok": true,
  "message": "Scheduler stopped successfully"
}
```

**响应**（失败）：
```json
{
  "ok": false,
  "error": "scheduler already stopped"
}
```

**状态码**：
- `200 OK`: 停止成功
- `500 Internal Server Error`: 停止失败

**注意事项**：
- ⚠️ 仅在 `stress=true` 模式下可用
- ⚠️ 调用后 Scheduler 不会重启，需要重启 Consumer 服务才能继续压测
- ⚠️ 停止过程可能需要几秒钟（等待所有消息处理完成）

## 完整压测流程

### 单轮压测

```bash
# 1. 启动服务（压测模式）
./bin/producer &
PRODUCER_PID=$!
./bin/consumer &
CONSUMER_PID=$!

# 等待服务启动
sleep 3

# 2. 运行压测
cd app/stress
./run_stress.sh 10000 10

# 3. 停止 Scheduler
curl -X POST http://localhost:11113/stop

# 4. 收集结果
./check_metrics.sh

# 5. 停止服务
kill $PRODUCER_PID $CONSUMER_PID
```

### 多轮压测（累积数据）

如果想要多轮压测数据累积：

```bash
# 1. 启动服务
./bin/producer &
./bin/consumer &

# 2. 第一轮压测
./run_stress.sh 5000 10
sleep 5

# 3. 第二轮压测（MsgID 继续递增）
./run_stress.sh 5000 10
sleep 5

# 4. 第三轮压测
./run_stress.sh 5000 10

# 5. 停止并查看累积结果
curl -X POST http://localhost:11113/stop
./check_metrics.sh

# 总消息数：15000
# MsgID 范围：1-15000（跨越三轮）
```

## 监控指标说明

### 消息拉取计数

**日志格式**：
```
消息拉取耗时: count=<累积数>, topic=<主题>, partition=<分区>, offset=<偏移>, elapsed=<耗时>
```

**示例**：
```
消息拉取耗时: count=1234, topic=wx_inbound_chat, partition=3, offset=5678, elapsed=523.456µs
```

- `count`: 从 Consumer 启动后累积拉取的消息总数
- `elapsed`: 单条消息从拉取到分发的耗时

### 消息分发计数

**日志格式**：
```
消息分发成功: count=<累积数>, user=<用户ID>, type=<消息类型>, elapsed=<耗时>
消息分发超时: count=<累积数>, user=<用户ID>, elapsed=<耗时>
```

**示例**：
```
消息分发成功: count=1234, user=wm6Gh8CQAAs..., type=text, elapsed=234.567µs
```

- `count`: 从 Scheduler 启动后累积分发的消息总数
- `elapsed`: 单条消息分发到 Processor 的耗时

## 故障排查

### 问题 1：/stop 端点 404

**原因**：压测模式未启用

**解决**：
```toml
# config.toml
stress = true
```

重启 Consumer 后检查日志：
```
WARN Stress test mode enabled, /stop endpoint registered
```

### 问题 2：停止超时或失败

**可能原因**：
- 有大量消息正在处理中
- Processor 阻塞在某个操作上

**排查**：
```bash
# 查看 Consumer 日志
tail -f /var/log/qywx/consumer.log

# 检查是否有错误或阻塞
grep "ERROR\|timeout\|block" /var/log/qywx/consumer.log
```

**强制停止**：
```bash
# 如果 /stop 端点无响应，直接终止进程
kill -TERM <consumer_pid>
```

### 问题 3：重复调用 /stop

**行为**：第二次调用可能返回错误

**原因**：Scheduler 已经停止

**解决**：重启 Consumer 服务即可

## 与旧版本的兼容性

### 迁移指南

如果您之前使用的是带 reset 的压测流程：

**旧流程**：
```bash
curl -X POST http://localhost:11112/stress/reset  # ← 不再需要
./run_stress.sh 1000 10
```

**新流程**：
```bash
./run_stress.sh 1000 10                            # ← 直接运行
curl -X POST http://localhost:11113/stop           # ← 压测后停止
```

**主要区别**：
- ✅ 不需要在压测前 reset
- ✅ 压测后调用 /stop 停止
- ✅ MsgID 持续递增，便于长期监控
- ✅ 可以跨多轮累积数据

## 参考资料

- 主 README: `app/stress/README.md`
- UserID 管理: `app/stress/USERIDS.md`
- 安装指南: `app/stress/INSTALL.md`
- Consumer 代码: `app/consumer/main.go`
- Scheduler 代码: `models/schedule/schedule.go`
