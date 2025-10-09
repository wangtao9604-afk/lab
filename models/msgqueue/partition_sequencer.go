package msgqueue

import (
	"sync"

	"qywx/models/message"
)

// partitionSequencer：分区入站顺序器
// 作用：在“分区内并发拉取”的情况下，保证同一分区的消息按 offset 顺序
// 下刷到调度层（Scheduler.dispatchInbound），避免乱序进入业务层。
// 原理：维护 next（下一条必须首先吐出的 offset）与 buf（乱序到达的缓存）；
// 只要 buf[next] 存在，就吐出并 next++，直到遇到缺口为止。
type partitionSequencer struct {
	mu      sync.Mutex
	next    int64
	buf     map[int64]*message.InboundMsg
	deliver message.DispatchInbound
}

// newPartitionSequencer 使用“分区起始 offset”初始化顺序器。
// 起始 offset 应在分区 Assigned 时确定（通常=“已提交 offset 的下一个”或“本次第一条消息 offset”）。
func newPartitionSequencer(startOffset int64, deliver message.DispatchInbound) *partitionSequencer {
	return &partitionSequencer{
		next:    startOffset,
		buf:     make(map[int64]*message.InboundMsg, 128),
		deliver: deliver,
	}
}

// Push 在任意并发下接收分区内的消息（offset 可能乱序到达），按 offset 顺序下刷。
func (s *partitionSequencer) Push(offset int64, in *message.InboundMsg) {
	s.mu.Lock()
	s.buf[offset] = in

	for {
		msg, ok := s.buf[s.next]
		if !ok {
			break // 缺口未补齐，停止推进
		}
		delete(s.buf, s.next)
		s.next++

		// 释放锁再下刷，避免长时间持锁导致阻塞/死锁
		s.mu.Unlock()
		s.deliver(msg)
		s.mu.Lock()
	}
	s.mu.Unlock()
}
