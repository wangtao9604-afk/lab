package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	templateUserID = "wm6Gh8CQAAsJJJC8czVaRraSVQEWIzSw"
	userIDCount    = 5000
	partitionCount = 32
)

// UserIDData 存储 UserID 及其分区信息
type UserIDData struct {
	UserIDs            []string         `json:"userids"`
	PartitionMapping   map[string]int   `json:"partition_mapping"`   // userID -> partition
	PartitionDistribution map[int]int   `json:"partition_distribution"` // partition -> count
}

// murmur2 实现 Kafka 使用的 MurmurHash2 算法
func murmur2(data []byte) uint32 {
	const (
		seed uint32 = 0x9747b28c
		m    uint32 = 0x5bd1e995
		r           = 24
	)

	length := len(data)
	h := seed ^ uint32(length)

	// 处理 4 字节块
	for len(data) >= 4 {
		k := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24

		k *= m
		k ^= k >> r
		k *= m

		h *= m
		h ^= k

		data = data[4:]
	}

	// 处理剩余字节
	switch len(data) {
	case 3:
		h ^= uint32(data[2]) << 16
		fallthrough
	case 2:
		h ^= uint32(data[1]) << 8
		fallthrough
	case 1:
		h ^= uint32(data[0])
		h *= m
	}

	h ^= h >> 13
	h *= m
	h ^= h >> 15

	return h
}

// calculatePartition 计算 UserID 应该分配到哪个 Kafka 分区
func calculatePartition(userID string, numPartitions int) int {
	hashValue := murmur2([]byte(userID))

	partition := int(int32(hashValue)) % numPartitions
	if partition < 0 {
		partition = -partition
	}
	return partition
}

// generateSuffixForPartition 生成能够映射到指定分区的 UserID 后缀
func generateSuffixForPartition(prefix string, targetPartition int, startSeed uint64) (string, uint64) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	suffixLen := len(templateUserID) - len(prefix)

	seed := startSeed
	for attempts := 0; attempts < 1000000; attempts++ {
		// 使用 SplitMix64 算法生成后缀
		rng := seed
		suffix := make([]byte, suffixLen)
		for i := 0; i < suffixLen; i++ {
			rng += 0x9e3779b97f4a7c15
			z := rng
			z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
			z = (z ^ (z >> 27)) * 0x94d049bb133111eb
			z = z ^ (z >> 31)
			suffix[i] = charset[z%uint64(len(charset))]
		}

		userID := prefix + string(suffix)
		partition := calculatePartition(userID, partitionCount)

		if partition == targetPartition {
			return string(suffix), seed + 1
		}

		seed++
	}

	panic(fmt.Sprintf("failed to find suffix for partition %d after 1000000 attempts", targetPartition))
}

// generateUserIDs 生成 5000 个 UserID，确保均匀分配到 32 个 Kafka 分区
func generateUserIDs() *UserIDData {
	userIDs := make([]string, userIDCount)
	partitionMapping := make(map[string]int, userIDCount)
	partitionDistribution := make(map[int]int, partitionCount)

	prefix := "wm6Gh8CQAAs"

	// 计算每个分区应该分配的用户数
	usersPerPartition := userIDCount / partitionCount  // 62
	remainder := userIDCount % partitionCount          // 8

	userIndex := 0
	var seed uint64 = 0

	// 为每个分区生成指定数量的用户
	for partition := 0; partition < partitionCount; partition++ {
		// 前 remainder 个分区多分配 1 个用户
		count := usersPerPartition
		if partition < remainder {
			count++
		}

		fmt.Printf("Generating partition %2d: %2d users... ", partition, count)

		// 生成映射到当前分区的 UserIDs
		for i := 0; i < count; i++ {
			suffix, nextSeed := generateSuffixForPartition(prefix, partition, seed)
			seed = nextSeed
			userID := prefix + suffix
			userIDs[userIndex] = userID
			partitionMapping[userID] = partition
			partitionDistribution[partition]++
			userIndex++
		}

		fmt.Println("✓")
	}

	return &UserIDData{
		UserIDs:               userIDs,
		PartitionMapping:      partitionMapping,
		PartitionDistribution: partitionDistribution,
	}
}

func main() {
	fmt.Println("===========================================")
	fmt.Println("  Generate Stress Test UserIDs")
	fmt.Println("===========================================")
	fmt.Println()

	// 生成 UserIDs
	data := generateUserIDs()

	// 验证唯一性
	uniqueCheck := make(map[string]bool, userIDCount)
	for _, userID := range data.UserIDs {
		if uniqueCheck[userID] {
			panic(fmt.Sprintf("Duplicate UserID found: %s", userID))
		}
		uniqueCheck[userID] = true
	}
	fmt.Println()
	fmt.Printf("✓ Generated %d unique UserIDs\n", len(data.UserIDs))

	// 验证分区分布
	fmt.Println()
	fmt.Println("Partition Distribution:")
	usersPerPartition := userIDCount / partitionCount
	remainder := userIDCount % partitionCount
	for partition := 0; partition < partitionCount; partition++ {
		count := data.PartitionDistribution[partition]
		expected := usersPerPartition
		if partition < remainder {
			expected++
		}
		status := "✓"
		if count != expected {
			status = "✗"
		}
		fmt.Printf("  Partition %2d: %2d users (expected %2d) %s\n", partition, count, expected, status)
	}

	// 写入 JSON 文件
	outputPath := filepath.Join("app", "stress", "userids_5000.json")
	file, err := os.Create(outputPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to create file: %v", err))
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		panic(fmt.Sprintf("Failed to encode JSON: %v", err))
	}

	fmt.Println()
	fmt.Printf("✓ UserIDs saved to: %s\n", outputPath)
	fmt.Println()
	fmt.Println("===========================================")
}
