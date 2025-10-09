package msgqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"qywx/infrastructures/mq/kmq"
	"qywx/infrastructures/utils"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/message"

	kafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type KafkaRuntime struct {
	consumer  *kmq.Consumer
	dlq       *kmq.DLQ
	seqM      sync.RWMutex
	seq       map[kmq.TopicPartitionKey]*partitionSequencer
	dispacher message.DispatchInbound
}

func NewKafkaRuntime(brokers, groupID, dlqTopic string, dispacher message.DispatchInbound) (*KafkaRuntime, error) {
	rt := &KafkaRuntime{
		seq:       make(map[kmq.TopicPartitionKey]*partitionSequencer, 64),
		dispacher: dispacher,
	}

	hooks := kmq.LifecycleHooks{
		OnAssigned: func(topic string, partition int32, startOffset int64) {
			rt.seqM.Lock()
			key := kmq.TopicPartitionKey{Topic: topic, Partition: partition}
			if _, ok := rt.seq[key]; !ok {
				rt.seq[key] = newPartitionSequencer(startOffset, rt.dispacher)
			}
			rt.seqM.Unlock()
		},
		OnRevoked: func(topic string, partition int32) {
			rt.seqM.Lock()
			delete(rt.seq, kmq.TopicPartitionKey{Topic: topic, Partition: partition})
			rt.seqM.Unlock()
		},
	}

	c, err := kmq.NewConsumer(
		brokers, groupID,
		kmq.WithMaxInflightPerPartition(32),
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

// handleWithAck：解包 → 查找分区顺序器 → 推入（由顺序器按 offset 顺序下刷）
func (rt *KafkaRuntime) handleWithAck(m *kafka.Message, ack func(bool)) {
	var kf kefu.KFRecvMessage
	if err := json.Unmarshal(m.Value, &kf); err != nil {
		_ = rt.dlq.Send(m.Key, json.RawMessage(m.Value), m.Headers)
		ack(true)
		return
	}

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
		rt.seqM.Lock()
		if sq = rt.seq[key]; sq == nil {
			start := int64(m.TopicPartition.Offset)
			sq = newPartitionSequencer(start, rt.dispacher)
			rt.seq[key] = sq
		}
		rt.seqM.Unlock()
	}

	sq.Push(int64(m.TopicPartition.Offset), in)
}
