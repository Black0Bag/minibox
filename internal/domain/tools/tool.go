// Package tools 定义工具系统。
// 设计：清单式 manifest（白名单）取代沙箱黑名单（ToolClad 2026 安全模型反转）。
// 原则（ai-agents skill）：有界工具、allowlist、幂等副作用、未验证输出不信任。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Metadata 工具元数据（权限系统看，tRPC-Agent 2026 实证）。
type Metadata struct {
	ReadOnly         bool   `json:"read_only"`         // 只读
	Destructive      bool   `json:"destructive"`       // 破坏性（删/改）
	ConcurrencySafe  bool   `json:"concurrency_safe"`  // 并发安全
	SearchOrRead     bool   `json:"search_or_read"`    // 搜索/读取类
	OpenWorld        bool   `json:"open_world"`        // 可联网/外部世界
	MaxResultSize    int    `json:"max_result_size"`   // 输出上限（防烧爆上下文）
	RiskTier         string `json:"risk_tier"`         // low/medium/high
	RequiresApproval bool   `json:"requires_approval"` // 需人工批准
}

// Tool 工具接口。
// 实现：内置工具（编译进二进制）+ 外部 MCP 工具（subprocess）。
type Tool interface {
	Name() string
	Description() string
	// JSONSchema 输入参数 schema（LLM 看）。
	JSONSchema() json.RawMessage
	// Metadata 元数据（权限系统看）。
	Metadata() Metadata
	// Invoke 执行工具。
	Invoke(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry 工具注册表。
// 内置 + MCP 工具同注册表，命名 server__tool 防冲突（cronicle/Wox 实证）。
// 并发安全：读写锁保护（subagent 并行取工具时安全，golang-safety 实证）。
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	aliases map[string]string
	order   []string // 保持注册顺序
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools:   make(map[string]Tool),
		aliases: make(map[string]string),
	}
}

// Register 注册工具。
// 拒绝：禁止工具表（控制面操作）、重名。
func (r *Registry) Register(t Tool) error {
	name := t.Name()
	if IsForbidden(name) {
		return &ForbiddenError{Name: name}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return &DuplicateError{Name: name}
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

// RegisterAlias 注册别名。
func (r *Registry) RegisterAlias(alias, canonical string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias] = canonical
}

// Get 获取工具。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.tools[name]; ok {
		return t, true
	}
	if canonical, ok := r.aliases[name]; ok {
		t, ok := r.tools[canonical]
		return t, ok
	}
	return nil, false
}

// List 列出所有工具（按注册顺序）。
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Names 列出所有工具名。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string{}, r.order...)
}

// Subset 白名单子集（subagent 用，核心 2026 实证）。
func (r *Registry) Subset(names []string) *Registry {
	sub := NewRegistry()
	allow := make(map[string]bool, len(names))
	for _, n := range names {
		allow[n] = true
	}
	for _, name := range r.Names() {
		if allow[name] {
			t, _ := r.Get(name)
			// 子集内工具均已注册且非禁止名，Register 必然成功
			_ = sub.Register(t)
		}
	}
	return sub
}

// Count 工具数量。
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ForbiddenError 禁止工具错误。
type ForbiddenError struct{ Name string }

func (e *ForbiddenError) Error() string {
	return "工具被禁止注册（控制面操作）: " + e.Name
}

// DuplicateError 重名错误。
type DuplicateError struct{ Name string }

func (e *DuplicateError) Error() string {
	return "工具重名: " + e.Name
}

// forbiddenNames 禁止工具表（ironclaw 2026 实证）。
// 控制面操作不许作为直接工具，必须走宿主网关。
var forbiddenNames = map[string]bool{
	"install_packages":  true,
	"install_package":   true,
	"add_mcp_server":    true,
	"remove_mcp_server": true,
	"self_edit":         true,
	"edit_self":         true,
	"edit_source":       true,
	"set_persona":       true,
	"set_permissions":   true,
	"set_worldbook":     true,
}

// IsForbidden 判断工具名是否在禁止表。
func IsForbidden(name string) bool {
	return forbiddenNames[name]
}

// ToolOutputTooLargeError 工具输出超过元数据上限（防烧爆上下文，ai-agents skill）。
type ToolOutputTooLargeError struct {
	Name string
	Got  int
	Max  int
}

func (e *ToolOutputTooLargeError) Error() string {
	return fmt.Sprintf("工具 %s 输出过大: %d > %d 字节", e.Name, e.Got, e.Max)
}

// ToolPanicError 工具 Invoke 内部 panic（golang-safety 实证：recover 隔离，不炸宿主）。
type ToolPanicError struct {
	Name  string
	Panic any
}

func (e *ToolPanicError) Error() string {
	return fmt.Sprintf("工具 %s 内部 panic: %v", e.Name, e.Panic)
}

// SafeInvoke 防御性调用工具：
//  1. panic 恢复（golang-safety：工具实现不可信，隔离 panic）
//  2. 输出大小上限（Metadata.MaxResultSize，防烧爆上下文，ai-agents）
//  3. 空输出规范化（返回空串，避免 nil/空歧义）
//
// MaxResultSize<=0 时视为不限制（metadata 未配置）。
func SafeInvoke(ctx context.Context, t Tool, input json.RawMessage) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &ToolPanicError{Name: t.Name(), Panic: r}
		}
	}()

	out, err = t.Invoke(ctx, input)
	if err != nil {
		return "", err
	}

	if maxSize := t.Metadata().MaxResultSize; maxSize > 0 && len(out) > maxSize {
		return "", &ToolOutputTooLargeError{Name: t.Name(), Got: len(out), Max: maxSize}
	}
	return out, nil
}
