package stress

import (
	"strconv"
	"sync"
)

// msgIDTracker 跟踪每个 UserID 的消息 ID
type msgIDTracker struct {
	mu      sync.Mutex
	nextIDs map[string]int64 // userID -> next msgID
}

var (
	tracker     *msgIDTracker
	trackerOnce sync.Once
)

// initTracker 初始化 MsgID 追踪器
func initTracker() {
	trackerOnce.Do(func() {
		tracker = &msgIDTracker{
			nextIDs: make(map[string]int64, userIDCount),
		}
	})
}

// GetNextMsgID 为指定 UserID 获取下一个 MsgID（原子递增）
// MsgID 格式为整数字符串："1", "2", "3", ...
func GetNextMsgID(userID string) string {
	initTracker()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	// 获取当前 ID（如果不存在则从 1 开始）
	currentID := tracker.nextIDs[userID]
	if currentID == 0 {
		currentID = 1
	}

	// 记录下一个 ID
	tracker.nextIDs[userID] = currentID + 1

	return strconv.FormatInt(currentID, 10)
}

// ResetMsgID 重置指定 UserID 的 MsgID（测试用）
func ResetMsgID(userID string) {
	initTracker()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	delete(tracker.nextIDs, userID)
}

// ResetAllMsgIDs 重置所有 UserID 的 MsgID（测试用）
func ResetAllMsgIDs() {
	initTracker()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	tracker.nextIDs = make(map[string]int64, userIDCount)
}
