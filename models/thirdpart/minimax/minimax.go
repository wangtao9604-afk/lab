package minimax

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
)

// 定义常量，避免魔术字符串
const (
	endConversationToolName        = "end_conversation"
	extractKeywordsToolName        = "extract_keywords"
	confirmLocationToolName        = "confirm_location_change"
	classifyPurchaseIntentToolName = "classify_purchase_intent"
	microLocationToolName          = "extract_micro_location"
	endConversationMessage         = "为您生成详细的房源推荐报告"
)

// Client MiniMax API客户端
type Client struct {
	apiKey      string
	groupID     string
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	topP        float64
	stream      bool
	httpClient  *http.Client

	// 系统Prompt相关配置
	systemPrompt       string
	enableSystemPrompt bool
	systemRoleName     string
}

// ContentItem 内容项（用于多模态content数组）
type ContentItem struct {
	Type     string    `json:"type"`                // 内容类型：text/image_url
	Text     string    `json:"text,omitempty"`      // 文本内容（type=text时）
	ImageURL *ImageURL `json:"image_url,omitempty"` // 图片URL（type=image_url时）
}

// ImageURL 图片URL结构
type ImageURL struct {
	URL string `json:"url"` // 图片URL地址，支持公网URL或base64编码
}

// Message 消息结构
type Message struct {
	Role             string      `json:"role"`                        // system/user/assistant/tool
	Content          interface{} `json:"content"`                     // 消息内容：string或[]ContentItem
	Name             string      `json:"name,omitempty"`              // 可选的名称字段
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`        // 工具调用（assistant角色）
	ToolCallID       string      `json:"tool_call_id,omitempty"`      // 工具调用ID（tool角色）
	ReasoningContent string      `json:"reasoning_content,omitempty"` // MiniMax特有：推理过程内容
	AudioContent     string      `json:"audio_content,omitempty"`     // MiniMax特有：音频内容
}

type RawMsg struct {
	Source  string // 来源：AI or 用户
	Content string
}

// ToolCall 工具调用结构
type ToolCall struct {
	ID       string   `json:"id"`       // 工具调用ID
	Type     string   `json:"type"`     // 类型，固定为"function"
	Function Function `json:"function"` // 函数调用信息
}

// Function 函数调用信息
type Function struct {
	Name      string `json:"name"`      // 函数名称
	Arguments string `json:"arguments"` // 函数参数（JSON字符串）
}

// Tool 工具定义
type Tool struct {
	Type     string             `json:"type"`     // 工具类型，固定为"function"
	Function FunctionDefinition `json:"function"` // 函数定义
}

// FunctionDefinition 函数定义
type FunctionDefinition struct {
	Name        string                 `json:"name"`        // 函数名称
	Description string                 `json:"description"` // 函数描述
	Parameters  map[string]interface{} `json:"parameters"`  // 函数参数schema
}

// ResponseFormat 响应格式控制
type ResponseFormat struct {
	Type       string     `json:"type"`        // 响应格式类型，固定为"json_schema"
	JSONSchema JSONSchema `json:"json_schema"` // JSON Schema定义
}

// JSONSchema 结构化输出schema
type JSONSchema struct {
	Name        string                 `json:"name"`        // 输出格式名称
	Description string                 `json:"description"` // 输出格式描述
	Schema      map[string]interface{} `json:"schema"`      // JSON Schema对象
}

// StreamOptions 流式响应选项
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"` // 是否包含usage信息
}

// ChatCompletionRequest 对话请求
type ChatCompletionRequest struct {
	Model             string          `json:"model"`                         // 模型名称
	Messages          []Message       `json:"messages"`                      // 对话消息
	Stream            bool            `json:"stream,omitempty"`              // 是否流式返回
	MaxTokens         int             `json:"max_tokens,omitempty"`          // 最大生成token数
	Temperature       float64         `json:"temperature,omitempty"`         // 温度参数
	TopP              float64         `json:"top_p,omitempty"`               // 采样参数
	MaskSensitiveInfo bool            `json:"mask_sensitive_info,omitempty"` // 是否对敏感信息打码
	Tools             []Tool          `json:"tools,omitempty"`               // 工具定义
	ToolChoice        string          `json:"tool_choice,omitempty"`         // 工具选择模式
	ResponseFormat    *ResponseFormat `json:"response_format,omitempty"`     // 响应格式控制
	StreamOptions     *StreamOptions  `json:"stream_options,omitempty"`      // 流式响应选项
}

