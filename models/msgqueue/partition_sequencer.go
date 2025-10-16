package msgqueue

import (
	"sync"

	"qywx/infrastructures/log"
	"qywx/models/message"
)

// partitionSequencer：分区入站顺序器
// 作用：在“分区内并发拉取”的情况下，保证同一分区的消息按 offset 顺序
// 下刷到调度层（Scheduler.dispatchInbound），避免乱序进入业务层。
// 原理：维护 next（下一条必须首先吐出的 offset）与 buf（乱序到达的缓存）；
// 只要 buf[next] 存在，就吐出并 next++，直到遇到缺口为止。
type partitionSequencer struct {
	mu        sync.Mutex
	next      int64
	buf       map[int64]*message.InboundMsg
	deliver   message.DispatchInbound
	partition int32
}

// newPartitionSequencer 使用“分区起始 offset”初始化顺序器。
// 起始 offset 应在分区 Assigned 时确定（通常=“已提交 offset 的下一个”或“本次第一条消息 offset”）。
func newPartitionSequencer(startOffset int64, deliver message.DispatchInbound, partition int32) *partitionSequencer {
	return &partitionSequencer{
		next:      startOffset,
		buf:       make(map[int64]*message.InboundMsg, 128),
		deliver:   deliver,
		partition: partition,
	}
}

// Push 在任意并发下接收分区内的消息（offset 可能乱序到达），按 offset 顺序下刷。
func (s *partitionSequencer) Push(offset int64, in *message.InboundMsg) {
	s.mu.Lock()

	// ---------- 观测：抓“晚到的更小 offset” ----------
	if offset < s.next {
		// 业务上下文（安全读取）
		var userID, msgID string
		if in != nil && in.Msg != nil {
			userID = in.Msg.ExternalUserID
			msgID = in.Msg.MsgID
		} else {
			userID = "<unknown>"
			msgID = "<unknown>"
		}

		// 这里不要解引用任何可能为 nil 的指针（例如 *kafka.TopicPartition.Topic）
		// 仅打印我们确定安全的字段
		log.GetInstance().Sugar.Warnw("[SEQ] late smaller offset",
			"next", s.next, // 期望下一个 offset
			"got", offset, // 实际到达的旧 offset
			"delta", s.next-offset, // 相差多少
			"user_id", userID, // 可选：帮助定位是否只是单个用户的偶发
			"msg_id", msgID,
			"partition", s.partition,
		)

		// 释放锁避免长持有
		s.mu.Unlock()
		if in != nil && in.Ack != nil {
			// 正常消费掉这条消息
			in.Ack(true)
		}
		return
	}
	// -----------------------------------------------

	s.buf[offset] = in
	log.GetInstance().Sugar.Infof("input offset: %d, partition: %d, next=%d", offset, s.partition, s.next)

	for {
		cur := s.next
		msg, ok := s.buf[cur]
		if !ok {
			break // 缺口未补齐，停止推进
		}
		delete(s.buf, cur)
		// 先释放锁再调用 deliver，但**不要**提前推进 next，消除竞态窗口
		s.mu.Unlock()
		// 此处同步调用，确保 msg[cur] 已经真正进入业务层
		s.deliver(msg)
		s.mu.Lock()
		// deliver 返回后再推进 next（cur -> cur+1）
		if s.next == cur {
			s.next = cur + 1
		}
	}
	s.mu.Unlock()
}
