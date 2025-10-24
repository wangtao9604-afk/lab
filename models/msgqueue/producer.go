package msgqueue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	qconfig "qywx/infrastructures/config"
	qlog "qywx/infrastructures/log"
	"qywx/infrastructures/mq/kmq"
	"qywx/infrastructures/utils"
	"qywx/infrastructures/wxmsg/kefu"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// KafkaProducer 封装 kmq.Producer，提供发送客服消息到 Kafka 的统一入口。
type KafkaProducer struct {
	producer *kmq.Producer
	dlq      *kmq.DLQ
	topic    string
}

// KafkaProducerOptions 描述构建 KafkaProducer 所需的可选参数。
type KafkaProducerOptions struct {
	Brokers  string
	Topic    string
	ClientID string
	DLQTopic string
}

// NewKafkaProducer 根据配置构建 KafkaProducer；未提供的字段会回落到全局配置默认值。
func NewKafkaProducer(opts KafkaProducerOptions) (*KafkaProducer, error) {
	if opts.Topic == "" {
		return nil, fmt.Errorf("kafka producer: topic is required")
	}

	cfg := qconfig.GetInstance().Kafka
	brokers := opts.Brokers
	if brokers == "" {
		brokers = cfg.Brokers
	}
	clientID := opts.ClientID
	if clientID == "" {
		clientID = cfg.Producer.Chat.ClientID
	}
	if clientID == "" {
		clientID = "qywx-kafka-producer"
	}

	producer, err := kmq.NewProducer(brokers, clientID)
	if err != nil {
		return nil, fmt.Errorf("kafka producer: create producer failed: %w", err)
	}

	dlqTopic := opts.DLQTopic
	if dlqTopic == "" {
		dlqTopic = cfg.ChatDLQ.Topic
	}

	var dlq *kmq.DLQ
	if dlqTopic != "" {
		dlqClientID := cfg.ChatDLQ.ClientID
		if dlqClientID == "" {
			dlqClientID = clientID
			if suffix := cfg.ChatDLQ.ClientIDSuffix; suffix != "" {
				if !strings.HasSuffix(dlqClientID, suffix) {
					dlqClientID += suffix
				}
			}
		}

		dlq, err = kmq.NewDLQ(brokers, dlqClientID, dlqTopic)
		if err != nil {
			producer.Close()
			return nil, fmt.Errorf("kafka producer: create dlq failed: %w", err)
		}
		producer.AttachDLQ(dlq)
	}

	return &KafkaProducer{
		producer: producer,
		dlq:      dlq,
		topic:    opts.Topic,
	}, nil
}

// Produce 实现 RecorderProducer 接口，直接发送消息到指定 topic
func (kp *KafkaProducer) Produce(topic string, key, value []byte, headers []kafka.Header) error {
	if kp == nil || kp.producer == nil {
		return fmt.Errorf("kafka producer: nil producer")
	}
	return kp.producer.Produce(topic, key, value, headers)
}

// ProduceKFMessage 将客服消息写入 Kafka；按照 external_user_id 做分区键，消息体沿用企业微信原始结构。
func (kp *KafkaProducer) ProduceKFMessage(msg *kefu.KFRecvMessage) error {
	if kp == nil {
		return fmt.Errorf("kafka producer: nil producer")
	}
	if msg == nil {
		return fmt.Errorf("kafka producer: nil message")
	}

	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal kf message: %w", err)
	}

	keyStr := msg.ExternalUserID
	if len(keyStr) == 0 {
		if msg.MsgType == "text" {
			// 强约束：如果没有 ExternalUserID，就不要投到“聊天业务 Topic”
			return fmt.Errorf("produce message: external_user_id empty (skip chat topic)")
		} else if msg.MsgType == "event" {
			if msg.Event != nil {
				keyStr = msg.Event.ExternalUserID
			}
		}
	}

	// 兜底策略
	if len(keyStr) == 0 {
		keyStr = fmt.Sprintf("%d", utils.Now().UnixNano())
	}

	key := []byte(keyStr)

	headers := make([]kafka.Header, 0, 5)
	headers = append(headers,
		kafka.Header{Key: "kf_msg_type", Value: []byte(msg.MsgType)},
		kafka.Header{Key: "kf_origin", Value: []byte(strconv.Itoa(msg.Origin))},
	)
	if msg.OpenKFID != "" {
		headers = append(headers, kafka.Header{Key: "kf_open_kfid", Value: []byte(msg.OpenKFID)})
	}
	if msg.MsgID != "" {
		headers = append(headers, kafka.Header{Key: "kf_msg_id", Value: []byte(msg.MsgID)})
	}
	headers = append(headers, kafka.Header{Key: "kf_send_time", Value: []byte(strconv.FormatInt(msg.SendTime, 10))})

	if err := kp.producer.Produce(kp.topic, key, value, headers); err != nil {
		if kp.dlq != nil {
			meta := &kafka.Message{
				TopicPartition: kafka.TopicPartition{Topic: &kp.topic, Partition: kafka.PartitionAny},
				Key:            append([]byte(nil), key...),
				Value:          append([]byte(nil), value...),
				Headers:        cloneHeaders(headers),
			}
			if dlqErr := kp.dlq.SendWithContext(meta, "produce_failed"); dlqErr != nil {
				return fmt.Errorf("produce failed: %v, dlq failed: %w", err, dlqErr)
			}
			qlog.GetInstance().Sugar.Warnf("Kafka produce failed, message sent to DLQ: %v", err)
			return nil
		}
		return fmt.Errorf("produce message: %w", err)
	}

	return nil
}

// Close 刷新缓冲并关闭底层 producer/DLQ。
func (kp *KafkaProducer) Close() {
	if kp == nil {
		return
	}
	if kp.producer != nil {
		kp.producer.Flush(5000)
		kp.producer.Close()
	}
	if kp.dlq != nil {
		kp.dlq.Close() // Close里会先调Flush
	}
}

func cloneHeaders(headers []kafka.Header) []kafka.Header {
	if len(headers) == 0 {
		return nil
	}
	cloned := make([]kafka.Header, len(headers))
	copy(cloned, headers)
	return cloned
}