// Choice 响应选项
type Choice struct {
	Index        int      `json:"index"`                   // 选择索引
	Message      *Message `json:"message,omitempty"`       // 非流式响应消息
	Delta        *Message `json:"delta,omitempty"`         // 流式响应增量
	FinishReason string   `json:"finish_reason,omitempty"` // 结束原因：stop/tool_calls/length
}

// Usage token使用统计
type Usage struct {
	TotalTokens      int `json:"total_tokens"`
	TotalCharacters  int `json:"total_characters"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// BaseResp 基础响应状态
type BaseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

// ChatCompletionResponse 对话响应
type ChatCompletionResponse struct {
	ID                  string    `json:"id"`
	Object              string    `json:"object"`
	Created             int64     `json:"created"`
	Model               string    `json:"model"`
	Choices             []Choice  `json:"choices"`
	Usage               *Usage    `json:"usage,omitempty"`
	InputSensitive      bool      `json:"input_sensitive"`
	OutputSensitive     bool      `json:"output_sensitive"`
	InputSensitiveType  int       `json:"input_sensitive_type"`
	OutputSensitiveType int       `json:"output_sensitive_type"`
	OutputSensitiveInt  int       `json:"output_sensitive_int"`
	BaseResp            *BaseResp `json:"base_resp,omitempty"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	ID                  string    `json:"id"`
	Object              string    `json:"object"`
	Created             int64     `json:"created"`
	Model               string    `json:"model"`
	Choices             []Choice  `json:"choices"`
	Usage               *Usage    `json:"usage,omitempty"`
	InputSensitive      bool      `json:"input_sensitive"`
	OutputSensitive     bool      `json:"output_sensitive"`
	InputSensitiveType  int       `json:"input_sensitive_type"`
	OutputSensitiveType int       `json:"output_sensitive_type"`
	BaseResp            *BaseResp `json:"base_resp,omitempty"`
}

// NewClient 创建MiniMax客户端（从全局配置）
func NewClient() (*Client, error) {
	cfg := config.GetInstance().MiniMaxConfig

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("MiniMax API key is required")
	}

	// 使用配置值，如果配置中有默认值会在config.parseConfig中设置
	client := &Client{
		apiKey:      cfg.APIKey,
		groupID:     cfg.GroupID,
		model:       cfg.Model,
		baseURL:     cfg.BaseURL,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		topP:        cfg.TopP,
		stream:      cfg.Stream,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
		// 系统Prompt配置
		systemPrompt:       cfg.SystemPrompt,
		enableSystemPrompt: cfg.EnableSystemPrompt,
		systemRoleName:     cfg.SystemRoleName,
	}

	if err := client.ValidateConfig(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewClientWithCustomConfig 使用自定义配置创建客户端
// API Key 始终从配置文件读取，只允许自定义模型参数
func NewClientWithCustomConfig(model string, maxTokens int, temperature float64) (*Client, error) {
	// 获取配置，包括API Key
	cfg := config.GetInstance().MiniMaxConfig

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("MiniMax API key is required")
	}

	// 应用自定义值（如果提供）
	if model != "" {
		cfg.Model = model
	}
	if maxTokens > 0 {
		cfg.MaxTokens = maxTokens
	}
	if temperature > 0 {
		cfg.Temperature = temperature
	}

	client := &Client{
		apiKey:      cfg.APIKey, // 始终从配置读取
		groupID:     cfg.GroupID,
		model:       cfg.Model,
		baseURL:     cfg.BaseURL,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		topP:        cfg.TopP,
		stream:      cfg.Stream,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
		// 系统Prompt配置
		systemPrompt:       cfg.SystemPrompt,
		enableSystemPrompt: cfg.EnableSystemPrompt,
		systemRoleName:     cfg.SystemRoleName,
	}

	if err := client.ValidateConfig(); err != nil {
		return nil, err
	}

	return client, nil
}

// prepareMessages 自动注入系统prompt
func (c *Client) prepareMessages(messages []Message) []Message {
	// 如果未启用系统prompt或prompt为空，直接返回原消息
	if !c.enableSystemPrompt || c.systemPrompt == "" {
		return messages
	}

	// 检查是否已有系统消息
	hasSystemMessage := false
	for _, msg := range messages {
		if msg.Role == "system" {
			hasSystemMessage = true
			break
		}
	}

	// 如果没有系统消息，在开头插入
	if !hasSystemMessage {
		systemMsg := Message{
			Role:    "system",
			Content: c.systemPrompt,
			Name:    c.systemRoleName,
		}
		return append([]Message{systemMsg}, messages...)
	}

	return messages
}

