package llm

import (
	"errors"
	"fmt"
)

// 错误分类（agent 重试决策用）。
var (
	// ErrRateLimited 限流（429），应等待重试。
	ErrRateLimited = errors.New("llm: 限流")
	// ErrAuthFailed 认证失败（401/403），key 永久禁用。
	ErrAuthFailed = errors.New("llm: 认证失败")
	// ErrBadRequest 请求错误（400），不重试。
	ErrBadRequest = errors.New("llm: 请求无效")
	// ErrServerError 服务器错误（5xx），可重试。
	ErrServerError = errors.New("llm: 服务器错误")
	// ErrContextExceeded 上下文超限。
	ErrContextExceeded = errors.New("llm: 上下文超限")
	// ErrTimeout 超时。
	ErrTimeout = errors.New("llm: 超时")
	// ErrCircuitOpen 熔断器打开。
	ErrCircuitOpen = errors.New("llm: 熔断器打开")
)

// APIError 供应商 API 错误。
// StatusCode 用于错误分类（429/401/403/400/5xx）。
type APIError struct {
	StatusCode int
	Message    string
	RetryAfter int // 429 时 Retry-After 秒数
	Type       error
}

// Error 实现 error 接口。
func (e *APIError) Error() string {
	return fmt.Sprintf("llm api: status=%d message=%q", e.StatusCode, e.Message)
}

// Unwrap 返回分类错误。
func (e *APIError) Unwrap() error {
	return e.Type
}

// ClassifyError 根据 HTTP 状态码分类错误。
func ClassifyError(statusCode int, body string) error {
	switch {
	case statusCode == 429:
		return &APIError{StatusCode: statusCode, Message: body, Type: ErrRateLimited}
	case statusCode == 401 || statusCode == 403:
		return &APIError{StatusCode: statusCode, Message: body, Type: ErrAuthFailed}
	case statusCode == 400:
		return &APIError{StatusCode: statusCode, Message: body, Type: ErrBadRequest}
	case statusCode >= 500:
		return &APIError{StatusCode: statusCode, Message: body, Type: ErrServerError}
	default:
		return &APIError{StatusCode: statusCode, Message: body, Type: fmt.Errorf("未知状态码 %d", statusCode)}
	}
}
