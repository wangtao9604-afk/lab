package kmq

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	qconfig "qywx/infrastructures/config"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// DLQ 是 Dead-Letter Queue 的轻量封装，沿用主 producer 的分区器，便于回放时命中原分区。
type DLQ struct {
	p     *Producer
	topic string
}

type dlqEnvelope struct {
	OriginalTopic     string          `json:"original_topic,omitempty"`
	OriginalPartition int32           `json:"original_partition,omitempty"`
	OriginalOffset    int64           `json:"original_offset,omitempty"`
	Reason            string          `json:"reason"`
	FailedAtUnix      int64           `json:"failed_at_unix"`
	Message           json.RawMessage `json:"message"`
}

// NewDLQ 以 clientID 后缀创建专用的 DLQ producer。
func NewDLQ(brokers, clientID, topic string) (*DLQ, error) {
	cfg := qconfig.GetInstance().Kafka

	// 根据 clientID 选择使用 Chat 或 Recorder DLQ 配置
	var dlqCfg qconfig.KafkaDLQConfig
	if clientID != "" && (strings.Contains(clientID, "recorder") || strings.Contains(clientID, "Recorder")) {
		dlqCfg = cfg.RecorderDLQ
	} else {
		dlqCfg = cfg.ChatDLQ
	}

	effectiveTopic := topic
	if effectiveTopic == "" {
		effectiveTopic = dlqCfg.Topic
	}
	if effectiveTopic == "" {
		return nil, fmt.Errorf("kafka dlq: topic not configured")
	}

	dlqClientID := dlqCfg.ClientID
	if dlqClientID == "" {
		baseID := clientID
		if baseID == "" {
			// 根据 clientID 选择使用 Chat 或 Recorder Producer 配置
			if clientID != "" && (strings.Contains(clientID, "recorder") || strings.Contains(clientID, "Recorder")) {
				baseID = cfg.Producer.Recorder.ClientID
			} else {
				baseID = cfg.Producer.Chat.ClientID
			}
		}
		if baseID == "" {
			return nil, fmt.Errorf("kafka dlq: client id not configured")
		}

		suffix := dlqCfg.ClientIDSuffix
		if suffix == "" {
			suffix = "-dlq"
		}
		dlqClientID = baseID + suffix
	}

	p, err := NewProducer(brokers, dlqClientID)
	if err != nil {
		return nil, err
	}
	return &DLQ{p: p, topic: effectiveTopic}, nil
}

// Send 将载荷序列化为 JSON 并写入 DLQ；调用者应把成功写入视为“已处理”，用于推进提交闸门。
func (d *DLQ) Send(key []byte, v interface{}, headers []kafka.Header) error {
	payload, err := marshalDLQMessage(v)
	if err != nil {
		return err
	}

	env := dlqEnvelope{
		Reason:       "kmq_dlq_send",
		FailedAtUnix: time.Now().Unix(),
		Message:      payload,
	}

	return d.produceEnvelope(key, env, headers, "kmq-client")
}

// SendWithContext 将原始 Kafka message 与失败原因封装写入 DLQ。
func (d *DLQ) SendWithContext(meta *kafka.Message, reason string) error {
	if meta == nil {
		return fmt.Errorf("dlq send: meta message is nil")
	}

	env := dlqEnvelope{
		Reason:       reason,
		FailedAtUnix: time.Now().Unix(),
		Message:      json.RawMessage(append([]byte(nil), meta.Value...)),
	}

	if meta.TopicPartition.Topic != nil {
		env.OriginalTopic = *meta.TopicPartition.Topic
	}
	env.OriginalPartition = meta.TopicPartition.Partition
	if offset := meta.TopicPartition.Offset; offset >= 0 {
		env.OriginalOffset = int64(offset)
	}

	headers := cloneHeaders(meta.Headers)

	return d.produceEnvelope(meta.Key, env, headers, "kmq-producer")
}

// WrapDLQPayload 构造通用 DLQ 载荷，便于业务层在缺少 Kafka context 时保持格式统一。
func WrapDLQPayload(key, value []byte, headers []kafka.Header, reason string) ([]byte, []kafka.Header, error) {
	env := dlqEnvelope{
		Reason:       reason,
		FailedAtUnix: time.Now().Unix(),
		Message:      json.RawMessage(append([]byte(nil), value...)),
	}

	payload, err := json.Marshal(&env)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal DLQ envelope: %w", err)
	}

	mergedHeaders := appendDLQHeaders(headers, "kmq-client", env.FailedAtUnix)

	return payload, mergedHeaders, nil
}

func (d *DLQ) produceEnvelope(key []byte, env dlqEnvelope, headers []kafka.Header, source string) error {
	payload, err := json.Marshal(&env)
	if err != nil {
		return fmt.Errorf("marshal DLQ envelope: %w", err)
	}

	mergedHeaders := appendDLQHeaders(headers, source, env.FailedAtUnix)

	if err := d.p.Produce(d.topic, key, payload, mergedHeaders); err != nil {
		return err
	}

	reportTopic := env.OriginalTopic
	if reportTopic == "" {
		reportTopic = d.topic
	}
	ReportDLQ(reportTopic, componentFromSource(source))

	return nil
}

func componentFromSource(source string) string {
	switch source {
	case "kmq-producer":
		return "producer"
	default:
		return "consumer"
	}
}

func marshalDLQMessage(v interface{}) (json.RawMessage, error) {
	switch typed := v.(type) {
	case nil:
		return json.RawMessage("null"), nil
	case json.RawMessage:
		return append(json.RawMessage(nil), typed...), nil
	case []byte:
		copied := append([]byte(nil), typed...)
		if len(copied) == 0 {
			return json.RawMessage("null"), nil
		}
		if json.Valid(copied) {
			return json.RawMessage(copied), nil
		}
		// Fallback to base64 encoding via JSON string
		b, err := json.Marshal(copied)
		if err != nil {
			return nil, fmt.Errorf("marshal DLQ payload bytes: %w", err)
		}
		return json.RawMessage(b), nil
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal DLQ payload: %w", err)
		}
		return json.RawMessage(b), nil
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

func appendDLQHeaders(headers []kafka.Header, source string, ts int64) []kafka.Header {
	base := cloneHeaders(headers)
	return append(base,
		kafka.Header{Key: "dlq_source", Value: []byte(source)},
		kafka.Header{Key: "dlq_timestamp", Value: []byte(strconv.FormatInt(ts, 10))},
	)
}

// Flush 在指定时间内等待 DLQ 剩余消息发送完成。
func (d *DLQ) Flush(timeoutMs int) int {
	return d.p.Flush(timeoutMs)
}

// Close 在关闭前自动 Flush，确保 DLQ 消息不丢失。
func (d *DLQ) Close() {
	d.p.Flush(5000)
	d.p.Close()
}
