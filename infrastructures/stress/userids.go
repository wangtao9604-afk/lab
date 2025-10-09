package stress

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const (
	userIDCount    = 1000
	userIDFilePath = "/etc/qywx/userids.json" // UserID 预生成文件路径（固定绝对路径）
)

var (
	userIDs     []string
	userIDIndex int
	userIDMutex sync.Mutex
	initOnce    sync.Once
)

// UserIDData 存储 UserID 及其分区信息
type UserIDData struct {
	UserIDs               []string       `json:"userids"`
	PartitionMapping      map[string]int `json:"partition_mapping"`
	PartitionDistribution map[int]int    `json:"partition_distribution"`
}

// loadUserIDs 从固定路径 /etc/qywx/userids.json 加载预生成的 UserIDs
// 这确保每次压测使用完全相同的 UserID 集合，提高测试的可重复性
func loadUserIDs() error {
	// 读取 JSON 文件
	data, err := os.ReadFile(userIDFilePath)
	if err != nil {
		return fmt.Errorf("read userids file %s: %w", userIDFilePath, err)
	}

	// 解析 JSON
	var userData UserIDData
	if err := json.Unmarshal(data, &userData); err != nil {
		return fmt.Errorf("parse userids JSON: %w", err)
	}

	// 验证数据
	if len(userData.UserIDs) != userIDCount {
		return fmt.Errorf("invalid userids count: got %d, expected %d", len(userData.UserIDs), userIDCount)
	}

	// 加载到全局变量
	userIDs = userData.UserIDs

	return nil
}

// initUserIDs 初始化 UserIDs（从 JSON 文件加载）
func initUserIDs() {
	initOnce.Do(func() {
		if err := loadUserIDs(); err != nil {
			panic(fmt.Sprintf("Failed to load UserIDs: %v\n\nPlease ensure %s exists and contains 1000 valid UserIDs.\nTo generate: go run app/stress/generate_userids.go", err, userIDFilePath))
		}
	})
}

// GetNextUserID 获取下一个 UserID（轮询方式）
// 每 1000 次调用会重复一轮
func GetNextUserID() string {
	initUserIDs()

	userIDMutex.Lock()
	defer userIDMutex.Unlock()

	userID := userIDs[userIDIndex]
	userIDIndex = (userIDIndex + 1) % userIDCount
	return userID
}

// GetUserIDByIndex 根据索引获取 UserID（0-999）
// 用于批量生成消息时确保每个 UserID 对应一条消息
// 注意：索引越界会 panic，调用方应确保索引有效
func GetUserIDByIndex(index int) string {
	initUserIDs()

	if index < 0 || index >= userIDCount {
		// 在压测工具中使用 panic 可接受，因为索引由内部控制
		panic(fmt.Sprintf("index out of range: %d (must be 0-%d)", index, userIDCount-1))
	}
	return userIDs[index]
}

// GetUserIDCount 返回总的 UserID 数量
func GetUserIDCount() int {
	return userIDCount
}
