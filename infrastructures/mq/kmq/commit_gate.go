package kmq

import (
	"sync"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	qlog "qywx/infrastructures/log"
)

// offsetStorer 抽象 kafka.Consumer.StoreOffsets，方便单元测试。
type offsetStorer interface {
	StoreOffsets([]kafka.TopicPartition) ([]kafka.TopicPartition, error)
}

// offsetCommitter 抽象 kafka.Consumer.CommitOffsets，方便单元测试。
type offsetCommitter interface {
	CommitOffsets([]kafka.TopicPartition) ([]kafka.TopicPartition, error)
}

// PartitionCommitGate 负责控制同一分区内的连续提交：允许并行处理，但仅在更低偏移全部完成后才推进提交，确保顺序不被破坏。
type PartitionCommitGate struct {
	mu        sync.Mutex
	topic     string
	partition int32

	// nextCommit 表示尚未提交的最小偏移。
	nextCommit int64

	// done 保存已完成但待提交的偏移。
	done map[int64]struct{}

	// initialized 标记是否已根据真实起始偏移完成校准。
	initialized bool
}

// NewPartitionCommitGate 从指定起始偏移创建闸门，通常使用已提交偏移或水位作为起点。
func NewPartitionCommitGate(topic string, partition int32, startOffset int64) *PartitionCommitGate {
	return &PartitionCommitGate{
		topic:       topic,
		partition:   partition,
		nextCommit:  startOffset,
		done:        make(map[int64]struct{}),
		initialized: startOffset > 0,
	}
}

// Topic 返回闸门对应的主题。
func (g *PartitionCommitGate) Topic() string {
	return g.topic
}

// Partition 返回闸门对应的分区。
func (g *PartitionCommitGate) Partition() int32 {
	return g.partition
}

// EnsureInit 在第一条消息的偏移高于初始值时自动校准，防止分区从高水位恢复时闸门卡住。
func (g *PartitionCommitGate) EnsureInit(firstMsgOffset int64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.initialized && firstMsgOffset > g.nextCommit {
		prev := g.nextCommit
		g.nextCommit = firstMsgOffset
		g.initialized = true

		for offset := range g.done {
			if offset < firstMsgOffset {
				delete(g.done, offset)
			}
		}

		qlog.GetInstance().Sugar.Infof("提交闸门自动校准：%s:%d 从 %d 调整为 %d", g.topic, g.partition, prev, firstMsgOffset)
	}
}

// MarkDone 标记偏移已完成（成功或写入 DLQ）；当所有低于 nextCommit 的偏移都完成后，可继续推进提交。
func (g *PartitionCommitGate) MarkDone(offset int64) {
	g.mu.Lock()
	g.done[offset] = struct{}{}
	backlog := len(g.done)
	g.mu.Unlock()

	ReportGateBacklog(g.topic, g.partition, int64(backlog))
}

// GetBacklog 返回当前已完成但尚未提交的偏移数量，便于监控提交积压。
func (g *PartitionCommitGate) GetBacklog() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return int64(len(g.done))
}

// tryAdvance 在存在连续完成的偏移时推进 nextCommit，并返回最高的连续完成偏移；若无进展则返回 -1。
func (g *PartitionCommitGate) tryAdvance() int64 {
	g.mu.Lock()

	advanced := int64(-1)
	for {
		if _, ok := g.done[g.nextCommit]; ok {
			delete(g.done, g.nextCommit)
			advanced = g.nextCommit
			g.nextCommit++
			continue
		}
		break
	}

	backlog := len(g.done)
	g.mu.Unlock()

	if advanced >= 0 {
		ReportGateBacklog(g.topic, g.partition, int64(backlog))
	}

	return advanced
}

// StoreContiguous 调用 StoreOffsets 仅存储（不提交）连续完成的最大偏移 +1；返回已存储的偏移，若无进展则返回 -1。
func (g *PartitionCommitGate) StoreContiguous(st offsetStorer) (int64, error) {
	hi := g.tryAdvance()
	if hi < 0 {
		return -1, nil
	}

	tp := kafka.TopicPartition{
		Topic:     &g.topic,
		Partition: g.partition,
		Offset:    kafka.Offset(hi + 1),
	}
	_, err := st.StoreOffsets([]kafka.TopicPartition{tp})
	return hi + 1, err
}

// CommitContiguous 提交连续完成的最大偏移 +1；若无进展则直接返回，不执行提交。
func (g *PartitionCommitGate) CommitContiguous(cm offsetCommitter) error {
	hi := g.tryAdvance()
	if hi < 0 {
		return nil
	}

	tp := kafka.TopicPartition{
		Topic:     &g.topic,
		Partition: g.partition,
		Offset:    kafka.Offset(hi + 1),
	}
	_, err := cm.CommitOffsets([]kafka.TopicPartition{tp})
	return err
}
