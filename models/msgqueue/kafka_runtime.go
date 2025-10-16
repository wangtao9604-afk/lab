package msgqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	qlog "qywx/infrastructures/log"
	"qywx/infrastructures/mq/kmq"
	"qywx/infrastructures/utils"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/message"

	kafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type KafkaRuntime struct {
	consumer   *kmq.Consumer
	dlq        *kmq.DLQ
	seqM       sync.RWMutex
	seq        map[kmq.TopicPartitionKey]*partitionSequencer
	dispacher  message.DispatchInbound
	userPart   map[string]int32
	userPartMu sync.Mutex
}

func NewKafkaRuntime(brokers, groupID, dlqTopic string, dispacher message.DispatchInbound) (*KafkaRuntime, error) {
	rt := &KafkaRuntime{
		seq:       make(map[kmq.TopicPartitionKey]*partitionSequencer, 64),
		dispacher: dispacher,
		userPart:  make(map[string]int32, 1<<14),
	}

	hooks := kmq.LifecycleHooks{
		OnAssigned: func(topic string, partition int32, startOffset int64) {
			rt.seqM.Lock()
			key := kmq.TopicPartitionKey{Topic: topic, Partition: partition}
			if _, ok := rt.seq[key]; !ok {
				rt.seq[key] = newPartitionSequencer(startOffset, rt.dispacher, partition)
			}
			rt.seqM.Unlock()
		},
		OnRevoked: func(topic string, partition int32) {
			rt.seqM.Lock()
			delete(rt.seq, kmq.TopicPartitionKey{Topic: topic, Partition: partition})
			rt.seqM.Unlock()
		},
		OnLost: func(topic string, partition int32) {
			rt.seqM.Lock()
			delete(rt.seq, kmq.TopicPartitionKey{Topic: topic, Partition: partition})
			rt.seqM.Unlock()
		},
	}

	c, err := kmq.NewConsumer(
		brokers, groupID,
		kmq.WithMaxInflightPerPartition(384),
		kmq.WithMaxInflightGlobal(32000),
		kmq.WithBatchCommit(5*time.Second, 200),
		kmq.WithLifecycleHooks(hooks),
	)
	if err != nil {
		return nil, fmt.Errorf("create consumer: %w", err)
	}
	rt.consumer = c

	dlqClientID := groupID
	if dlqClientID == "" {
		dlqClientID = "qywx-schedule-consumer"
	}

	dlq, err := kmq.NewDLQ(brokers, dlqClientID, dlqTopic)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("create dlq: %w", err)
	}
	rt.dlq = dlq

	return rt, nil
}

// Start 订阅并启动轮询
func (rt *KafkaRuntime) Start(ctx context.Context, topics []string) error {
	if err := rt.consumer.Subscribe(topics); err != nil {
		return fmt.Errorf("subscribe topics: %w", err)
	}
	go rt.consumer.PollAndDispatchWithAck(ctx, rt.handleWithAck, 100)
	return nil
}

func (rt *KafkaRuntime) Close() {
	if rt.consumer != nil {
		rt.consumer.Close()
	}
	if rt.dlq != nil {
		rt.dlq.Close()
	}
}

func (rt *KafkaRuntime) resolveSeqStart(tp kafka.TopicPartition, topic string, part int32, off int64) int64 {
	// 1) Committed（confluent 语义：下一条要读）
	if committed, err := rt.consumer.Committed([]kafka.TopicPartition{tp}, 5000); err == nil && len(committed) == 1 {
		co := committed[0].Offset
		if co >= 0 {
			return int64(co)
		}
	}
	// 2) 低水位
	if low, _, err := rt.consumer.QueryWatermarkOffsets(topic, part, 5000); err == nil {
		return low
	}
	// 2b) 低水位（缓存）
	if low2, _, err2 := rt.consumer.GetWatermarkOffsets(topic, part); err2 == nil && low2 >= 0 {
		return low2
	}
	// 3) 兜底：用当前 off（但要强告警）
	qlog.GetInstance().Sugar.Errorf("[SEQ] resolve start failed, fallback to off=%d for %s:%d", off, topic, part)
	return off
}

// handleWithAck：解包 → 查找分区顺序器 → 推入（由顺序器按 offset 顺序下刷）
func (rt *KafkaRuntime) handleWithAck(m *kafka.Message, ack func(bool)) {
	var kf kefu.KFRecvMessage
	if err := json.Unmarshal(m.Value, &kf); err != nil {
		_ = rt.dlq.Send(m.Key, json.RawMessage(m.Value), m.Headers)
		ack(true)
		return
	}

	uid := kf.ExternalUserID
	part := m.TopicPartition.Partition

	rt.userPartMu.Lock()
	if p0, ok := rt.userPart[uid]; !ok {
		rt.userPart[uid] = part
	} else if p0 != part {
		qlog.GetInstance().Sugar.Errorf("[PART-DRIFT] user=%s first_part=%d now_part=%d off=%d msg_id=%s",
			uid, p0, part, int64(m.TopicPartition.Offset), kf.MsgID)
	}
	rt.userPartMu.Unlock()

	in := &message.InboundMsg{Msg: &kf, Ack: ack, PolledAt: utils.Now()}

	partition := m.TopicPartition.Partition
	topic := ""
	if m.TopicPartition.Topic != nil {
		topic = *m.TopicPartition.Topic
	}
	key := kmq.TopicPartitionKey{Topic: topic, Partition: partition}
	rt.seqM.RLock()
	sq := rt.seq[key]
	rt.seqM.RUnlock()

	if sq == nil {
		// 这里自旋等 OnAssigned 安装 sequencer（通常几十毫秒内就绪）
		const maxWaitIters = 100
		const waitStep = 10 * time.Millisecond
		for i := 0; i < maxWaitIters; i++ {
			time.Sleep(waitStep)
			rt.seqM.RLock()
			sq = rt.seq[key]
			rt.seqM.RUnlock()
			if sq != nil {
				break
			}
		}

		if sq == nil {
			// 兜底：用“与 OnAssigned 一致”的规则计算 start（不要用当前 off）
			off := int64(m.TopicPartition.Offset)
			tp := kafka.TopicPartition{Topic: &topic, Partition: part}
			start := rt.resolveSeqStart(tp, topic, part, off)

			qlog.GetInstance().Sugar.Warnf("[SEQ] Lazy-create sequencer at %s:%d start=%d (msg.off=%d)",
				topic, part, start, off)

			rt.seqM.Lock()
			if sq = rt.seq[key]; sq == nil {
				sq = newPartitionSequencer(start, rt.dispacher, part)
				rt.seq[key] = sq
			}
			rt.seqM.Unlock()
		}
	}
	sq.Push(int64(m.TopicPartition.Offset), in)
}
