package permission

import (
	"context"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// ForbiddenPolicy 禁止表策略（控制面操作拒绝，最优先）。
// 对应门控顺序第 4 步：禁止表。
type ForbiddenPolicy struct{}

// NewForbiddenPolicy 创建禁止表策略。
func NewForbiddenPolicy() *ForbiddenPolicy { return &ForbiddenPolicy{} }

// Check 实现 Policy 接口。
func (p *ForbiddenPolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	if tools.IsForbidden(req.ToolName) {
		return DecisionDeny, "工具被禁止（控制面操作）: " + req.ToolName, nil
	}
	return DecisionAllow, "", nil
}

// ModePolicy 模式策略（core-agent Gate 实证）。
// 对应门控顺序第 3 步：模式检查。
//   - plan 模式：读放写禁
//   - ask 模式：全问（非只读即 ask）
//   - yolo：全放
//   - accept_edits：写自动放，高危问
type ModePolicy struct{}

// NewModePolicy 创建模式策略。
func NewModePolicy() *ModePolicy { return &ModePolicy{} }

// Check 实现 Policy 接口。
func (p *ModePolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	switch req.Mode {
	case ModeYolo:
		return DecisionAllow, "", nil
	case ModePlan:
		if req.Metadata.Destructive || !req.Metadata.ReadOnly {
			return DecisionDeny, "plan 模式禁止写操作: " + req.ToolName, nil
		}
		return DecisionAllow, "", nil
	case ModeAcceptEdits:
		if req.Metadata.RiskTier == "high" {
			return DecisionAsk, "accept_edits 模式高危工具需批准: " + req.ToolName, nil
		}
		return DecisionAllow, "", nil
	case ModeAsk:
		if req.Metadata.ReadOnly {
			return DecisionAllow, "", nil
		}
		return DecisionAsk, "ask 模式非只读工具需批准: " + req.ToolName, nil
	default:
		return DecisionDeny, "未知权限模式: " + string(req.Mode), nil
	}
}

// MetadataPolicy 元数据风险策略（低/中/高风险分级）。
// 高危险工具（破坏性 + 未标只读）一律 ask，除非 yolo。
type MetadataPolicy struct{}

// NewMetadataPolicy 创建元数据风险策略。
func NewMetadataPolicy() *MetadataPolicy { return &MetadataPolicy{} }

// Check 实现 Policy 接口。
func (p *MetadataPolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	// 只读安全工具放行
	if req.Metadata.ReadOnly {
		return DecisionAllow, "", nil
	}
	// 破坏性且高风险 → ask（即使 accept_edits 也拦，双保险）
	if req.Metadata.Destructive && req.Metadata.RiskTier == "high" {
		return DecisionAsk, "破坏性高风险操作需批准: " + req.ToolName, nil
	}
	return DecisionAllow, "", nil
}

// ApprovalRequiredPolicy 元数据 RequiresApproval 策略。
// 工具自身声明需人工批准 → ask。
type ApprovalRequiredPolicy struct{}

// NewApprovalRequiredPolicy 创建批准策略。
func NewApprovalRequiredPolicy() *ApprovalRequiredPolicy { return &ApprovalRequiredPolicy{} }

// Check 实现 Policy 接口。
func (p *ApprovalRequiredPolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	if req.Metadata.RequiresApproval {
		return DecisionAsk, "工具声明需人工批准: " + req.ToolName, nil
	}
	return DecisionAllow, "", nil
}

// ArgsValidationPolicy 参数校验策略（门控顺序第 7 步）。
// 基础校验：参数必须是合法 JSON 对象（非空即验证）。
type ArgsValidationPolicy struct{}

// NewArgsValidationPolicy 创建参数校验策略。
func NewArgsValidationPolicy() *ArgsValidationPolicy { return &ArgsValidationPolicy{} }

// Check 实现 Policy 接口。
func (p *ArgsValidationPolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	// 空参数（工具无参）放行
	if len(req.Args) == 0 || string(req.Args) == "null" || string(req.Args) == "{}" {
		return DecisionAllow, "", nil
	}
	// 必须是一个 JSON 对象
	s := strings.TrimSpace(string(req.Args))
	if !strings.HasPrefix(s, "{") {
		return DecisionDeny, "参数必须为 JSON 对象: " + req.ToolName, nil
	}
	return DecisionAllow, "", nil
}

// StringPolicy 字符串白名单策略（按工具名精确放行/拦截）。
type StringPolicy struct {
	allow []string
}

// NewStringPolicy 创建字符串白名单策略。
func NewStringPolicy(allow ...string) *StringPolicy {
	return &StringPolicy{allow: allow}
}

// Check 实现 Policy 接口。
func (p *StringPolicy) Check(_ context.Context, req Request) (Decision, string, error) {
	if len(p.allow) == 0 {
		return DecisionAllow, "", nil // 空白名单=全放
	}
	for _, name := range p.allow {
		if req.ToolName == name {
			return DecisionAllow, "", nil
		}
	}
	return DecisionDeny, fmt.Sprintf("工具不在白名单: %s", req.ToolName), nil
}