// ChatCompletion 同步对话接口
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (*ChatCompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("MiniMax API key not configured")
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	// 自动注入系统prompt
	messages = c.prepareMessages(messages)

	req := &ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Stream:      c.stream,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		TopP:        c.topP,
	}

	return c.doRequest(ctx, req)
}

// ChatCompletionWithOptions 带选项的对话接口
func (c *Client) ChatCompletionWithOptions(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("MiniMax API key not configured")
	}

	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	// 自动注入系统prompt
	req.Messages = c.prepareMessages(req.Messages)

	// 设置默认值
	if req.Model == "" {
		req.Model = c.model
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.maxTokens
	}
	if req.Temperature <= 0 {
		req.Temperature = c.temperature
	}

	return c.doRequest(ctx, req)
}

// ChatCompletionStream 流式对话接口
func (c *Client) ChatCompletionStream(ctx context.Context, messages []Message) (<-chan *StreamChunk, <-chan error) {
	chunkChan := make(chan *StreamChunk, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		defer close(errChan)

		if c.apiKey == "" {
			errChan <- fmt.Errorf("MiniMax API key not configured")
			return
		}

		if len(messages) == 0 {
			errChan <- fmt.Errorf("messages cannot be empty")
			return
		}

		// 自动注入系统prompt
		messages = c.prepareMessages(messages)

		req := &ChatCompletionRequest{
			Model:       c.model,
			Messages:    messages,
			Stream:      true,
			MaxTokens:   c.maxTokens,
			Temperature: c.temperature,
			TopP:        c.topP,
		}

		if err := c.doStreamRequest(ctx, req, chunkChan); err != nil {
			errChan <- err
		}
	}()

	return chunkChan, errChan
}

// ChatCompletionStreamWithOptions 带选项的流式对话接口
func (c *Client) ChatCompletionStreamWithOptions(ctx context.Context, req *ChatCompletionRequest) (<-chan *StreamChunk, <-chan error) {
	chunkChan := make(chan *StreamChunk, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		defer close(errChan)

		if c.apiKey == "" {
			errChan <- fmt.Errorf("MiniMax API key not configured")
			return
		}

		if len(req.Messages) == 0 {
			errChan <- fmt.Errorf("messages cannot be empty")
			return
		}

		// 自动注入系统prompt
		req.Messages = c.prepareMessages(req.Messages)

		// 强制设置为流式
		req.Stream = true

		// 设置默认值
		if req.Model == "" {
			req.Model = c.model
		}
		if req.MaxTokens <= 0 {
			req.MaxTokens = c.maxTokens
		}
		if req.Temperature <= 0 {
			req.Temperature = c.temperature
		}

		if err := c.doStreamRequest(ctx, req, chunkChan); err != nil {
			errChan <- err
		}
	}()

	return chunkChan, errChan
}

// doRequest 执行同步请求
func (c *Client) doRequest(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	apiURL := fmt.Sprintf("%s/v1/text/chatcompletion_v2", c.baseURL)
	if c.groupID != "" {
		apiURL = fmt.Sprintf("%s?GroupId=%s", apiURL, url.QueryEscape(c.groupID))
	}

	// 构建请求体
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	// 调试日志：打印原始请求body（限制长度避免内存问题）
	// log.GetInstance().Sugar.Debugf("MiniMax API Request Body: %s", string(body))

	// 创建HTTP请求（使用context）
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置请求头
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	// 发送请求
	// fmt.Printf("开始调用Http接口..... %s\n", time.Now().Format("2006-01-02 15:04:05"))
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request failed: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码（限制错误响应体大小）
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024)) // 限制读取1KB
		if err != nil {
			return nil, fmt.Errorf("API request failed with status %d and could not read body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result ChatCompletionResponse
	// 读取并保存原始响应体以便调试
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	// 调试日志：打印原始响应body（限制长度避免内存问题）
	log.GetInstance().Sugar.Debugf("MiniMax API Response Body: %s", string(respBody))

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	// 检查业务错误
	if result.BaseResp != nil && result.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("API error %d: %s", result.BaseResp.StatusCode, result.BaseResp.StatusMsg)
	}

	return &result, nil
}

