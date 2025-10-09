package stress

import (
	"math/rand"
)

var (
	// 常用汉字范围 (U+4E00 到 U+9FA5)
	chineseStart = rune(0x4E00)
	chineseEnd   = rune(0x9FA5)
	chineseRange = int(chineseEnd - chineseStart + 1)
)

// GenerateChineseText 生成指定长度的随机汉字文本
// 使用包级 rand.Intn（并发安全）
func GenerateChineseText(length int) string {
	if length <= 0 {
		return ""
	}

	result := make([]rune, length)
	for i := 0; i < length; i++ {
		offset := rand.Intn(chineseRange) // 包级 rand 并发安全
		result[i] = chineseStart + rune(offset)
	}

	return string(result)
}
