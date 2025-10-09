package keywords

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
)

// 错误类型定义
var (
	// 初始化错误
	ErrNotInitialized = errors.New("extractor not initialized")
	ErrInitFailed     = errors.New("initialization failed")

	// 输入验证错误
	ErrEmptyInput      = errors.New("empty input text")
	ErrInputTooLong    = errors.New("input text exceeds maximum length")
	ErrInvalidEncoding = errors.New("invalid text encoding")

	// 提取错误
	ErrExtractionFailed  = errors.New("extraction failed")
	ErrNoFieldsExtracted = errors.New("no fields extracted")
	ErrPartialExtraction = errors.New("partial extraction completed with errors")
	ErrTimeoutExceeded   = errors.New("extraction timeout exceeded")
	ErrContextCancelled  = errors.New("extraction cancelled by context")

	// 分词错误
	ErrSegmentationFailed = errors.New("text segmentation failed")
	ErrDictionaryNotFound = errors.New("dictionary not found")

	// NLP处理错误
	ErrEntityExtractionFailed  = errors.New("entity extraction failed")
	ErrSentimentAnalysisFailed = errors.New("sentiment analysis failed")
	ErrNumberExtractionFailed  = errors.New("number extraction failed")

	// 配置错误
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrConfigFileNotFound = errors.New("configuration file not found")
	ErrConfigParseFailed  = errors.New("configuration parse failed")

	// 正则表达式错误
	ErrRegexCompileFailed = errors.New("regex compilation failed")
	ErrRegexMatchTimeout  = errors.New("regex match timeout")

	// 资源错误
	ErrResourceNotFound   = errors.New("resource not found")
	ErrResourceLoadFailed = errors.New("resource load failed")
)

// ErrorWithContext 带上下文的错误
type ErrorWithContext struct {
	Err     error
	Context map[string]interface{}
	Stack   string
}

// Error 实现error接口
func (e *ErrorWithContext) Error() string {
	if e.Context != nil && len(e.Context) > 0 {
		return fmt.Sprintf("%v (context: %v)", e.Err, e.Context)
	}
	return e.Err.Error()
}

// Unwrap 支持errors.Is和errors.As
func (e *ErrorWithContext) Unwrap() error {
	return e.Err
}

// WrapError 包装错误并添加上下文
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return errors.Wrap(err, message)
}

// WrapErrorf 包装错误并添加格式化消息
func WrapErrorf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return errors.Wrapf(err, format, args...)
}

// NewError 创建新的错误
func NewError(message string) error {
	return errors.New(message)
}

// NewErrorf 创建格式化的错误
func NewErrorf(format string, args ...interface{}) error {
	return errors.Errorf(format, args...)
}

// IsError 检查错误类型
func IsError(err error, target error) bool {
	return errors.Is(err, target)
}

// AsError 错误类型转换
func AsError(err error, target interface{}) bool {
	return errors.As(err, target)
}

// GetRootCause 获取错误的根本原因
func GetRootCause(err error) error {
	return errors.Cause(err)
}

// ErrorDetails 错误详情
type ErrorDetails struct {
	Code       string                 `json:"code,omitempty"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
	Timestamp  int64                  `json:"timestamp"`
}

// NewErrorDetails 创建错误详情
func NewErrorDetails(err error, code string) *ErrorDetails {
	details := &ErrorDetails{
		Code:      code,
		Message:   err.Error(),
		Timestamp: time.Now().Unix(),
	}

	// 添加堆栈信息（如果可用）
	if stackErr, ok := err.(interface{ StackTrace() errors.StackTrace }); ok {
		details.StackTrace = fmt.Sprintf("%+v", stackErr.StackTrace())
	}

	return details
}

// ValidationError 验证错误
type ValidationError struct {
	Field   string
	Message string
	Value   interface{}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// MultiError 多个错误的集合
type MultiError struct {
	Errors []error
}

// Error 实现error接口
func (e *MultiError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("multiple errors occurred (%d errors)", len(e.Errors))
}

// Add 添加错误
func (e *MultiError) Add(err error) {
	if err != nil {
		e.Errors = append(e.Errors, err)
	}
}

// HasErrors 是否有错误
func (e *MultiError) HasErrors() bool {
	return len(e.Errors) > 0
}

// First 获取第一个错误
func (e *MultiError) First() error {
	if len(e.Errors) > 0 {
		return e.Errors[0]
	}
	return nil
}
