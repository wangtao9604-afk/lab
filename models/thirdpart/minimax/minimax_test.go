package minimax

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestNewClient 测试客户端创建
func TestNewClient(t *testing.T) {
	// 测试默认配置创建
	client, err := NewClient()
	if err == nil {
		// 如果配置了API Key，应该成功
		if client == nil {
			t.Error("NewClient() returned nil client with valid config")
		}
	} else {
		// 如果未配置API Key，应该返回错误
		if err.Error() != "MiniMax API key is required" {
			t.Errorf("Expected 'MiniMax API key is required' error, got: %v", err)
		}
	}
}

// TestNewClientWithCustomConfig 测试使用自定义配置创建客户端
func TestNewClientWithCustomConfig(t *testing.T) {
	// 测试自定义配置创建（API Key从配置文件读取）
	client, err := NewClientWithCustomConfig("MiniMax-Text-01", 512, 0.8)
	if err == nil {
		// 如果配置了API Key，应该成功
		if client == nil {
			t.Error("NewClientWithCustomConfig() returned nil client with valid config")
		}
		
		// 验证自定义参数
		if client.GetModel() != "MiniMax-Text-01" {
			t.Errorf("Expected model MiniMax-Text-01, got %s", client.GetModel())
		}
		if client.GetMaxTokens() != 512 {
			t.Errorf("Expected max tokens 512, got %d", client.GetMaxTokens())
		}
		if client.GetTemperature() != 0.8 {
			t.Errorf("Expected temperature 0.8, got %f", client.GetTemperature())
		}
	} else {
		// 如果未配置API Key，应该返回错误
		if err.Error() != "MiniMax API key is required" {
			t.Errorf("Expected 'MiniMax API key is required' error, got: %v", err)
		}
	}
}

// TestValidateConfig 测试配置验证
func TestValidateConfig(t *testing.T) {
	// 创建一个测试客户端
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create test client (API key required in config): ", err)
	}

	// 验证配置应该通过
	err = client.ValidateConfig()
	if err != nil {
		t.Errorf("ValidateConfig failed: %v", err)
	}
}

// TestModelGetterSetter 测试模型的getter和setter
func TestModelGetterSetter(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create test client (API key required in config): ", err)
	}

	// 获取默认模型
	defaultModel := client.GetModel()
	if defaultModel == "" {
		t.Error("Default model should not be empty")
	}

	// 设置新模型
	newModel := "MiniMax-Text-01"
	client.SetModel(newModel)

	// 验证设置成功
	if client.GetModel() != newModel {
		t.Errorf("Expected model %s, got %s", newModel, client.GetModel())
	}

	// 恢复默认模型
	client.SetModel(defaultModel)
}

// TestTemperatureGetterSetter 测试温度参数的getter和setter
func TestTemperatureGetterSetter(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create test client (API key required in config): ", err)
	}

	// 获取默认温度
	defaultTemp := client.GetTemperature()
	if defaultTemp <= 0 {
		t.Error("Default temperature should be greater than 0")
	}

	// 设置新温度
	newTemp := 0.9
	client.SetTemperature(newTemp)

	// 验证设置成功
	if client.GetTemperature() != newTemp {
		t.Errorf("Expected temperature %f, got %f", newTemp, client.GetTemperature())
	}

	// 恢复默认温度
	client.SetTemperature(defaultTemp)
}

// TestMaxTokensGetterSetter 测试最大token数的getter和setter
func TestMaxTokensGetterSetter(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create test client (API key required in config): ", err)
	}

	// 获取默认最大token数
	defaultMaxTokens := client.GetMaxTokens()
	if defaultMaxTokens <= 0 {
		t.Error("Default max tokens should be greater than 0")
	}

	// 设置新的最大token数
	newMaxTokens := 2000
	client.SetMaxTokens(newMaxTokens)

	// 验证设置成功
	if client.GetMaxTokens() != newMaxTokens {
		t.Errorf("Expected max tokens %d, got %d", newMaxTokens, client.GetMaxTokens())
	}

	// 恢复默认值
	client.SetMaxTokens(defaultMaxTokens)
}

// TestMessageStructure 测试消息结构
func TestMessageStructure(t *testing.T) {
	messages := []Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant",
			Name:    "MiniMax AI",
		},
		{
			Role:    "user",
			Content: "Hello",
			Name:    "User",
		},
		{
			Role:    "assistant",
			Content: "Hi there!",
		},
	}

	// 验证消息结构
	if len(messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(messages))
	}

	// 验证角色
	expectedRoles := []string{"system", "user", "assistant"}
	for i, msg := range messages {
		if msg.Role != expectedRoles[i] {
			t.Errorf("Message %d: expected role %s, got %s", i, expectedRoles[i], msg.Role)
		}
	}
}

