package schedule

import (
	"strings"

	"qywx/infrastructures/ipang"
	"qywx/models/thirdpart/minimax"
)

// BuildQAPairs 从对话历史中构建问答对
// AI 消息作为 Q，用户消息作为 A（这是逻辑上正确的方向）
//
// contentValidator 可选的内容验证函数，用于过滤不合适的消息
// - 用于 recorder 时：contentValidator 为 nil，接受所有消息
// - 用于 Ipang qas 字段时：传入 isValidForIpang 过滤器
func BuildQAPairs(rawHistory []minimax.RawMsg, contentValidator func(string) bool) []ipang.QA {
	qas := make([]ipang.QA, 0, len(rawHistory)/2)

	// AI消息作为Q，用户消息作为A
	for i := 0; i < len(rawHistory)-1; {
		curr := rawHistory[i]
		next := rawHistory[i+1]

		if strings.EqualFold(curr.Source, "AI") && strings.EqualFold(next.Source, "用户") {
			q := strings.TrimSpace(curr.Content)
			a := strings.TrimSpace(next.Content)
			if q != "" && a != "" {
				// 如果提供了验证器，检查Q的内容是否有效
				if contentValidator == nil || contentValidator(q) {
					qas = append(qas, ipang.QA{
						Q: q,
						A: a,
					})
				}
			}
			i += 2
		} else {
			i++
		}
	}

	return qas
}
