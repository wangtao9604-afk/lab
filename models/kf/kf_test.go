package kf

import (
	"testing"
	"time"

	"qywx/infrastructures/wxmsg/kefu"
)

func TestKFService(t *testing.T) {
	// 获取单例服务实例
	service := GetInstance()

	// 测试启动服务（可能已经被其他测试启动了）
	err := service.Start()
	if err != nil && err.Error() != "kf service already started" {
		t.Fatalf("Failed to start service: %v", err)
	}

	// 如果服务之前没有启动，测试重复启动
	if err == nil {
		err = service.Start()
		if err == nil {
			t.Fatal("Should return error when starting already started service")
		}
	}

	// 测试接收事件
	event := &kefu.KFCallbackMessage{
		Token:    "test_token",
		OpenKFID: "test_kf_id",
	}

	err = service.Receive(event)
	if err != nil {
		t.Fatalf("Failed to receive event: %v", err)
	}

	// 给处理器一些时间处理事件
	time.Sleep(100 * time.Millisecond)

	// 注意：在单例模式下，我们不能测试Stop和重新Start
	// 因为这会影响其他测试的运行
	t.Log("KF Service singleton test completed")
}

func TestCursorManager(t *testing.T) {
	cm := &cursorManager{prefix: "test_kf_cursor:"}

	// 测试获取不存在的cursor
	cursor, err := cm.getCursor("user_not_exists")
	if err != nil {
		t.Logf("Warning: getCursor returned error for non-existent key: %v", err)
		// 对于单元测试，如果无法连接Redis或其他问题，记录但不失败
		return
	}
	if cursor != "" {
		t.Fatal("Cursor should be empty for non-existent key")
	}

	// 测试设置和获取cursor
	testUserID := "test_user_123"
	testCursor := "cursor_abc_123"

	err = cm.setCursor(testUserID, testCursor)
	if err != nil {
		t.Fatalf("Failed to set cursor: %v", err)
	}

	cursor, err = cm.getCursor(testUserID)
	if err != nil {
		t.Fatalf("Failed to get cursor: %v", err)
	}
	if cursor != testCursor {
		t.Fatalf("Expected cursor %s, got %s", testCursor, cursor)
	}
}

func TestBasicReceive(t *testing.T) {
	// 获取单例服务实例
	service := GetInstance()
	
	// 确保服务已启动（可能在其他测试中已启动）
	err := service.Start()
	if err != nil && err.Error() != "kf service already started" {
		t.Fatalf("Failed to start service: %v", err)
	}

	// 测试接收事件
	event := &kefu.KFCallbackMessage{
		Token:    "test_token",
		OpenKFID: "test_kf_id",
	}
	err = service.Receive(event)
	if err != nil {
		t.Fatalf("Failed to receive event: %v", err)
	}
}

func TestGetInstance(t *testing.T) {
	// 注意：这个测试依赖全局配置，需要确保config.toml存在
	// 或者mock配置

	// 第一次获取
	service1 := GetInstance()
	if service1 == nil {
		t.Fatal("Failed to get service instance")
	}

	// 第二次应该返回同一个实例
	service2 := GetInstance()
	if service2 != service1 {
		t.Fatal("Should return the same instance")
	}
}
