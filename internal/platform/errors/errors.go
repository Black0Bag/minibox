// Package errors 提供 RFC 7807 Problem Details for HTTP APIs 错误封装。
//
// 设计依据：
//   - dev/02_后端开发路线图.md：platform/errors RFC 7807 独立错误包
//   - RFC 7807 §3：Problem Details JSON Object
//
// 关键实践（互联网 2026 校准）：
//   - Content-Type: application/problem+json（RFC 7807 §3，不是 application/json）
//   - type 用 URI 引用（如 https://minibox.dev/errors/not_found），非开发环境不暴露内部堆栈
//   - title 为简短可读描述（不随实例变化）
//   - detail 为具体错误信息（可随实例变化）
//   - instance 标识具体发生位置（通常为请求路径）
//   - 扩展字段放在 members 中（RFC 7807 §3.2 允许扩展成员）
//   - 不暴露内部堆栈/SQL/文件路径（安全默认）
package errors

import (
	"encoding/json"
	"net/http"
)

// ProblemDetail RFC 7807 问题详情对象。
type ProblemDetail struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Status   int            `json:"status"`
	Detail   string         `json:"detail"`
	Instance string         `json:"instance,omitempty"`
	Members  map[string]any `json:"-"`
}

// Write 将 ProblemDetail 写入 HTTP 响应。
// Content-Type 设为 application/problem+json（RFC 7807 §3）。
func Write(w http.ResponseWriter, pd ProblemDetail) {
	if pd.Type == "" {
		pd.Type = "about:blank"
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(pd.Status)
	m := make(map[string]any, 6+len(pd.Members))
	m["type"] = pd.Type
	m["title"] = pd.Title
	m["status"] = pd.Status
	m["detail"] = pd.Detail
	if pd.Instance != "" {
		m["instance"] = pd.Instance
	}
	for k, v := range pd.Members {
		m[k] = v
	}
	_ = json.NewEncoder(w).Encode(m)
}

// New 快速创建 ProblemDetail。
func New(status int, typ, title, detail string) ProblemDetail {
	return ProblemDetail{Type: typ, Title: title, Status: status, Detail: detail}
}

// WithInstance 设置 instance（通常为请求路径）。
func (pd ProblemDetail) WithInstance(instance string) ProblemDetail {
	pd.Instance = instance
	return pd
}

// WithMember 添加扩展成员。
func (pd ProblemDetail) WithMember(key string, value any) ProblemDetail {
	if pd.Members == nil {
		pd.Members = make(map[string]any)
	}
	pd.Members[key] = value
	return pd
}

// --- 常用错误构造器 ---

// NotFound 构造 404 资源不存在。
func NotFound(detail string) ProblemDetail {
	return New(http.StatusNotFound, "not_found", "资源不存在", detail)
}

// BadRequest 构造 400 请求参数错误。
func BadRequest(detail string) ProblemDetail {
	return New(http.StatusBadRequest, "bad_request", "请求参数错误", detail)
}

// Internal 构造 500 内部错误。
func Internal(detail string) ProblemDetail {
	return New(http.StatusInternalServerError, "internal_error", "内部错误", detail)
}

// ServiceUnavailable 构造 503 服务不可用（依赖未就绪时用）。
func ServiceUnavailable(detail string) ProblemDetail {
	return New(http.StatusServiceUnavailable, "service_unavailable", "服务不可用", detail)
}

// Unauthorized 构造 401 未认证（调用方需自行补 WWW-Authenticate 挑战头）。
func Unauthorized(detail string) ProblemDetail {
	return New(http.StatusUnauthorized, "unauthorized", "未认证", detail)
}

// Forbidden 构造 403 禁止访问（已认证但权限不足）。
func Forbidden(detail string) ProblemDetail {
	return New(http.StatusForbidden, "forbidden", "禁止访问", detail)
}

// Conflict 构造 409 资源冲突。
func Conflict(detail string) ProblemDetail {
	return New(http.StatusConflict, "conflict", "资源冲突", detail)
}