// TestMultiModalMessage 测试多模态消息创建
func TestMultiModalMessage(t *testing.T) {
	// 创建文本内容项
	textItem := CreateTextContentItem("这是一张图片的描述")
	if textItem.Type != "text" {
		t.Errorf("Expected text content type, got %s", textItem.Type)
	}
	if textItem.Text != "这是一张图片的描述" {
		t.Errorf("Expected text content, got %s", textItem.Text)
	}

	// 创建图片内容项
	imageURL := "https://example.com/image.jpg"
	imageItem := CreateImageContentItem(imageURL)
	if imageItem.Type != "image_url" {
		t.Errorf("Expected image_url content type, got %s", imageItem.Type)
	}
	if imageItem.ImageURL == nil || imageItem.ImageURL.URL != imageURL {
		t.Errorf("Expected image URL %s, got %+v", imageURL, imageItem.ImageURL)
	}

	// 创建多模态消息
	multiModalMessage := CreateMultiModalMessage("user", []ContentItem{textItem, imageItem})
	if multiModalMessage.Role != "user" {
		t.Errorf("Expected user role, got %s", multiModalMessage.Role)
	}

	// 验证Content是数组类型
	if contentItems, ok := multiModalMessage.Content.([]ContentItem); ok {
		if len(contentItems) != 2 {
			t.Errorf("Expected 2 content items, got %d", len(contentItems))
		}
	} else {
		t.Error("Expected Content to be []ContentItem")
	}
}

// TestCreateHelperFunctions 测试创建辅助函数
func TestCreateHelperFunctions(t *testing.T) {
	// 测试CreateTool
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "城市名称",
			},
		},
		"required": []string{"location"},
	}

	tool := CreateTool("get_weather", "获取天气信息", params)
	if tool.Type != "function" {
		t.Errorf("Expected function type, got %s", tool.Type)
	}
	if tool.Function.Name != "get_weather" {
		t.Errorf("Expected get_weather name, got %s", tool.Function.Name)
	}
	if tool.Function.Description != "获取天气信息" {
		t.Errorf("Expected description, got %s", tool.Function.Description)
	}

	// 测试CreateResponseFormat
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"result": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"result"},
	}

	respFormat := CreateResponseFormat("analysis_result", "分析结果", schema)
	if respFormat.Type != "json_schema" {
		t.Errorf("Expected json_schema type, got %s", respFormat.Type)
	}
	if respFormat.JSONSchema.Name != "analysis_result" {
		t.Errorf("Expected analysis_result name, got %s", respFormat.JSONSchema.Name)
	}

	// 测试CreateToolMessage
	toolMsg := CreateToolMessage("call_123", "天气数据")
	if toolMsg.Role != "tool" {
		t.Errorf("Expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_123" {
		t.Errorf("Expected call_123 tool call ID, got %s", toolMsg.ToolCallID)
	}
	if content, ok := toolMsg.Content.(string); !ok || content != "天气数据" {
		t.Errorf("Expected string content '天气数据', got %+v", toolMsg.Content)
	}
}

// TestChatCompletionRequest 测试请求结构
func TestChatCompletionRequest(t *testing.T) {
	req := &ChatCompletionRequest{
		Model: "MiniMax-M1",
		Messages: []Message{
			{Role: "user", Content: "Test message"},
		},
		Stream:      false,
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	// 验证请求字段
	if req.Model != "MiniMax-M1" {
		t.Errorf("Expected model MiniMax-M1, got %s", req.Model)
	}

	if len(req.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(req.Messages))
	}

	if req.Stream {
		t.Error("Stream should be false")
	}

	if req.MaxTokens != 1000 {
		t.Errorf("Expected max tokens 1000, got %d", req.MaxTokens)
	}

	if req.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %f", req.Temperature)
	}
}

// 集成测试（需要在配置文件中配置API Key才能运行）
// 运行方式: go test -v -run TestIntegration -tags=integration

// TestIntegrationSimpleChat 集成测试：简单对话
func TestIntegrationSimpleChat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 使用配置文件创建客户端
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create client from config (API key required): ", err)
	}

	ctx := context.Background()

	// 测试简单对话（优化版：更短问题，限制token）
	question := "我有干海参，想拿来炖鸡汤，该如何处理呢?"

	// 打印测试信息
	t.Logf("\n"+
		"============================================\n"+
		"📝 测试: SimpleChat (简单对话)\n"+
		"============================================\n"+
		"❓ 问题: %s\n"+
		"⏳ 等待响应...", question)

	startTime := time.Now()
	response, err := client.SimpleChat(ctx, question)
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("SimpleChat failed: %v", err)
	}

	if response == "" {
		t.Error("Expected non-empty response")
	}

	t.Logf("✅ 回答: %s\n"+
		"⏱️  耗时: %.2f秒\n"+
		"============================================",
		response, elapsed.Seconds())
}

