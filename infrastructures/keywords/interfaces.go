package keywords

import (
	"context"
	"time"
)

// KeywordExtractor 关键词提取器接口
type KeywordExtractor interface {
	ExtractKeywords(text string) (map[string][]string, error)
	ExtractKeywordsWithContext(ctx context.Context, text string) (map[string][]string, error)
	MatchFieldValues(keywords map[string][]string, fieldCategory string) map[string]interface{}
	GetSuggestedQuestions(missingFields []string) []string
	ExtractFromMessage(msg string) (*ExtractionResult, error)
	ExtractFromMessageWithContext(ctx context.Context, msg string) (*ExtractionResult, error)
	SetConfidenceThreshold(threshold float64)
	GetExtractionMetrics() *ExtractionMetrics
}


// NLPProcessor NLP处理器接口
type NLPProcessor interface {
	ExtractEntities(text string) ([]Entity, error)
	ExtractEntitiesWithContext(ctx context.Context, text string) ([]Entity, error)
	AnalyzeSentiment(text string) (*SentimentResult, error)
	AnalyzeSentimentWithContext(ctx context.Context, text string) (*SentimentResult, error)
	ExtractNumbers(text string) ([]NumberEntity, error)
	ExtractNumbersWithContext(ctx context.Context, text string) ([]NumberEntity, error)
	NormalizeText(text string) string
}

// ExtractionResult 提取结果
type ExtractionResult struct {
	Fields      map[string]interface{} `json:"fields"`       // 提取的字段数据
	Confidence  map[string]float64     `json:"confidence"`   // 各字段置信度
	Keywords    []string               `json:"keywords"`     // 提取的关键词
	Entities    []Entity               `json:"entities"`     // 命名实体
	Numbers     []NumberEntity         `json:"numbers"`      // 数字实体
	ProcessTime time.Duration          `json:"process_time"` // 处理耗时
	Method      string                 `json:"method"`       // 提取方法
}

// Entity 命名实体
type Entity struct {
	Text       string      `json:"text"`       // 实体文本
	Type       string      `json:"type"`       // 实体类型：LOCATION, PRICE, AREA, etc.
	Start      int         `json:"start"`      // 起始位置
	End        int         `json:"end"`        // 结束位置
	Confidence float64     `json:"confidence"` // 置信度
	Value      interface{} `json:"value"`      // 标准化值
}

// NumberEntity 数字实体
type NumberEntity struct {
	Text       string  `json:"text"`       // 原始文本
	Value      float64 `json:"value"`      // 数字值
	Unit       string  `json:"unit"`       // 单位：万、平、层等
	Type       string  `json:"type"`       // 类型：price, area, floor等
	Start      int     `json:"start"`      // 起始位置
	End        int     `json:"end"`        // 结束位置
	Confidence float64 `json:"confidence"` // 置信度
}

// SegmentResult 分词结果
type SegmentResult struct {
	Word string `json:"word"` // 词语
	Pos  string `json:"pos"`  // 词性
}

// SentimentResult 情感分析结果
type SentimentResult struct {
	Score      float64 `json:"score"`      // 情感得分 [-1, 1]
	Label      string  `json:"label"`      // 情感标签：positive, negative, neutral
	Confidence float64 `json:"confidence"` // 置信度
}

// ExtractionMetrics 提取性能指标
type ExtractionMetrics struct {
	TotalExtractions      int64              `json:"total_extractions"`      // 总提取次数
	SuccessfulExtractions int64              `json:"successful_extractions"` // 成功提取次数
	AvgProcessingTime     float64            `json:"avg_processing_time"`    // 平均处理时间(ms)
	AvgConfidence         float64            `json:"avg_confidence"`         // 平均置信度
	FieldExtractionRate   map[string]float64 `json:"field_extraction_rate"`  // 各字段提取成功率
	ErrorCount            int64              `json:"error_count"`            // 错误次数
	LastUpdated           time.Time          `json:"last_updated"`           // 最后更新时间
}

// FieldDefinition 字段定义
type FieldDefinition struct {
	Name        string   `json:"name"`        // 字段名
	Type        string   `json:"type"`        // 字段类型：string, number, object等
	Required    bool     `json:"required"`    // 是否必需
	Priority    int      `json:"priority"`    // 优先级 1-10
	Keywords    []string `json:"keywords"`    // 相关关键词
	Patterns    []string `json:"patterns"`    // 正则表达式模式
	Validators  []string `json:"validators"`  // 验证规则
	Description string   `json:"description"` // 字段描述
}

// ExtractionContext 提取上下文
type ExtractionContext struct {
	UserID      string                 `json:"user_id"`      // 用户ID
	SessionID   string                 `json:"session_id"`   // 会话ID
	PrevContext map[string]interface{} `json:"prev_context"` // 之前的上下文
	Timestamp   time.Time              `json:"timestamp"`    // 时间戳
}
