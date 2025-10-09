package schedule

import "testing"

// TestGetInstanceSingleton 测试单例模式
func TestGetInstanceSingleton(t *testing.T) {
	// 第一次获取实例
	instance1 := GetInstance()
	if instance1 == nil {
		t.Fatal("GetInstance() returned nil on first call")
	}

	// 第二次获取实例
	instance2 := GetInstance()
	if instance2 == nil {
		t.Fatal("GetInstance() returned nil on second call")
	}

	// 验证两次获取的是同一个实例
	if instance1 != instance2 {
		t.Error("GetInstance() returned different instances, expected singleton")
	}

	// 多次调用验证
	for i := 0; i < 10; i++ {
		instance := GetInstance()
		if instance != instance1 {
			t.Errorf("GetInstance() call %d returned different instance", i+3)
		}
	}
}

// TestSchedulerLifecycle 测试调度器生命周期
func TestSchedulerLifecycle(t *testing.T) {
	scheduler := GetInstance()

	// 测试未启动时停止
	err := scheduler.Stop()
	if err == nil {
		t.Error("Expected error when stopping unstarted scheduler")
	}

	// 启动调度器
	err = scheduler.Start()
	if err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	// 测试重复启动
	err = scheduler.Start()
	if err == nil {
		t.Error("Expected error when starting already started scheduler")
	}

	// 正常停止
	err = scheduler.Stop()
	if err != nil {
		t.Errorf("Failed to stop scheduler: %v", err)
	}
}
