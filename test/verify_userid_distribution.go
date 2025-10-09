package main

import (
	"fmt"
	"strings"

	"qywx/infrastructures/stress"
)

const partitionCount = 16

// murmur2 实现 Kafka 使用的 MurmurHash2 算法（与 userids.go 中相同）
func murmur2(data []byte) uint32 {
	const (
		seed uint32 = 0x9747b28c
		m    uint32 = 0x5bd1e995
		r           = 24
	)

	length := len(data)
	h := seed ^ uint32(length)

	for len(data) >= 4 {
		k := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
		k *= m
		k ^= k >> r
		k *= m
		h *= m
		h ^= k
		data = data[4:]
	}

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

func calculatePartition(userID string, numPartitions int) int {
	hashValue := murmur2([]byte(userID))
	partition := int(int32(hashValue)) % numPartitions
	if partition < 0 {
		partition = -partition
	}
	return partition
}

func bars(count int) string {
	return strings.Repeat("█", count/10)
}

func main() {
	fmt.Println("===========================================")
	fmt.Println("  Verify UserID Distribution")
	fmt.Println("===========================================")
	fmt.Println()

	// 统计每个分区的用户数
	distribution := make(map[int]int)
	userIDCount := stress.GetUserIDCount()

	fmt.Printf("Total UserIDs: %d\n", userIDCount)
	fmt.Printf("Target Partitions: %d\n", partitionCount)
	fmt.Println()

	// 收集所有 UserID 并计算分区
	for i := 0; i < userIDCount; i++ {
		userID := stress.GetUserIDByIndex(i)
		partition := calculatePartition(userID, partitionCount)
		distribution[partition]++
	}

	// 打印分布统计
	fmt.Println("===========================================")
	fmt.Println("  Distribution Results")
	fmt.Println("===========================================")

	usersPerPartition := userIDCount / partitionCount
	remainder := userIDCount % partitionCount

	totalUsers := 0
	minCount := userIDCount
	maxCount := 0
	mismatches := 0

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
			mismatches++
		}

		fmt.Printf("Partition %2d: %3d users (expected %2d) %s %s\n",
			i, count, expected, status, bars(count))
	}

	fmt.Println("===========================================")
	fmt.Printf("Total users: %d\n", totalUsers)
	fmt.Printf("Min/Max: %d/%d (deviation: %d)\n", minCount, maxCount, maxCount-minCount)
	fmt.Printf("Mismatches: %d/%d partitions\n", mismatches, partitionCount)
	fmt.Println()

	if totalUsers == userIDCount && maxCount-minCount <= 1 && mismatches == 0 {
		fmt.Println("✓ Distribution is PERFECT!")
		fmt.Println("✓ All partitions have expected user counts!")
	} else if maxCount-minCount <= 5 {
		fmt.Println("⚠ Distribution is acceptable (deviation ≤ 5)")
	} else {
		fmt.Println("✗ Distribution needs adjustment")
	}
}