// TestIntegrationChatCompletion 集成测试：完整对话
func TestIntegrationChatCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 使用配置文件创建客户端
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create client from config (API key required): ", err)
	}

	ctx := context.Background()

	systemPrompt := "你是一个有帮助的助手"
	userQuestion := "请用一句话解释什么是人工智能"

	messages := []Message{
		{
			Role:    "system",
			Content: systemPrompt,
			Name:    "MiniMax AI",
		},
		{
			Role:    "user",
			Content: userQuestion,
		},
	}

	// 打印测试信息
	t.Logf("\n"+
		"============================================\n"+
		"🎭 测试: ChatCompletion (完整对话)\n"+
		"============================================\n"+
		"🤖 系统提示: %s\n"+
		"❓ 用户问题: %s\n"+
		"⏳ 等待响应...", systemPrompt, userQuestion)

	startTime := time.Now()
	resp, err := client.ChatCompletion(ctx, messages)
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("ChatCompletion failed: %v", err)
	}

	// 验证响应结构
	if resp.ID == "" {
		t.Error("Expected non-empty response ID")
	}

	if len(resp.Choices) == 0 {
		t.Error("Expected at least one choice")
	}

	if resp.Choices[0].Message == nil {
		t.Error("Expected message in first choice")
	}

	// 安全地处理Content类型
	var contentStr string
	if content, ok := resp.Choices[0].Message.Content.(string); ok {
		contentStr = content
	} else {
		contentStr = fmt.Sprintf("Non-string content: %+v", resp.Choices[0].Message.Content)
	}

	t.Logf("✅ 回答: %s\n"+
		"📊 Token使用: 总计=%d, 提示=%d, 完成=%d\n"+
		"⏱️  耗时: %.2f秒\n"+
		"============================================",
		contentStr,
		resp.Usage.TotalTokens,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		elapsed.Seconds())
}

// TestIntegrationStream 集成测试：流式对话
func TestIntegrationStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 使用配置文件创建客户端
	client, err := NewClient()
	if err != nil {
		t.Skip("Could not create client from config (API key required): ", err)
	}

	ctx := context.Background()

	userQuestion := "请数到5"
	messages := []Message{
		{
			Role:    "user",
			Content: userQuestion,
		},
	}

	// 打印测试信息
	t.Logf("\n"+
		"============================================\n"+
		"🌊 测试: Stream (流式响应)\n"+
		"============================================\n"+
		"❓ 问题: %s\n"+
		"⏳ 等待流式响应...", userQuestion)

	startTime := time.Now()
	chunkChan, errChan := client.ChatCompletionStream(ctx, messages)

	// 设置超时（MiniMax API响应极慢，特别是流式响应）
	timeout := time.After(300 * time.Second) // 5分钟超时

	var fullContent string
	var reasoningContent string
	chunkCount := 0

	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				// Channel closed
				goto done
			}
			if chunk != nil && len(chunk.Choices) > 0 {
				chunkCount++
				choice := chunk.Choices[0]

				// 处理Delta（增量内容）
				if choice.Delta != nil {
					delta := choice.Delta

					// 收集内容
					if content, ok := delta.Content.(string); ok && content != "" {
						fullContent += content
						t.Logf("Chunk %d (content): %s", chunkCount, content)
					}

					// 收集推理内容（MiniMax特有）
					if delta.ReasoningContent != "" {
						reasoningContent += delta.ReasoningContent
						t.Logf("Chunk %d (reasoning): %s", chunkCount, delta.ReasoningContent)
					}
				}

				// 处理最终Message（最后一个chunk可能包含完整消息）
				if choice.Message != nil {
					if content, ok := choice.Message.Content.(string); ok && content != "" {
						fullContent = content // 使用最终的完整内容
						t.Logf("Final message content: %s", content)
					}
					if choice.Message.ReasoningContent != "" {
						t.Logf("Final reasoning content: %s", choice.Message.ReasoningContent)
					}
				}
			}

		case err := <-errChan:
			if err != nil {
				t.Fatalf("Stream error: %v", err)
			}

		case <-timeout:
			t.Fatal("Stream timeout")
		}
	}

done:
	elapsed := time.Since(startTime)

	// MiniMax可能在content或reasoning_content中返回内容
	if fullContent == "" && reasoningContent == "" {
		t.Error("Expected non-empty streamed content or reasoning content")
	}

	// 打印结果
	t.Logf("\n" +
		"--------------------------------------------\n" +
		"📊 流式响应结果:\n")

	if fullContent != "" {
		t.Logf("✅ 最终内容: %s", fullContent)
	}
	if reasoningContent != "" {
		// 推理内容可能很长，只显示前200字符
		displayReasoning := reasoningContent
		if len(reasoningContent) > 200 {
			displayReasoning = reasoningContent[:200] + "..."
		}
		t.Logf("💭 推理过程(前200字): %s", displayReasoning)
	}

	t.Logf("📈 统计: 共%d个chunk, 耗时%.2f秒\n"+
		"============================================",
		chunkCount, elapsed.Seconds())
}
