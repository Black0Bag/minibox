// Package app 是组合根（composition root）。
// 这是唯一能看到所有模块的地方：装配依赖、跨模块翻译。
// 模块之间绝不直接 import，全部通过这里调解。
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/permission"
	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// toolkit 把工具注册表适配成 agent.ToolExecutor（阶段 1.1 装配）。
// 权限链在组合根注入：每次执行前过 PolicyChain 门控。
type toolkit struct {
	reg    *tools.Registry
	policy *permission.PolicyChain
}

// Execute 执行工具调用（走权限链门控 + SafeInvoke 防御性包装）。
func (t *toolkit) Execute(ctx context.Context, call llm.ToolCall) (string, error) {
	tool, ok := t.reg.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("工具不存在: %s", call.Name)
	}

	// 权限门控：Allow → 执行；Deny → 拒绝；Ask → 需人工批准
	if t.policy != nil {
		decision, reason, err := t.policy.Check(ctx, permission.Request{
			ToolName: call.Name,
			Metadata: tool.Metadata(),
		})
		if err != nil {
			return "", fmt.Errorf("权限检查失败: %w", err)
		}
		switch decision {
		case permission.DecisionDeny:
			return "", fmt.Errorf("权限拒绝: %s", reason)
		case permission.DecisionAsk:
			return "", fmt.Errorf("需要人工批准: %s", reason)
		}
	}

	// 解析输入 JSON
	var input json.RawMessage
	if call.Arguments != "" {
		input = json.RawMessage(call.Arguments)
	}

	// SafeInvoke：panic 恢复 + 输出上限 + 空输出规范化
	out, err := tools.SafeInvoke(ctx, tool, input)
	if err != nil {
		return "", err
	}
	return out, nil
}

// RequiresApproval 判断工具是否需要人类批准（权限链 DecisionAsk + 元数据标注）。
func (t *toolkit) RequiresApproval(ctx context.Context, call llm.ToolCall) bool {
	tool, ok := t.reg.Get(call.Name)
	if !ok {
		return false
	}
	if tool.Metadata().RequiresApproval {
		return true
	}
	if t.policy != nil {
		decision, _, err := t.policy.Check(ctx, permission.Request{
			ToolName: call.Name,
			Metadata: tool.Metadata(),
		})
		return err == nil && decision == permission.DecisionAsk
	}
	return false
}

var _ agent.ToolExecutor = (*toolkit)(nil)
