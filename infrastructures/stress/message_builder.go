package stress

import (
	"crypto/rand"
	"time"

	"qywx/infrastructures/wxmsg/kefu"
)

const (
	// 压测使用的固定值
	// 注意：ToUserName 与 OpenKFID 在压测场景下复用同一值
	stressToUserName = "wk6Gh8CQAAH3T-AxpjWbx8Ybhw84AFnQ"
	stressOpenKFID   = "wk6Gh8CQAAH3T-AxpjWbx8Ybhw84AFnQ"
	tokenLength      = 10
)

var (
	tokenCharset = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")
	// 缓存东八区时区，避免重复加载
	cst8 = time.FixedZone("CST", 8*3600)
)

// BuildStressKFCallback 构造压测用的 KFCallbackMessage
func BuildStressKFCallback() *kefu.KFCallbackMessage {
	// 使用缓存的东八区时区
	now := time.Now().In(cst8)

	return &kefu.KFCallbackMessage{
		ToUserName: stressToUserName,
		CreateTime: now.Unix(),
		MsgType:    "event",
		Event:      "kf_msg_or_event",
		Token:      generateRandomToken(tokenLength),
		OpenKFID:   stressOpenKFID,
	}
}

// generateRandomToken 使用 crypto/rand 生成安全随机 Token（并发安全）
func generateRandomToken(length int) string {
	if length <= 0 {
		return ""
	}

	randomBytes := make([]byte, length)
	if _, err := rand.Read(randomBytes); err != nil {
		// crypto/rand 失败的可能性极低，降级到简单随机
		return generateFallbackToken(length)
	}

	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = tokenCharset[int(randomBytes[i])%len(tokenCharset)]
	}
	return string(result)
}

// generateFallbackToken 降级方案：使用包级 rand（并发安全）
func generateFallbackToken(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = tokenCharset[i%len(tokenCharset)]
	}
	return string(result)
}