// doStreamRequest 执行流式请求
func (c *Client) doStreamRequest(ctx context.Context, req *ChatCompletionRequest, chunkChan chan<- *StreamChunk) error {
	apiURL := fmt.Sprintf("%s/v1/text/chatcompletion_v2", c.baseURL)
	if c.groupID != "" {
		apiURL = fmt.Sprintf("%s?GroupId=%s", apiURL, url.QueryEscape(c.groupID))
	}

	// 构建请求体
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request failed: %w", err)
	}

	// 创建HTTP请求（使用context）
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	// 设置请求头
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request failed: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码（限制错误响应体大小）
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024)) // 限制读取1KB
		if err != nil {
			return fmt.Errorf("API request failed with status %d and could not read body: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 读取流式响应
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		// 检查context是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// 处理SSE格式（Server-Sent Events）
		// MiniMax使用标准SSE格式，每行以"data: "开头
		if !strings.HasPrefix(line, "data: ") {
			// 忽略非数据行（可能是注释或其他SSE字段）
			continue
		}

		// 提取JSON数据
		jsonData := strings.TrimPrefix(line, "data: ")

		// 检查是否是结束标记
		if jsonData == "[DONE]" {
			// 流结束
			break
		}

		// 解析JSON块
		var chunk StreamChunk
		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			// 解析失败是致命错误，应该终止流
			return fmt.Errorf("failed to parse stream chunk: %w", err)
		}

		// 检查API错误
		if chunk.BaseResp != nil && chunk.BaseResp.StatusCode != 0 {
			return fmt.Errorf("API error in stream: code=%d, msg=%s",
				chunk.BaseResp.StatusCode, chunk.BaseResp.StatusMsg)
		}

		// 发送到channel（使用context）
		select {
		case chunkChan <- &chunk:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream failed: %w", err)
	}

	return nil
}

// SimpleChat 简单对话接口（便捷方法）
func (c *Client) SimpleChat(ctx context.Context, userMessage string) (string, error) {
	messages := []Message{
		{
			Role:    "user",
			Content: userMessage,
		},
	}

	resp, err := c.ChatCompletion(ctx, messages)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return "", fmt.Errorf("no response from MiniMax")
	}

	// 处理字符串类型的content
	if content, ok := resp.Choices[0].Message.Content.(string); ok {
		return content, nil
	}

	return "", fmt.Errorf("unexpected response content type")
}

// SimpleChatWithSystem 带系统提示的简单对话
func (c *Client) SimpleChatWithSystem(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	messages := []Message{
		{
			Role:    "system",
			Content: systemPrompt,
			Name:    "MiniMax AI",
		},
		{
			Role:    "user",
			Content: userMessage,
		},
	}

	resp, err := c.ChatCompletion(ctx, messages)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return "", fmt.Errorf("no response from MiniMax")
	}

	// 处理字符串类型的content
	if content, ok := resp.Choices[0].Message.Content.(string); ok {
		return content, nil
	}

	return "", fmt.Errorf("unexpected response content type")
}

// ChatCompletionWithTools 带工具的对话接口
func (c *Client) ChatCompletionWithTools(ctx context.Context, messages []Message, tools []Tool, toolChoice string) (*ChatCompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("MiniMax API key not configured")
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	// 自动注入系统prompt
	messages = c.prepareMessages(messages)

	req := &ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Stream:      false,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		TopP:        c.topP,
		Tools:       tools,
		ToolChoice:  toolChoice,
	}

	return c.doRequest(ctx, req)
}

// CreateTextMessage 创建文本消息
func CreateTextMessage(role, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

// CreateMultiModalMessage 创建多模态消息
func CreateMultiModalMessage(role string, items []ContentItem) Message {
	return Message{
		Role:    role,
		Content: items,
	}
}

// CreateTextContentItem 创建文本内容项
func CreateTextContentItem(text string) ContentItem {
	return ContentItem{
		Type: "text",
		Text: text,
	}
}

// CreateImageContentItem 创建图片内容项
func CreateImageContentItem(imageURL string) ContentItem {
	return ContentItem{
		Type: "image_url",
		ImageURL: &ImageURL{
			URL: imageURL,
		},
	}
}

// CreateToolMessage 创建工具调用结果消息
func CreateToolMessage(toolCallID, content string) Message {
	return Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	}
}

// CreateTool 创建工具定义
func CreateTool(name, description string, parameters map[string]interface{}) Tool {
	return Tool{
		Type: "function",
		Function: FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// CreateEndConversationTool 创建简化的会话结束工具
// 仅用于通知智能体对话结束
func CreateEndConversationTool() Tool {
	return Tool{
		Type: "function",
		Function: FunctionDefinition{
			Name:        endConversationToolName,
			Description: "结束房源咨询对话",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"consultation_summary": map[string]interface{}{
						"type":        "string",
						"description": "本次咨询的总结",
					},
				},
				"required": []string{}, // 仅通知对话结束，无必需参数
			},
		},
	}
}

