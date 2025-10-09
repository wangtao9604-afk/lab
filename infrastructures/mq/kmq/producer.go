package kmq

import (
	"fmt"
	"strings"

	qconfig "qywx/infrastructures/config"
	qlog "qywx/infrastructures/log"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Producer 封装了 confluent Kafka producer，内置默认配置与 DLQ 转发能力。
type Producer struct {
	p   *kafka.Producer
	dlq *DLQ
}

// AttachDLQ 绑定一个 DLQ producer；开启后，所有发送失败的消息会携带原始键与头部写入 DLQ，方便后续分析或回放。
func (kp *Producer) AttachDLQ(d *DLQ) {
	kp.dlq = d
}

// NewProducer 使用 librdkafka 风格的配置创建 producer，配置遵循方案要求：murmur2_random 分区器、幂等发送以及包含头部信息的发送结果回调。

func NewProducer(brokers, clientID string) (*Producer, error) {
	cm, err := buildProducerConfig(brokers, clientID)
	if err != nil {
		return nil, err
	}

	p, err := kafka.NewProducer(cm)
	if err != nil {
		return nil, fmt.Errorf("create producer failed: %w", err)
	}

	kp := &Producer{p: p}

	// 监听发送结果，将失败的消息尽力转发到 DLQ。
	go func(events chan kafka.Event) {
		for ev := range events {
			msg, ok := ev.(*kafka.Message)
			if !ok {
				continue
			}

			if msg.TopicPartition.Error != nil {
				qlog.GetInstance().Sugar.Warnf("Kafka 发送失败: %v", msg.TopicPartition)
				if kp.dlq != nil {
					if err := kp.dlq.SendWithContext(msg, "produce_delivery_failed"); err != nil {
						qlog.GetInstance().Sugar.Errorf("发送失败消息写入 DLQ 也失败: %v", err)
					}
				}
			}
		}
	}(p.Events())

	return kp, nil
}

func buildProducerConfig(brokers, clientID string) (*kafka.ConfigMap, error) {
	cfg := qconfig.GetInstance().Kafka

	effectiveBrokers := brokers
	if effectiveBrokers == "" {
		effectiveBrokers = cfg.Brokers
	}
	if effectiveBrokers == "" {
		return nil, fmt.Errorf("kafka producer: bootstrap servers not configured")
	}

	// 根据 clientID 选择使用 Chat 或 Recorder 配置
	var producerCfg qconfig.KafkaProducerConfig
	if clientID != "" && (strings.Contains(clientID, "recorder") || strings.Contains(clientID, "Recorder")) {
		producerCfg = cfg.Producer.Recorder
	} else {
		producerCfg = cfg.Producer.Chat
	}

	effectiveClientID := clientID
	if effectiveClientID == "" {
		effectiveClientID = producerCfg.ClientID
	}
	if effectiveClientID == "" {
		return nil, fmt.Errorf("kafka producer: client id not configured")
	}

	configMap := kafka.ConfigMap{
		"bootstrap.servers": effectiveBrokers,
		"client.id":         effectiveClientID,
	}

	if producerCfg.Acks != "" {
		configMap["acks"] = producerCfg.Acks
	}
	if producerCfg.EnableIdempotence != nil {
		configMap["enable.idempotence"] = *producerCfg.EnableIdempotence
	}
	if producerCfg.MessageSendMaxRetries > 0 {
		configMap["message.send.max.retries"] = producerCfg.MessageSendMaxRetries
	}
	if producerCfg.MessageTimeoutMs > 0 {
		configMap["message.timeout.ms"] = producerCfg.MessageTimeoutMs
	}
	if producerCfg.LingerMs > 0 {
		configMap["linger.ms"] = producerCfg.LingerMs
	}
	if producerCfg.BatchNumMessages > 0 {
		configMap["batch.num.messages"] = producerCfg.BatchNumMessages
	}
	if producerCfg.QueueBufferingMaxMs > 0 {
		configMap["queue.buffering.max.ms"] = producerCfg.QueueBufferingMaxMs
	}
	if producerCfg.QueueBufferingMaxKB > 0 {
		configMap["queue.buffering.max.kbytes"] = producerCfg.QueueBufferingMaxKB
	}
	if producerCfg.CompressionType != "" {
		configMap["compression.type"] = producerCfg.CompressionType
	}
	if producerCfg.SocketKeepaliveEnable != nil {
		configMap["socket.keepalive.enable"] = *producerCfg.SocketKeepaliveEnable
	}
	if producerCfg.ConnectionsMaxIdleMs > 0 {
		configMap["connections.max.idle.ms"] = producerCfg.ConnectionsMaxIdleMs
	}
	if producerCfg.StatisticsIntervalMs > 0 {
		configMap["statistics.interval.ms"] = producerCfg.StatisticsIntervalMs
	}
	if producerCfg.GoDeliveryReportFields != "" {
		configMap["go.delivery.report.fields"] = producerCfg.GoDeliveryReportFields
	}
	if producerCfg.Partitioner != "" {
		configMap["partitioner"] = producerCfg.Partitioner
	}
	if producerCfg.MessageMaxBytes > 0 {
		configMap["message.max.bytes"] = producerCfg.MessageMaxBytes
	}

	return &configMap, nil
}

// Produce 按键发送消息；若发送失败会保留头部并写入 DLQ。
func (kp *Producer) Produce(topic string, key, value []byte, headers []kafka.Header) error {
	topicCopy := topic // 避免直接引用调用方栈上的变量
	return kp.p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topicCopy, Partition: int32(kafka.PartitionAny)},
		Key:            key,
		Value:          value,
		Headers:        headers,
	}, nil)
}

// Flush 在超时前阻塞等待未完成的发送任务。
// 并不是“立即发送”的动作，而是阻塞等待客户端缓冲区里尚未送达的消息在指定超时时间内完成投递
// Produce() 调用后消息进入本地队列，由后台线程异步发送到 broker；
// 如果某些消息还没拿到 delivery report（无论成功还是失败），调用 Flush(timeoutMs) 会阻塞等待，直到这些消息全部得到结果或超时；
// 返回值是仍然未完成的消息数量：0 表示都处理完了，>0 表示还有这么多条没有得到确认。
func (kp *Producer) Flush(timeoutMs int) int {
	return kp.p.Flush(timeoutMs)
}

// Close 关闭底层 producer，关闭前应先调用 Flush 确保消息写出。
func (kp *Producer) Close() {
	kp.p.Close()
}
