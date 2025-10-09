package main

import (
	"fmt"

	"qywx/infrastructures/stress"
)

func main() {
	// 模拟生成 10 次消息，检查 UserID 的唯一性
	fmt.Println("Simulating message generation...")
	fmt.Println()

	// 收集所有生成的 UserID
	userIDSet := make(map[string]int)  // userID -> count

	// 模拟 10 次请求
	rounds := 10
	messagesPerRound := 1000

	for round := 1; round <= rounds; round++ {
		fmt.Printf("Round %d: ", round)

		// 模拟每批 1000 条消息
		for i := 0; i < messagesPerRound; i++ {
			userID := stress.GetUserIDByIndex(i)
			msgID := stress.GetNextMsgID(userID)

			userIDSet[userID]++

			// 验证 MsgID 的递增性
			expectedMsgID := fmt.Sprintf("%d", round)
			if msgID != expectedMsgID {
				fmt.Printf("\n  ✗ UserID[%d] = %s, Expected MsgID=%s, Got=%s\n",
					i, userID, expectedMsgID, msgID)
			}
		}

		fmt.Printf("✓ Generated 1000 messages\n")
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  Analysis")
	fmt.Println("========================================")

	// 统计唯一 UserID 数量
	uniqueCount := len(userIDSet)
	fmt.Printf("Unique UserIDs: %d\n", uniqueCount)
	fmt.Printf("Total messages: %d\n", rounds * messagesPerRound)
	fmt.Println()

	// 检查每个 UserID 的消息数
	minCount := rounds
	maxCount := 0

	for _, count := range userIDSet {
		if count < minCount {
			minCount = count
		}
		if count > maxCount {
			maxCount = count
		}
	}

	fmt.Printf("Messages per UserID:\n")
	fmt.Printf("  Min: %d\n", minCount)
	fmt.Printf("  Max: %d\n", maxCount)
	fmt.Printf("  Expected: %d (rounds)\n", rounds)
	fmt.Println()

	// 检查是否所有 UserID 都收到了相同数量的消息
	if minCount == maxCount && maxCount == rounds {
		fmt.Println("✓ PASS: All UserIDs received exactly", rounds, "messages")
	} else {
		fmt.Println("✗ FAIL: UserIDs received different message counts")

		// 显示异常的 UserID
		fmt.Println()
		fmt.Println("Abnormal UserIDs:")
		for userID, count := range userIDSet {
			if count != rounds {
				fmt.Printf("  %s: %d messages (expected %d)\n", userID, count, rounds)
			}
		}
	}

	if uniqueCount != 1000 {
		fmt.Printf("\n✗ FAIL: Expected 1000 unique UserIDs, got %d\n", uniqueCount)
	} else {
		fmt.Println("✓ PASS: Exactly 1000 unique UserIDs")
	}
}