// CreateExtractKeywordsTool 创建关键词提取工具
// 每次智能体应答时调用，按10个维度结构化提取关键词
func CreateExtractKeywordsTool() Tool {
	return Tool{
		Type: "function",
		Function: FunctionDefinition{
			Name:        extractKeywordsToolName,
			Description: "提取当前对话中的房源相关关键词，按10个维度结构化。仅填写当前已确定且高置信度的信息；不要凭空推断；每个维度只填写单个最新意图。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					// 1. location - 位置信息（结构化）
					"location": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"province": map[string]interface{}{
									"type":        "string",
									"description": "省份，固定填写‘上海’",
								},
								"district": map[string]interface{}{
									"type":        "string",
									"description": "区域，如：浦东、闵行。若用户说‘浦东新区’，请填写为‘浦东’",
								},
								"plate": map[string]interface{}{
									"type":        "string",
									"description": "板块，如：陆家嘴、张江。若已知所属区，请同时填写 district；若仅有板块，可在确认函数中补充所属区",
								},
								"landmark": map[string]interface{}{
									"type":        "string",
									"description": "地标，仅填写地名本身，例如‘东方明珠’；不要包含‘附近/靠近/在/到’等关系词",
								},
							},
						},
						"description": "地理位置信息；本系统按最新为准。若检测到与当前不同的区域/板块，请先向用户确认是否切换，再调用位置确认函数。",
					},

					// 2. decoration - 装修要求
					"decoration": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"毛坯", "简装", "精装", "豪装", "中装"},
						},
						"description": "装修要求。注意：‘中装’在后端统一映射为‘简装’",
					},

					// 3. property_type - 房产类型
					"property_type": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"住宅", "别墅", "公寓", "商铺", "写字楼", "厂房", "车位"},
						},
						"description": "物业类型（不是交易类型）。‘新房/二手房’请放在兴趣点中，不要填到 property_type。",
					},

					// 4. room_layout - 户型（结构化）
					"room_layout": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"bedrooms": map[string]interface{}{
									"type":        "integer",
									"description": "卧室数量",
								},
								"living_rooms": map[string]interface{}{
									"type":        "integer",
									"description": "客厅数量",
								},
								"description": map[string]interface{}{
									"type":        "string",
									"description": "户型描述，如‘3室2厅’。不包含卫生间字段；仅提交最新的一项",
								},
							},
						},
						"description": "户型需求",
					},

					// 5. price - 购买价格（结构化）
					"price": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"min": map[string]interface{}{
									"type":        "number",
									"description": "最低价格",
								},
								"max": map[string]interface{}{
									"type":        "number",
									"description": "最高价格",
								},
								"operator": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"about", "<=", ">=", "="},
									"description": "操作符",
								},
								"unit": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"万", "万元", "元"},
									"description": "价格单位。统一换算为‘万’：例如‘1亿’→10000万，‘5000万元’→5000万，‘500万’保持500万；‘1000万左右’→ operator=about, min=1000",
								},
							},
						},
						"description": "购买价格范围。支持关于、区间、上/下限表达；每轮仅输出一个一致的表达。",
					},

					// 6. rent_price - 租赁价格（结构化）
					"rent_price": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"min": map[string]interface{}{
									"type":        "number",
									"description": "最低租金",
								},
								"max": map[string]interface{}{
									"type":        "number",
									"description": "最高租金",
								},
								"operator": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"about", "<=", ">=", "="},
									"description": "操作符",
								},
								"unit": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"元/月", "元", "千", "万"},
									"description": "租金单位",
								},
							},
						},
						"description": "租赁价格范围",
					},

					// 7. area - 面积（结构化）
					"area": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"value": map[string]interface{}{
									"type":        "number",
									"description": "面积值",
								},
								"min": map[string]interface{}{
									"type":        "number",
									"description": "最小面积",
								},
								"max": map[string]interface{}{
									"type":        "number",
									"description": "最大面积",
								},
								"operator": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"about", "<=", ">=", "="},
									"description": "操作符",
								},
								"unit": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"平米", "平方米", "平", "㎡"},
									"description": "面积单位。统一为‘平米’；例如‘150平左右’→ value=150, operator=about, unit=‘平米’",
								},
							},
						},
						"description": "面积需求。支持关于、区间、上/下限表达；每轮仅输出一个一致的表达。",
					},

					// 8. interest_points - 兴趣点
					"interest_points": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "兴趣点。优先使用标准标签：例如‘近地铁/学校好/学区房/近公园/交通便利/地段好/CBD商圈/综合体/人流量大/商业街/新房/二手房’等",
					},

					// 9. commercial - 商业投资属性
					"commercial": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "商业相关需求，如：投资回报率、租售比、商圈成熟度",
					},

					// 10. orientation - 朝向
					"orientation": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"东", "南", "西", "北", "东南", "东北", "西南", "西北", "南北通透", "东西朝向"},
						},
						"description": "朝向要求。‘南北向’统一为‘南北通透’，‘东西向’统一为‘东西朝向’；用户说‘南/北/东/西’统一写‘朝南/朝北/朝东/朝西’",
					},
				},
				"required": []string{}, // 全部可选
			},
		},
	}
}

