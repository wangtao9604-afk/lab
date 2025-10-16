package schedule

import (
	"fmt"
	"strconv"
	"sync/atomic"
)

// SequenceChecker 消息顺序检测器
// 简化版本：每个 Processor 只处理单个 UserID，使用 atomic.Int64 跟踪期望序列号
type SequenceChecker struct {
	userID      string       // 当前 Processor 负责的 UserID
	expectedSeq atomic.Int64 // 期望的下一个 MsgID（原子操作）
	initialized atomic.Bool  // 是否已初始化
}

// NewSequenceChecker 创建新的顺序检测器
func NewSequenceChecker() *SequenceChecker {
	return &SequenceChecker{}
}

// Check 检查消息的 MsgID 是否符合顺序
// 返回 nil 表示顺序正确，返回 error 表示顺序违规
func (c *SequenceChecker) Check(userID string, msgID string) error {
	// 解析 MsgID 为整数
	actualSeq, err := strconv.ParseInt(msgID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid msgID format: %s (not an integer)", msgID)
	}

	// 首次调用：初始化期望值为当前实际序号
	if c.initialized.CompareAndSwap(false, true) {
		c.userID = userID
		// c.expectedSeq.Store(actualSeq)
		c.expectedSeq.Add(1)
	}

	// 读取期望值
	expectedSeq := c.expectedSeq.Load()

	if actualSeq != expectedSeq {
		// 顺序违规：期望值和实际值不匹配
		return &SequenceViolation{
			UserID:      userID,
			ExpectedSeq: expectedSeq,
			ActualSeq:   actualSeq,
		}
	}

	// 更新下一个期望值
	c.expectedSeq.Add(1)
	return nil
}

// Reset 重置序列号（测试用）
func (c *SequenceChecker) Reset() {
	c.expectedSeq.Store(0)
	c.initialized.Store(false)
	c.userID = ""
}

// SequenceViolation 顺序违规错误
type SequenceViolation struct {
	UserID      string
	ExpectedSeq int64
	ActualSeq   int64
}

func (e *SequenceViolation) Error() string {
	return fmt.Sprintf("sequence violation for user %s: expected %d, got %d",
		e.UserID, e.ExpectedSeq, e.ActualSeq)
}

// IsSequenceViolation 判断错误是否为顺序违规错误
func IsSequenceViolation(err error) (*SequenceViolation, bool) {
	if sv, ok := err.(*SequenceViolation); ok {
		return sv, true
	}
	return nil, false
}
