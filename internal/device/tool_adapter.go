package device

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// CommandSender 是设备命令执行能力，由 Hub 实现。
type CommandSender interface {
	SendCommand(ctx context.Context, deviceID, method string, params json.RawMessage) (*CommandResult, error)
}

// NewCommandTools 将 D-09 的设备能力定义接入统一 Agent 工具注册表。
// 每个调用必须显式提供 device_id；Hub 和 guardrails 仍负责在线状态、频率限制与 HITL。
func NewCommandTools(sender CommandSender) []tools.Tool {
	if sender == nil {
		return nil
	}
	out := make([]tools.Tool, 0, len(DeviceTools))
	for _, def := range DeviceTools {
		out = append(out, &commandTool{sender: sender, def: def})
	}
	return out
}

type commandTool struct {
	sender CommandSender
	def    ToolDef
}

func (t *commandTool) Name() string        { return t.def.Name }
func (t *commandTool) Description() string { return t.def.Description }

func (t *commandTool) JSONSchema() json.RawMessage {
	schema, ok := t.def.Parameters.(map[string]any)
	if !ok {
		return json.RawMessage(`{"type":"object","properties":{"device_id":{"type":"string"}},"required":["device_id"],"additionalProperties":false}`)
	}
	properties := make(map[string]any)
	if original, ok := schema["properties"].(map[string]any); ok {
		for key, value := range original {
			properties[key] = value
		}
	}
	properties["device_id"] = map[string]any{
		"type":        "string",
		"description": "目标已配对设备 ID",
	}
	required := []string{"device_id"}
	if original, ok := schema["required"].([]string); ok {
		required = append(required, original...)
	}
	out := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{"device_id":{"type":"string"}},"required":["device_id"]}`)
	}
	return raw
}

func (t *commandTool) Metadata() tools.Metadata {
	if isReadOnlyDeviceMethod(t.def.Name) {
		return tools.Metadata{
			ReadOnly:        true,
			SearchOrRead:    true,
			ConcurrencySafe: true,
			MaxResultSize:   1 << 20,
			RiskTier:        "low",
		}
	}
	return tools.Metadata{
		Destructive:      true,
		ConcurrencySafe:  false,
		MaxResultSize:    1 << 20,
		RiskTier:         "high",
		RequiresApproval: true,
	}
}

func (t *commandTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	var deviceID string
	if raw, ok := args["device_id"]; ok {
		if err := json.Unmarshal(raw, &deviceID); err != nil {
			return "", fmt.Errorf("device_id 参数无效: %w", err)
		}
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", fmt.Errorf("缺少参数 device_id")
	}
	delete(args, "device_id")
	params, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("序列化设备参数失败: %w", err)
	}
	result, err := t.sender.SendCommand(ctx, deviceID, t.def.Name, params)
	if err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("设备执行失败: %s", result.Err)
	}
	return result.Data, nil
}

func isReadOnlyDeviceMethod(method string) bool {
	switch method {
	case "设备_截屏", "设备_界面层级", "设备_通知列表", "设备_信息", "设备_前台应用":
		return true
	default:
		return false
	}
}

var _ tools.Tool = (*commandTool)(nil)