// CreateMicroLocationTool 创建微地标/距离表达捕获工具
func CreateMicroLocationTool() Tool {
	travelItemSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"anchor_name": map[string]interface{}{
				"type":        "string",
				"description": "锚点名称，例如‘七宝万科广场’或‘9号线七宝站’",
			},
			"anchor_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"street_address", "metro_station", "bus_stop", "poi", "unknown"},
				"description": "锚点类型，无法判断填 unknown",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"walk", "bike", "drive"},
				"description": "出行方式：walk(步行)/bike(骑行)/drive(驾车)",
			},
			"min_minutes": map[string]interface{}{
				"type":        "number",
				"description": "最短时间（分钟）",
			},
			"max_minutes": map[string]interface{}{
				"type":        "number",
				"description": "最长时间（分钟）",
			},
		},
		"required": []string{"anchor_name", "mode", "min_minutes", "max_minutes"},
	}

	return Tool{
		Type: "function",
		Function: FunctionDefinition{
			Name:        microLocationToolName,
			Description: "识别用户提供的微地标（地址/地铁站/地标）以及‘离某地步行/骑行/驾车约Y分钟’等距离表达，用于补全位置锚点。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"micro_location": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name": map[string]interface{}{
								"type":        "string",
								"description": "微地标名称，如‘七宝万科广场’或‘XX路XX号’",
							},
							"type": map[string]interface{}{
								"type":        "string",
								"enum":        []string{"street_address", "metro_station", "bus_stop", "poi", "unknown"},
								"description": "锚点类型，缺省为 unknown",
							},
						},
					},
					"micro_location_opt_out": map[string]interface{}{
						"type":        "boolean",
						"description": "用户是否明确表示不限定微地标",
					},
					"travel_time_constraints": map[string]interface{}{
						"type":        "array",
						"items":       travelItemSchema,
						"description": "距离表达数组，通常取第一项即可",
					},
				},
				"required": []string{},
			},
		},
	}
}

// CreateClassifyPurchaseIntentTool 创建购房意向分类工具
// 模型需调用该工具并在arguments中返回 {"intent":"新房|二手房|All"}
func CreateClassifyPurchaseIntentTool() Tool {
	return CreateTool(
		classifyPurchaseIntentToolName,
		"根据对话上下文判断用户购房意向：新房、二手房或都可以(All)。务必在arguments中返回对象 {intent}，其中 intent ∈ {新房, 二手房, All}。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"intent": map[string]interface{}{
					"type": "string",
					"enum": []string{"新房", "二手房", "All"},
				},
			},
		},
	)
}

// CreateConfirmLocationChangeTool 创建位置切换确认工具
// 当用户明确同意切换区域或板块后调用
func CreateConfirmLocationChangeTool() Tool {
	return Tool{
		Type: "function",
		Function: FunctionDefinition{
			Name:        confirmLocationToolName,
			Description: "当且仅当用户明确同意切换到新的区域/板块后调用，用于更新当前位置。若用户否认则不要调用。district/plate 任一非空代表变更；若仅给出 plate，系统可据板块反查所属区域。若用户明确要求‘清空/重来’，请将 reset_non_location 置为 true；否则默认为 false（沿用之前的非位置条件）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"district": map[string]interface{}{
						"type":        "string",
						"description": "新的区域名（如：浦东、黄浦）。为空代表不变；若用户表述为‘浦东新区’，请填写为‘浦东’",
					},
					"plate": map[string]interface{}{
						"type":        "string",
						"description": "新的板块名（如：陆家嘴、张江）。为空代表不变；若仅给出板块也可调用，本系统可反查其所属区域",
					},
					"reset_non_location": map[string]interface{}{
						"type":        "boolean",
						"description": "是否清空已收集的非位置维度（户型、面积、预算等）。仅当用户明确要求‘清空/重新设置’时为 true；默认 false 表示沿用之前的条件。",
						"default":     false,
					},
				},
				"required": []string{},
			},
		},
	}
}

