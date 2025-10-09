package message

import (
	"time"

	"qywx/infrastructures/wxmsg/kefu"
)

// 统一消息包装，携带 ack 回调。
// 业务端在“业务完成点”（成功或已安全落 DLQ）处必须调用 Ack(true) 推进提交。
type InboundMsg struct {
	Msg      *kefu.KFRecvMessage
	Ack      func(success bool) // 必须调用一次；成功或已落DLQ后应为 true
	PolledAt time.Time          // 可选：用于指标（从 Poll 到入队耗时）
}

type DispatchInbound func(in *InboundMsg)
