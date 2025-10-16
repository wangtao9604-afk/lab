package stress

import (
	"time"

	"qywx/infrastructures/wxmsg/kefu"
)

const (
	// 消息生成常量
	messageBatchSize  = 5000 // 每批生成 5000 条消息
	textContentLength = 50   // 每条消息 50 个汉字
)

// cst8 在 message_builder.go 中定义并共享

// SimulateKFMessages 模拟生成一批客服消息
// 返回包含 1000 条消息的 KFSyncMsgResponse
// 每条消息对应一个不同的 UserID，MsgID 自动递增
func SimulateKFMessages() *kefu.KFSyncMsgResponse {
	// 使用缓存的东八区时区（与 message_builder.go 共享）
	now := time.Now().In(cst8)

	msgList := make([]kefu.KFRecvMessage, messageBatchSize)

	for i := 0; i < messageBatchSize; i++ {
		// 轮询选择 UserID（0-999）
		userID := GetUserIDByIndex(i)

		// 为该 UserID 生成递增的 MsgID
		msgID := GetNextMsgID(userID)

		// 生成随机汉字内容
		content := GenerateChineseText(textContentLength)

		msgList[i] = kefu.KFRecvMessage{
			MsgID:          msgID,
			OpenKFID:       stressOpenKFID,
			ExternalUserID: userID,
			SendTime:       now.Unix(),
			Origin:         3,
			ServicerUserID: "",
			MsgType:        "text",
			Text: &kefu.KFTextContent{
				Content: content,
				MenuID:  "",
			},
			// Image, Voice, Video, File, Location, Link, Event 全部为 nil
			Image:    nil,
			Voice:    nil,
			Video:    nil,
			File:     nil,
			Location: nil,
			Link:     nil,
			Event:    nil,
		}
	}

	return &kefu.KFSyncMsgResponse{
		ErrCode:    0,
		ErrMsg:     "",
		NextCursor: "",
		HasMore:    0,
		MsgList:    msgList,
	}
}