// CreateResponseFormat 创建响应格式
func CreateResponseFormat(name, description string, schema map[string]interface{}) *ResponseFormat {
	return &ResponseFormat{
		Type: "json_schema",
		JSONSchema: JSONSchema{
			Name:        name,
			Description: description,
			Schema:      schema,
		},
	}
}

// ValidateConfig 验证配置
func (c *Client) ValidateConfig() error {
	if c.apiKey == "" {
		return fmt.Errorf("API key is required")
	}
	if c.baseURL == "" {
		return fmt.Errorf("base URL is required")
	}
	if c.model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}

// GetModel 获取当前模型
func (c *Client) GetModel() string {
	return c.model
}

// SetModel 设置模型
func (c *Client) SetModel(model string) {
	c.model = model
}

// GetMaxTokens 获取最大token数
func (c *Client) GetMaxTokens() int {
	return c.maxTokens
}

// SetMaxTokens 设置最大token数
func (c *Client) SetMaxTokens(maxTokens int) {
	c.maxTokens = maxTokens
}

// GetTemperature 获取温度参数
func (c *Client) GetTemperature() float64 {
	return c.temperature
}

// SetTemperature 设置温度参数
func (c *Client) SetTemperature(temperature float64) {
	c.temperature = temperature
}

// GetTopP 获取TopP参数
func (c *Client) GetTopP() float64 {
	return c.topP
}

// SetTopP 设置TopP参数
func (c *Client) SetTopP(topP float64) {
	c.topP = topP
}

// GetStream 获取流式设置
func (c *Client) GetStream() bool {
	return c.stream
}

// SetStream 设置流式返回
func (c *Client) SetStream(stream bool) {
	c.stream = stream
}

// RealEstateConsult 房源咨询专用接口
// 用于处理上海房源查询的专门接口，会自动带上房源助手的系统prompt
func (c *Client) RealEstateConsult(ctx context.Context, userQuery string) (string, error) {
	// 构建消息，系统prompt会通过prepareMessages自动注入
	messages := []Message{
		{
			Role:    "user",
			Content: userQuery,
			Name:    "用户",
		},
	}

	resp, err := c.ChatCompletion(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("real estate consultation failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return "", fmt.Errorf("no response from real estate assistant")
	}

	// 处理响应内容
	if content, ok := resp.Choices[0].Message.Content.(string); ok {
		return content, nil
	}

	return "", fmt.Errorf("unexpected response content type")
}

// RealEstateMultiRound 房源咨询多轮对话接口
// 支持带历史消息的多轮对话，适用于收集用户需求的场景
func (c *Client) RealEstateMultiRound(ctx context.Context, history []Message, userQuery string) (*ChatCompletionResponse, error) {
	// 添加新的用户消息
	newMessage := Message{
		Role:    "user",
		Content: userQuery,
		Name:    "用户",
	}

	// 合并历史消息和新消息
	messages := append(history, newMessage)

	// 调用ChatCompletion，系统prompt会自动注入
	return c.ChatCompletion(ctx, messages)
}

// EnableSystemPrompt 启用系统Prompt
func (c *Client) EnableSystemPrompt(enable bool) {
	c.enableSystemPrompt = enable
}

// SetSystemPrompt 设置系统Prompt
func (c *Client) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt
}

// GetSystemPromptStatus 获取系统Prompt状态
func (c *Client) GetSystemPromptStatus() bool {
	return c.enableSystemPrompt
}

// RealEstateConsultResponse 房源咨询响应（包含结束信号）
type RealEstateConsultResponse struct {
	Content           string                  // AI回复内容
	IsConversationEnd bool                    // 是否会话结束
	IsEndFuncCalled   bool                    // 是否通过Function Call结束（用于决定是否清理历史）
	EndCallData       map[string]interface{}  // 结束时的函数调用数据
	ExtractedKeywords map[string]interface{}  // extract_keywords函数提取的关键词
	RawResponse       *ChatCompletionResponse // 原始响应
}

