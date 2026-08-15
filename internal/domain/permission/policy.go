// Package permission 定义权限策略（B23 三层级中的防火墙层）。
// 设计：三态 Allow/Deny/Ask（tRPC-Agent 2026 实证）。
// 门控顺序（core-agent Gate 实证）：
//
//	可见性过滤 → Plan 门控 → 模式检查 → 禁止表 → 策略 → 人工批准 → 参数校验 → 执行截断。
package permission

import (
	"context"
	"encoding/json"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// Decision 权限决策三态（Allow/Deny/Ask）。
type Decision string

const (
	// DecisionAllow 放行。
	DecisionAllow Decision = "allow"
	// DecisionDeny 拒绝（返回结构化 denied）。
	DecisionDeny Decision = "deny"
	// DecisionAsk 需人工批准。
	DecisionAsk Decision = "ask"
)

// Mode 权限模式（core-agent Gate 实证）。
type Mode string

const (
	// ModeYolo 全放。
	ModeYolo Mode = "yolo"
	// ModeAcceptEdits 写自动放，bash 问。
	ModeAcceptEdits Mode = "accept_edits"
	// ModeAsk 逐个问。
	ModeAsk Mode = "ask"
	// ModePlan 读放写禁。
	ModePlan Mode = "plan"
)

// Request 权限检查请求。
type Request struct {
	ToolName string          `json:"tool_name"`
	Metadata tools.Metadata  `json:"metadata"`
	Args     json.RawMessage `json:"args"`
	Mode     Mode            `json:"mode"`
}

// Policy 权限策略接口。
type Policy interface {
	// Check 检查工具调用是否允许。
	Check(ctx context.Context, req Request) (Decision, string, error)
}

// PolicyChain 门控链（多层护栏，ai-agents skill 实证）。
type PolicyChain struct {
	policies []Policy
}

// NewChain 创建门控链。
func NewChain(policies ...Policy) *PolicyChain {
	return &PolicyChain{policies: policies}
}

// Check 依次执行策略，第一个非 allow 的决策生效。
func (c *PolicyChain) Check(ctx context.Context, req Request) (Decision, string, error) {
	for _, p := range c.policies {
		decision, reason, err := p.Check(ctx, req)
		if err != nil {
			return DecisionDeny, err.Error(), err
		}
		if decision != DecisionAllow {
			return decision, reason, nil
		}
	}
	return DecisionAllow, "", nil
}
