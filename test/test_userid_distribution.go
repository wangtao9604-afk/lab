package main

import (
	"fmt"
	"strings"
)

const (
	partitionCount = 16
	userIDCount    = 1000
)

// murmur2 实现 Kafka 使用的 MurmurHash2 算法
// 参考：https://github.com/aappleby/smhasher/blob/master/src/MurmurHash2.cpp
func murmur2(data []byte) uint32 {
	const (
		seed uint32 = 0x9747b28c
		m    uint32 = 0x5bd1e995
		r           = 24
	)

	length := len(data)
	h := seed ^ uint32(length)

	// Process 4-byte chunks
	for len(data) >= 4 {
		k := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24

		k *= m
		k ^= k >> r
		k *= m

		h *= m
		h ^= k

		data = data[4:]
	}

	// Process remaining bytes
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

// calculatePartition 计算 UserID 应该分配到哪个分区
// 使用 Kafka murmur2_random 分区器的算法
func calculatePartition(userID string, numPartitions int) int {
	hashValue := murmur2([]byte(userID))

	// 转换为有符号整数并处理负数（Java 风格）
	partition := int(int32(hashValue)) % numPartitions
	if partition < 0 {
		partition = -partition
	}
	return partition
}

// generateRandomSuffix 生成指定长度的随机字符串后缀
func generateRandomSuffix(length int, seed int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)

	// 使用确定性生成（基于 seed）
	for i := 0; i < length; i++ {
		result[i] = charset[(seed+i*7)%len(charset)]
	}
	return string(result)
}

// findSuffixForPartition 暴力搜索：找到能映射到指定分区的后缀
func findSuffixForPartition(prefix string, targetPartition int, startSeed int) (string, int) {
	suffixLen := 24 - len(prefix) // templateUserID 长度为 32，前缀 "wm6Gh8CQAAs" 11个字符

	for seed := startSeed; seed < startSeed+100000; seed++ {
		suffix := generateRandomSuffix(suffixLen, seed)
		userID := prefix + suffix
		partition := calculatePartition(userID, partitionCount)

		if partition == targetPartition {
			return suffix, seed + 1 // 返回下一个 seed
		}
	}

	// 未找到（理论上不应该发生）
	return "", startSeed
}

// bars 生成可视化条形图
func bars(count int) string {
	return strings.Repeat("█", count/10)
}

func main() {
	fmt.Println("===========================================")
	fmt.Println("  UserID Partition Distribution Test")
	fmt.Println("===========================================")
	fmt.Println()

	// 生成均匀分配的 UserIDs
	prefix := "wm6Gh8CQAAs"
	userIDs := make([]string, userIDCount)
	distribution := make(map[int]int)

	// 计算每个分区应该有多少用户
	usersPerPartition := userIDCount / partitionCount  // 62
	remainder := userIDCount % partitionCount          // 8

	fmt.Printf("Target: %d users across %d partitions\n", userIDCount, partitionCount)
	fmt.Printf("Base allocation: %d users/partition\n", usersPerPartition)
	fmt.Printf("Remainder: %d (first %d partitions get +1 user)\n\n", remainder, remainder)

	userIndex := 0
	seed := 0

	for partition := 0; partition < partitionCount; partition++ {
		// 前 remainder 个分区多分配 1 个用户
		count := usersPerPartition
		if partition < remainder {
			count++
		}

		fmt.Printf("Generating %d users for partition %2d...", count, partition)

		for i := 0; i < count; i++ {
			suffix, nextSeed := findSuffixForPartition(prefix, partition, seed)
			seed = nextSeed
			userIDs[userIndex] = prefix + suffix
			userIndex++
		}

		fmt.Println(" ✓")
	}

	fmt.Println()
	fmt.Println("Verifying distribution...")
	fmt.Println()

	// 验证分布
	for _, userID := range userIDs {
		partition := calculatePartition(userID, partitionCount)
		distribution[partition]++
	}

	// 打印分布统计
	fmt.Println("===========================================")
	fmt.Println("  Distribution Results")
	fmt.Println("===========================================")

	totalUsers := 0
	minCount := userIDCount
	maxCount := 0

	for i := 0; i < partitionCount; i++ {
		count := distribution[i]
		totalUsers += count
		if count < minCount {
			minCount = count
		}
		if count > maxCount {
			maxCount = count
		}

		expected := usersPerPartition
		if i < remainder {
			expected++
		}

		status := "✓"
		if count != expected {
			status = "✗"
		}

		fmt.Printf("Partition %2d: %3d users (expected %2d) %s %s\n",
			i, count, expected, status, bars(count))
	}

	fmt.Println("===========================================")
	fmt.Printf("Total users: %d\n", totalUsers)
	fmt.Printf("Min/Max: %d/%d (deviation: %d)\n", minCount, maxCount, maxCount-minCount)

	if totalUsers == userIDCount && maxCount-minCount <= 1 {
		fmt.Println("✓ Distribution is PERFECT!")
	} else {
		fmt.Println("✗ Distribution needs adjustment")
	}
}
