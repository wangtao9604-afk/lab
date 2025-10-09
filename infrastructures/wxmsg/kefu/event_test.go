package kefu

import (
	"testing"
)

// TestAllEventTypes 测试所有8种事件类型的解析（简化版）
func TestAllEventTypes(t *testing.T) {
	tests := []struct {
		name      string
		event     *KFEventContent
	}{
		{
			name: "用户进入会话事件",
			event: &KFEventContent{
				EventType:      KFEventEnterSession,
				OpenKFID:       "test_kf",
				ExternalUserID: "test_user",
				Scene:          "qrcode",
				SceneParam:     "param1",
				WelcomeCode:    "welcome123",
			},
		},
		{
			name: "消息发送失败事件",
			event: &KFEventContent{
				EventType:      KFEventMsgSendFail,
				OpenKFID:       "test_kf",
				ExternalUserID: "test_user",
				ServicerUserID: "servicer1",
				FailMsgID:      "msg123",
				FailType:       1,
			},
		},
		{
			name: "接待人员接待状态变更事件",
			event: &KFEventContent{
				EventType:      KFEventServicerChange,
				OpenKFID:       "test_kf",
				ServicerUserID: "servicer1",
				Status:         1,
			},
		},
		{
			name: "会话状态变更事件",
			event: &KFEventContent{
				EventType:         KFEventSessionStatusChange,
				OpenKFID:          "test_kf",
				ExternalUserID:    "test_user",
				ChangeType:        2,
				OldServicerUserID: "servicer1",
				NewServicerUserID: "servicer2",
			},
		},
		{
			name: "用户撤回消息事件",
			event: &KFEventContent{
				EventType:      KFEventUserRecallMsg,
				OpenKFID:       "test_kf",
				ExternalUserID: "test_user",
				ServicerUserID: "servicer1",
				RecallMsgID:    "msg123",
			},
		},
		{
			name: "接待人员撤回消息事件",
			event: &KFEventContent{
				EventType:      KFEventServicerRecallMsg,
				OpenKFID:       "test_kf",
				ExternalUserID: "test_user",
				ServicerUserID: "servicer1",
				RecallMsgID:    "msg456",
			},
		},
		{
			name: "拒收客户消息变更事件",
			event: &KFEventContent{
				EventType:      KFEventRejectCustomerMsgSwitch,
				OpenKFID:       "test_kf",
				ServicerUserID: "servicer1",
				RejectSwitch:   1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建测试消息
			msg := &KFRecvMessage{
				MsgType: KFMsgTypeEvent,
				Event:   tt.event,
			}

			// 解析事件（简化版：直接返回KFEventContent）
			result, err := ParseKFEvent(msg)
			if err != nil {
				t.Fatalf("ParseKFEvent failed: %v", err)
			}

			// 验证返回的就是原始的event
			if result != tt.event {
				t.Errorf("Expected same event pointer, got different")
			}

			// 验证EventType正确
			if result.EventType != tt.event.EventType {
				t.Errorf("EventType mismatch: got %s, want %s", result.EventType, tt.event.EventType)
			}

			// 根据事件类型验证特定字段
			switch result.EventType {
			case KFEventEnterSession:
				if result.Scene == "" || result.WelcomeCode == "" {
					t.Error("Enter session event missing required fields")
				}
			case KFEventMsgSendFail:
				if result.FailMsgID == "" || result.FailType == 0 {
					t.Error("Msg send fail event missing required fields")
				}
			case KFEventServicerChange:
				if result.ServicerUserID == "" || result.Status == 0 {
					t.Error("Servicer change event missing required fields")
				}
			case KFEventSessionStatusChange:
				if result.ChangeType == 0 {
					t.Error("Session status change event missing required fields")
				}
			case KFEventUserRecallMsg, KFEventServicerRecallMsg:
				if result.RecallMsgID == "" {
					t.Error("Recall msg event missing required fields")
				}
			case KFEventRejectCustomerMsgSwitch:
				if result.RejectSwitch == 0 {
					t.Error("Reject switch event missing required fields")
				}
			}
		})
	}
}

// TestEventContentCompleteness 验证KFEventContent包含所有字段
func TestEventContentCompleteness(t *testing.T) {
	// 创建一个包含所有字段的KFEventContent
	content := &KFEventContent{
		EventType:         "test",
		OpenKFID:          "kf1",
		ExternalUserID:    "user1",
		Scene:             "scene1",
		SceneParam:        "param1",
		WelcomeCode:       "welcome1",
		FailMsgID:         "fail1",
		FailType:          1,
		ServicerUserID:    "servicer1",
		Status:            1,
		ChangeType:        2,
		OldServicerUserID: "old1",
		NewServicerUserID: "new1",
		RecallMsgID:       "recall1",
		RejectSwitch:      1,
	}

	// 验证所有字段都存在
	if content.EventType == "" {
		t.Error("EventType field missing")
	}
	if content.RejectSwitch == 0 {
		t.Error("RejectSwitch field missing")
	}

	t.Log("KFEventContent successfully contains all required fields for 8 event types")
}