// RealEstateConsultWithFunctionCall 带函数调用的房源咨询（重构版）
// 支持自动检测会话结束并返回信号，使用改进的错误处理
func (c *Client) RealEstateConsultWithFunctionCall(ctx context.Context, messages []Message) (*RealEstateConsultResponse, error) {
	// 创建工具：关键词提取、结束会话、位置切换确认
	tools := []Tool{
		CreateExtractKeywordsTool(),        // 每次应答时可能调用
		CreateEndConversationTool(),        // 结束时调用
		CreateConfirmLocationChangeTool(),  // 位置切换确认
		CreateClassifyPurchaseIntentTool(), // 意向分类
		CreateMicroLocationTool(),          // 微地标与距离表达
	}

	// 调用带工具的对话接口
	resp, err := c.ChatCompletionWithTools(ctx, messages, tools, "auto")
	if err != nil {
		return nil, fmt.Errorf("real estate consultation with tools failed: %w", err)
	}

	// 检查是否有extract_keywords函数调用
	var extractedKeywords map[string]interface{}
	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil && len(resp.Choices[0].Message.ToolCalls) > 0 {
		for _, toolCall := range resp.Choices[0].Message.ToolCalls {
			if toolCall.Function.Name == extractKeywordsToolName {
				// 解析extract_keywords的参数
				var keywords map[string]interface{}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &keywords); err == nil {
					extractedKeywords = keywords
					log.GetInstance().Sugar.Info("Extracted keywords from AI: ", keywords)
				}
			}
		}
	}

	// 使用统一的检测函数（单一数据源）
	isEnd, endData, isEndFuncCalled, err := IsConversationEndDetected(resp)
	if err != nil {
		// 解析错误时关键失败，需要传播
		return nil, fmt.Errorf("conversation end detected but with invalid data: %w", err)
	}

	result := &RealEstateConsultResponse{
		RawResponse:       resp,
		IsConversationEnd: isEnd,
		IsEndFuncCalled:   isEndFuncCalled,
		EndCallData:       endData,
		ExtractedKeywords: extractedKeywords,
	}

	if isEnd {
		if endData != nil {
			// 通过有效的函数调用检测到结束
			result.Content = endConversationMessage
		} else {
			// 通过文本关键词检测到结束
			if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
				if content, ok := resp.Choices[0].Message.Content.(string); ok {
					// 保留原始内容，由processor.go统一清理
					// endpoint标记只用于检测，不在这里清理
					result.Content = content
				}
			}
		}
	} else {
		// 会话未结束，返回助手的消息
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
			msg := resp.Choices[0].Message
			if content, ok := msg.Content.(string); ok {
				result.Content = content
			} else if len(msg.ToolCalls) > 0 {
				// 仅有工具调用无文本内容的情况（Function Calling工作流中常见）
				result.Content = "" // 允许空内容，由上层决定如何处理
			} else {
				return nil, fmt.Errorf("unexpected response content type from assistant")
			}
		} else {
			return nil, fmt.Errorf("no response from real estate assistant")
		}
	}

	return result, nil
}

// IsConversationEndDetected 检测会话是否结束
// 双重检测：
// 1. 优先检查function call（最明确的结束信号）
// 2. 其次检查文本内容是否包含结束消息
// 返回值：
// - isEnd: 是否结束
// - data: 函数调用数据（可能为nil）
// - isEndFuncCalled: 是否通过Function Call结束（用于决定是否清理历史）
// - err: 解析错误
func IsConversationEndDetected(resp *ChatCompletionResponse) (isEnd bool, data map[string]interface{}, isEndFuncCalled bool, err error) {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return false, nil, false, nil
	}

	msg := resp.Choices[0].Message

	// 1. 优先检查Function Call（最明确的结束信号）
	if len(msg.ToolCalls) > 0 {
		for _, toolCall := range msg.ToolCalls {
			if toolCall.Function.Name == endConversationToolName {
				// 找到结束函数调用，解析数据
				var functionCallData map[string]interface{}
				if jErr := json.Unmarshal([]byte(toolCall.Function.Arguments), &functionCallData); jErr != nil {
					// Function Call解析失败但仍然是结束信号
					return true, nil, true, fmt.Errorf("failed to parse end_conversation arguments: %w", jErr)
				}
				// Function Call成功，立即返回结束
				return true, functionCallData, true, nil
			}
		}
	}

	// 2. 检查文本内容是否包含结束消息
	if content, ok := msg.Content.(string); ok {
		if strings.Contains(content, endConversationMessage) {
			// 找到结束消息，返回结束信号（无Function Call数据）
			return true, nil, false, nil
		}
	}

	return false, nil, false, nil
}
