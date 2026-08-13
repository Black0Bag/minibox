// Package errors 提供 RFC 7807 兼容的错误类型。
// 设计：API 错误统一用 ProblemDetails 结构（RFC 7807），
// 含 recovery_action 扩展供 agent 自动重试。
package errors

import "fmt"

// ProblemDetails 是 RFC 7807 错误信封。
type ProblemDetails struct {
	// Type 错误类型标识（URI 或简短名）。
	Type string `json:"type"`
	// Title 人类可读的错误摘要。
	Title string `json:"title"`
	// Detail 具体错误描述。
	Detail string `json:"detail"`
	// Status HTTP 状态码。
	Status int `json:"status"`
	// Instance 出错的具体资源路径。
	Instance string `json:"instance,omitempty"`
	// TraceID 关联追踪 ID（贯穿三通道）。
	TraceID string `json:"trace_id,omitempty"`
	// RecoveryAction agent 可执行的恢复建议。
	RecoveryAction string `json:"recovery_action,omitempty"`
}

// Error 实现 error 接口。
func (p *ProblemDetails) Error() string {
	return fmt.Sprintf("%s: %s", p.Title, p.Detail)
}

// New 创建 ProblemDetails。
func New(status int, title, detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:   "about:blank",
		Title:  title,
		Detail: detail,
		Status: status,
	}
}

// WithType 设置错误类型。
func (p *ProblemDetails) WithType(t string) *ProblemDetails {
	p.Type = t
	return p
}

// WithInstance 设置实例路径。
func (p *ProblemDetails) WithInstance(inst string) *ProblemDetails {
	p.Instance = inst
	return p
}

// WithTraceID 设置追踪 ID。
func (p *ProblemDetails) WithTraceID(tid string) *ProblemDetails {
	p.TraceID = tid
	return p
}

// WithRecoveryAction 设置恢复建议（agent 自动处理用）。
func (p *ProblemDetails) WithRecoveryAction(action string) *ProblemDetails {
	p.RecoveryAction = action
	return p
}
